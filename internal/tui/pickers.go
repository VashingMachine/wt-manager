package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Pickers search folders below a few likely roots as you type. The folder
// picker offers any folder; the repository picker offers only git
// repositories, which is all the repository questions accept.

type pickerMode int

const (
	pickFolders pickerMode = iota
	pickRepositories
)

const (
	folderSearchDepth = 3
	folderSearchLimit = 700
	// Repository search does not descend into repositories, so it can look
	// deeper; it stops after a number of folders or a short time either way.
	repositorySearchDepth  = 4
	repositorySearchLimit  = 6000
	repositorySearchBudget = 300 * time.Millisecond
	pickerRows             = 8
)

type directoryPicker struct {
	services core.Services
	input    textinput.Model
	mode     pickerMode
	dirs     []string
	// planned folders are offered although they do not exist yet, such as a
	// suggested worktrees folder.
	planned  []string
	exclude  map[string]bool
	labels   map[string]string
	filtered []string
	cursor   int
	homeDir  string
	rows     int
}

func newPickerInput(services core.Services, homeDir string, initial string) textinput.Model {
	input := textinput.New()
	input.Prompt = "search/path: "
	input.Placeholder = "~/projects"
	input.CharLimit = 240
	input.Width = 72
	input.SetValue(services.CompactPath(initial, homeDir))
	input.CursorEnd()
	input.Focus()
	return input
}

func newDirectoryPicker(services core.Services, homeDir string, seeds []string, initial string) directoryPicker {
	picker := directoryPicker{
		services: services,
		input:    newPickerInput(services, homeDir, initial),
		dirs:     discoverDirectories(services, seeds, folderSearchDepth, folderSearchLimit),
		homeDir:  homeDir,
		rows:     pickerRows,
	}
	picker.refilter()
	return picker
}

// newWorktreesPicker offers folders for worktrees, leaving out git checkouts
// and everything inside them: worktrees never live in another checkout.
func newWorktreesPicker(services core.Services, homeDir string, seeds []string, initial string) directoryPicker {
	var dirs []string
	for _, dir := range discoverDirectories(services, seeds, folderSearchDepth, folderSearchLimit) {
		if !insideCheckout(dir, seeds) {
			dirs = append(dirs, dir)
		}
	}
	picker := directoryPicker{
		services: services,
		input:    newPickerInput(services, homeDir, initial),
		dirs:     dirs,
		homeDir:  homeDir,
		rows:     pickerRows,
	}
	picker.refilter()
	return picker
}

// insideCheckout reports whether dir is a git checkout or inside one, looking
// no higher than the closest seed (a home folder kept in git still counts as
// a plain folder).
func insideCheckout(dir string, seeds []string) bool {
	stop := map[string]bool{}
	for _, seed := range seeds {
		stop[filepath.Clean(seed)] = true
	}
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		if stop[current] {
			return false
		}
		if _, checkout := gitMarker(current); checkout {
			return true
		}
		if parent := filepath.Dir(current); parent == current {
			return false
		}
	}
}

// newRepositoryPicker offers the git repositories found near paths, leaving
// out the excluded ones (for example those already in the project).
func newRepositoryPicker(services core.Services, homeDir string, paths []string, initial string, exclude []string) directoryPicker {
	excluded := map[string]bool{}
	for _, path := range exclude {
		excluded[services.CanonicalPath(services.ExpandPath(path, homeDir))] = true
	}
	var repositories []string
	for _, repository := range discoverRepositories(services, repositorySeeds(services, homeDir, paths...), repositorySearchDepth, repositorySearchLimit, repositorySearchBudget) {
		if !excluded[repository] {
			repositories = append(repositories, repository)
		}
	}
	picker := directoryPicker{
		services: services,
		input:    newPickerInput(services, homeDir, initial),
		mode:     pickRepositories,
		dirs:     repositories,
		exclude:  excluded,
		homeDir:  homeDir,
		rows:     pickerRows,
	}
	picker.refilter()
	return picker
}

// plan offers path as a folder to create, listed even before it exists.
func (p *directoryPicker) plan(path string) {
	if path == "" {
		return
	}
	path = filepath.Clean(p.services.ExpandPath(path, p.homeDir))
	for _, existing := range p.planned {
		if existing == path {
			return
		}
	}
	p.planned = append(p.planned, path)
	p.refilter()
}

func (p directoryPicker) Update(key tea.KeyMsg) (directoryPicker, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	case tea.KeyDown:
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return p, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(key)
	p.refilter()
	return p, cmd
}

