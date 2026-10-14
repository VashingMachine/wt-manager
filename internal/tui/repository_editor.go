package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// repositoryEditor asks for one repository of a project: its folder (only git
// repositories are offered), then its name and worktrees folder, with
// defaults following the project's other repositories. Setup and the
// palette's "Add repository" share it.
type repositoryEditor struct {
	services core.Services
	homeDir  string
	// siblings are the project's other repositories, with expanded paths.
	siblings []core.Repository
	// original names the edited repository; it is empty when adding one.
	original        string
	choosingFolder  bool
	folder          directoryPicker
	repoPath        string
	remote          string
	github          string
	nameInput       textinput.Model
	worktrees       directoryPicker
	field           int
	worktreesEdited bool
	err             string
	done            bool
	cancelled       bool
	result          core.Repository
}

const (
	editorFieldName = iota
	editorFieldWorktrees
)

// newRepositoryEditor adds a repository to a project whose repositories are
// siblings, starting the folder search at initial.
func newRepositoryEditor(services core.Services, homeDir string, siblings []core.Repository, initial string) repositoryEditor {
	near := []string{initial}
	exclude := make([]string, 0, len(siblings))
	for _, sibling := range siblings {
		near = append(near, sibling.RepositoryPath)
		exclude = append(exclude, sibling.RepositoryPath)
	}
	return repositoryEditor{
		services:       services,
		homeDir:        homeDir,
		siblings:       siblings,
		choosingFolder: true,
		folder:         newRepositoryPicker(services, homeDir, near, initial, exclude),
	}
}

// editRepositoryEditor changes the name and worktrees folder of repository.
func editRepositoryEditor(services core.Services, homeDir string, siblings []core.Repository, repository core.Repository) repositoryEditor {
	editor := repositoryEditor{services: services, homeDir: homeDir, siblings: siblings, original: repository.Name}
	editor.openDetails(repository.RepositoryPath, repository.Name, repository.WorktreesDir)
	editor.remote = repository.RemoteName
	editor.github = repository.GitHubRepo
	editor.worktreesEdited = true
	return editor
}

func (e *repositoryEditor) openDetails(repoPath string, name string, worktrees string) {
	e.choosingFolder = false
	e.repoPath = repoPath
	e.nameInput = textinput.New()
	e.nameInput.Prompt = ""
	e.nameInput.CharLimit = 60
	e.nameInput.Width = 40
	e.nameInput.SetValue(name)
	e.nameInput.CursorEnd()
	e.worktrees = newWorktreesPicker(e.services, e.homeDir, setupDirectorySeeds(e.services, e.homeDir, worktrees), worktrees)
	e.worktrees.input.Prompt = ""
	e.worktrees.rows = 5
	e.worktrees.plan(worktrees)
	e.field = editorFieldName
	e.focus()
}

func (e *repositoryEditor) focus() {
	e.nameInput.Blur()
	e.worktrees.input.Blur()
	if e.field == editorFieldName {
		e.nameInput.Focus()
	} else {
		e.worktrees.input.Focus()
	}
}

// suggestedWorktrees follows the project's layout for a repository called name.
func (e repositoryEditor) suggestedWorktrees(name string) string {
	return core.SuggestWorktreesDir(e.siblings, name, e.repoPath)
}

func (e repositoryEditor) Update(msg tea.Msg) (repositoryEditor, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey && key.String() == "esc" {
		e.cancelled = true
		return e, nil
	}
	if e.choosingFolder {
		if !isKey {
			return e, nil
		}
		if key.String() == "enter" {
			e.chooseFolder()
			return e, nil
		}
		var cmd tea.Cmd
		e.folder, cmd = e.folder.Update(key)
		return e, cmd
	}
	if isKey {
		switch key.String() {
		case "tab", "shift+tab":
			e.field = 1 - e.field
			e.focus()
			return e, nil
		case "enter":
			if e.field == editorFieldName {
				e.field = editorFieldWorktrees
				e.focus()
				return e, nil
			}
			e.save()
			return e, nil
		case "up", "down":
			if e.field == editorFieldName {
				e.field = editorFieldWorktrees
				e.focus()
				return e, nil
			}
		}
	}
	var cmd tea.Cmd
	if e.field == editorFieldName {
		before := e.nameInput.Value()
		e.nameInput, cmd = e.nameInput.Update(msg)
		// Until it is edited by hand, the worktrees folder follows the name.
		if name := strings.TrimSpace(e.nameInput.Value()); name != before && name != "" && !e.worktreesEdited {
			suggested := e.suggestedWorktrees(name)
			e.worktrees.input.SetValue(e.services.CompactPath(suggested, e.homeDir))
			e.worktrees.plan(suggested)
		}
		return e, cmd
	}
	before := e.worktrees.input.Value()
	if isKey {
		e.worktrees, cmd = e.worktrees.Update(key)
	} else {
		e.worktrees.input, cmd = e.worktrees.input.Update(msg)
	}
	if e.worktrees.input.Value() != before {
		e.worktreesEdited = true
	}
	return e, cmd
}