func (p *directoryPicker) label(path string) string {
	if p.labels == nil {
		p.labels = map[string]string{}
	}
	if label, ok := p.labels[path]; ok {
		return label
	}
	label := p.services.CompactPath(path, p.homeDir)
	p.labels[path] = label
	return label
}

func (p *directoryPicker) refilter() {
	query := strings.TrimSpace(p.input.Value())
	candidates := p.dirs
	if len(p.planned) > 0 {
		candidates = append(append([]string(nil), p.dirs...), p.planned...)
	}
	p.filtered = filterDirectoriesBy(candidates, query, p.label)
	// A typed folder the search did not reach is still what was asked for;
	// it goes first, as an exact match would.
	if typed := p.typedCandidate(query); typed != "" {
		kept := p.filtered[:0:0]
		for _, path := range p.filtered {
			if p.services.CanonicalPath(path) != typed {
				kept = append(kept, path)
			}
		}
		p.filtered = append([]string{typed}, kept...)
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

func pathLike(query string) bool {
	return strings.HasPrefix(query, "~") || strings.HasPrefix(query, "/") || strings.HasPrefix(query, ".")
}

// typedCandidate is the typed path when it is an existing folder (an existing
// repository for the repository picker) that is not excluded.
func (p directoryPicker) typedCandidate(query string) string {
	if !pathLike(query) {
		return ""
	}
	typed := p.services.CanonicalPath(p.services.ExpandPath(query, p.homeDir))
	if p.exclude[typed] {
		return ""
	}
	if info, err := os.Stat(typed); err != nil || !info.IsDir() {
		return ""
	}
	if p.mode == pickRepositories {
		if repository, _ := gitMarker(typed); !repository {
			return ""
		}
	}
	return typed
}

// SelectedPath is the highlighted row, or the typed path when nothing matches.
func (p directoryPicker) SelectedPath() string {
	if len(p.filtered) == 0 {
		return p.services.ExpandPath(strings.TrimSpace(p.input.Value()), p.homeDir)
	}
	return p.filtered[min(max(p.cursor, 0), len(p.filtered)-1)]
}

func (p directoryPicker) View(width int) string {
	lines := []string{p.input.View()}
	rows := p.rows
	if rows <= 0 {
		rows = pickerRows
	}
	start := min(max(0, p.cursor-rows/2), max(0, len(p.filtered)-rows))
	end := min(len(p.filtered), start+rows)
	for i := start; i < end; i++ {
		path := p.filtered[i]
		label := p.services.CompactPath(path, p.homeDir)
		if _, err := os.Stat(path); err != nil {
			label += mutedStyle.Render("  new folder")
		}
		if i == p.cursor {
			lines = append(lines, infoStyle.Bold(true).Render(icons.Pointer+" ")+infoStyle.Bold(true).Render(label))
		} else {
			lines = append(lines, "  "+subtleStyle.Render(label))
		}
	}
	switch {
	case len(p.filtered) == 0 && p.mode == pickRepositories:
		lines = append(lines, mutedStyle.Render("  no git repositories match; enter uses the typed path"))
	case len(p.filtered) == 0:
		lines = append(lines, mutedStyle.Render("  no matching folders; enter uses typed path"))
	case len(p.filtered) > rows:
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("  %d–%d of %d", start+1, end, len(p.filtered))))
	}
	return strings.Join(lines, "\n")
}

func setupDirectorySeeds(services core.Services, homeDir string, paths ...string) []string {
	seeds := []string{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		expanded := services.ExpandPath(path, homeDir)
		seeds = append(seeds, expanded, filepath.Dir(expanded))
	}
	seeds = append(seeds,
		filepath.Join(homeDir, "projects"),
		filepath.Join(homeDir, "worktrees"),
		filepath.Join(homeDir, "src"),
		homeDir,
	)
	return uniqueCleanPaths(seeds)
}

// repositorySeeds starts the repository search next to paths (siblings of a
// repository are the likeliest candidates), then in common source folders.
func repositorySeeds(services core.Services, homeDir string, paths ...string) []string {
	seeds := []string{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		expanded := services.ExpandPath(path, homeDir)
		if _, checkout := gitMarker(expanded); !checkout {
			seeds = append(seeds, expanded)
		}
		seeds = append(seeds, filepath.Dir(expanded))
	}
	for _, name := range []string{"projects", "src", "code", "dev", "work", "git", "repos", "workspace", "Developer"} {
		seeds = append(seeds, filepath.Join(homeDir, name))
	}
	return uniqueCleanPaths(append(seeds, homeDir))
}

func discoverDirectories(services core.Services, seeds []string, maxDepth int, maxDirs int) []string {
	seen := map[string]struct{}{}
	var dirs []string
	for _, seed := range uniqueCleanPaths(seeds) {
		walkDirectories(services, seed, 0, maxDepth, maxDirs, seen, &dirs)
		if len(dirs) >= maxDirs {
			break
		}
	}
	sort.Strings(dirs)
	return dirs
}

func walkDirectories(services core.Services, path string, depth int, maxDepth int, maxDirs int, seen map[string]struct{}, dirs *[]string) {
	if len(*dirs) >= maxDirs || depth > maxDepth {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	clean := services.CanonicalPath(path)
	if _, ok := seen[clean]; ok {
		return
	}
	seen[clean] = struct{}{}
	*dirs = append(*dirs, clean)

	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if len(*dirs) >= maxDirs || !entry.IsDir() || skipSetupDir(entry) {
			continue
		}
		walkDirectories(services, filepath.Join(path, entry.Name()), depth+1, maxDepth, maxDirs, seen, dirs)
	}
}

// gitMarker reports whether dir is a repository (it has a .git directory) and
// whether it is any git checkout, linked worktrees and submodules included
// (whose .git is a file).
func gitMarker(dir string) (repository bool, checkout bool) {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return false, false
	}
	return info.IsDir(), true
}

// discoverRepositories finds git repositories below the seeds, shallowest
// first. It never looks inside a checkout below a seed (a seed itself, such
// as a home folder kept in git, is still searched), and stops after maxDirs
// folders or once budget has passed.
func discoverRepositories(services core.Services, seeds []string, maxDepth int, maxDirs int, budget time.Duration) []string {
	type folder struct {
		path  string
		depth int
	}
	deadline := time.Now().Add(budget)
	queue := make([]folder, 0, len(seeds))
	for _, seed := range uniqueCleanPaths(seeds) {
		queue = append(queue, folder{path: seed})
	}
	seen := map[string]struct{}{}
	var repositories []string
	for visited := 0; len(queue) > 0 && visited < maxDirs && time.Now().Before(deadline); {
		current := queue[0]
		queue = queue[1:]
		info, err := os.Stat(current.path)
		if err != nil || !info.IsDir() {
			continue
		}
		clean := services.CanonicalPath(current.path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		visited++
		repository, checkout := gitMarker(clean)
		if repository {
			repositories = append(repositories, clean)
		}
		if (checkout && current.depth > 0) || current.depth >= maxDepth {
			continue
		}
		entries, err := os.ReadDir(clean)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && !skipSetupDir(entry) {
				queue = append(queue, folder{path: filepath.Join(clean, entry.Name()), depth: current.depth + 1})
			}
		}
	}
	sort.Strings(repositories)
	return repositories
}

func skipSetupDir(entry fs.DirEntry) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".") {
		return true
	}
	return name == "node_modules" || name == "vendor" || name == "Library"
}

func uniqueCleanPaths(paths []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func filterDirectories(services core.Services, dirs []string, query string, homeDir string) []string {
	return filterDirectoriesBy(dirs, query, func(path string) string { return services.CompactPath(path, homeDir) })
}

// filterDirectoriesBy ranks folders by how well their label (a ~-relative
// path) matches the query: exact, prefix, folder name, substring, then typos.
func filterDirectoriesBy(dirs []string, query string, label func(string) string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return append([]string(nil), dirs...)
	}
	type scoredDir struct {
		path  string
		label string
		score int
	}
	var scored []scoredDir
	for _, dir := range dirs {
		candidate := strings.ToLower(label(dir))
		base := strings.ToLower(filepath.Base(dir))
		score := -1
		switch {
		case candidate == query:
			score = 0
		case strings.HasPrefix(candidate, query):
			score = 1
		case base == query:
			score = 2
		case strings.HasPrefix(base, query):
			score = 3
		case strings.Contains(candidate, query):
			score = 4
		case fuzzyMatch(candidate, query):
			score = 5
		case fuzzyMatch(base, query):
			score = 6
		}
		if score >= 0 {
			scored = append(scored, scoredDir{path: dir, label: candidate, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		if len(scored[i].label) != len(scored[j].label) {
			return len(scored[i].label) < len(scored[j].label)
		}
		return scored[i].label < scored[j].label
	})
	result := make([]string, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.path)
	}
	return result
}

func fuzzyMatch(candidate string, query string) bool {
	if query == "" {
		return true
	}
	idx := 0
	for _, r := range candidate {
		if idx < len(query) && byte(r) == query[idx] {
			idx++
		}
	}
	return idx == len(query)
}