func (e *repositoryEditor) chooseFolder() {
	selected := e.folder.SelectedPath()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	repoPath, ok := e.services.RepoRootFromDir(ctx, e.services.ExpandPath(selected, e.homeDir))
	if !ok {
		e.err = fmt.Sprintf("%s is not inside a git repository", e.services.CompactPath(selected, e.homeDir))
		return
	}
	for _, sibling := range e.siblings {
		if e.services.CanonicalPath(sibling.RepositoryPath) == e.services.CanonicalPath(repoPath) {
			e.err = fmt.Sprintf("%s is already part of this project", e.services.CompactPath(repoPath, e.homeDir))
			return
		}
	}
	e.err = ""
	remotes := e.services.GitRemotes(ctx, repoPath)
	e.remote = "origin"
	if len(remotes) > 0 && !slices.Contains(remotes, "origin") {
		e.remote = remotes[0]
	}
	e.github = e.services.InferGitHubRepo(ctx, RepositoryProfile{RepositoryPath: repoPath, RemoteName: e.remote})
	name := (RepositoryProfile{Repositories: e.siblings}).UniqueRepositoryName(core.RepositoryNameFromPath(repoPath))
	e.openDetails(repoPath, name, e.suggestedWorktrees(name))
}

func (e *repositoryEditor) save() {
	name := strings.TrimSpace(e.nameInput.Value())
	worktrees := strings.TrimSpace(e.worktrees.SelectedPath())
	e.err = ""
	switch {
	case name == "":
		e.err = "Name the repository"
	case strings.ContainsAny(name, `/\`):
		e.err = "A repository name cannot contain slashes"
	case worktrees == "":
		e.err = "Choose a worktrees folder"
	}
	for _, sibling := range e.siblings {
		if e.err == "" && sibling.Name == name {
			e.err = fmt.Sprintf("Another repository is already called %s", name)
		}
	}
	if e.err != "" {
		return
	}
	e.result = core.Repository{
		Name:           name,
		RepositoryPath: e.repoPath,
		WorktreesDir:   filepath.Clean(e.services.ExpandPath(worktrees, e.homeDir)),
		RemoteName:     e.remote,
		GitHubRepo:     e.github,
	}
	e.done = true
}

// View lists the editor's lines for a modal or the setup card.
func (e repositoryEditor) View(width int) []string {
	var lines []string
	if e.choosingFolder {
		lines = append(lines,
			"Choose the repository. Only git repositories are listed.",
			"",
			e.folder.View(width),
		)
	} else {
		label := func(text string, field int) string {
			if e.field == field {
				return keyHintKeyStyle.Render(fmt.Sprintf("%-10s", text))
			}
			return mutedStyle.Render(fmt.Sprintf("%-10s", text))
		}
		github := "none"
		if e.github != "" {
			github = e.github
		}
		lines = append(lines,
			mutedStyle.Render("Folder    ")+subtleStyle.Render(e.services.CompactPath(e.repoPath, e.homeDir)),
			mutedStyle.Render("Remote    ")+subtleStyle.Render(fmt.Sprintf("%s · GitHub %s", emptyDash(e.remote), github)),
			"",
			label("Name", editorFieldName)+e.nameInput.View(),
			label("Worktrees", editorFieldWorktrees)+e.worktrees.input.View(),
		)
		if e.field == editorFieldWorktrees {
			if rows := strings.Split(e.worktrees.View(width), "\n"); len(rows) > 1 {
				for _, row := range rows[1:] {
					lines = append(lines, "          "+row)
				}
			}
		}
		if note := e.worktreesNote(); note != "" {
			lines = append(lines, "", mutedStyle.Render(note))
		}
	}
	if e.err != "" {
		lines = append(lines, "", errorStyle.Render(icons.Err+" "+e.err))
	}
	return lines
}

// worktreesNote explains what happens with the chosen worktrees folder.
func (e repositoryEditor) worktreesNote() string {
	worktrees := filepath.Clean(e.services.ExpandPath(strings.TrimSpace(e.worktrees.input.Value()), e.homeDir))
	for _, sibling := range e.siblings {
		if filepath.Clean(sibling.WorktreesDir) == worktrees {
			name := strings.TrimSpace(e.nameInput.Value())
			return fmt.Sprintf("Shared with %s: new worktree folders here start with %q.", sibling.Name, name+"-")
		}
	}
	if _, err := os.Stat(worktrees); errors.Is(err, os.ErrNotExist) {
		return "The folder is created when the repository is saved."
	}
	return ""
}

func (e repositoryEditor) hints() []keyHint {
	if e.choosingFolder {
		return []keyHint{{"type", "search"}, {"↑↓", "move"}, {"enter", "choose"}, {"esc", "back"}}
	}
	return []keyHint{{"tab", "next field"}, {"↑↓", "folders"}, {"enter", "next / save"}, {"esc", "back"}}
}
