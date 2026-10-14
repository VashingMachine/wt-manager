// Package cli holds the commands wt-manager runs without its terminal UI.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
)

// Usage documents the repo commands.
const Usage = `usage: wt-manager repo add [folder]       add a repository to a project
       wt-manager repo remove [repository]  remove a repository from a project
       wt-manager repo list                 list projects and their repositories`

// Options connects a command to the user's terminal and environment.
type Options struct {
	Args       []string
	In         io.Reader
	Out        io.Writer
	HomeDir    string
	WorkingDir string
	Services   core.Services
}

// IsCommand reports whether args start a command handled here.
func IsCommand(args []string) bool {
	return len(args) > 0 && args[0] == "repo"
}

// Main runs a command against the real terminal and environment.
func Main(args []string) int {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt-manager: %v\n", err)
		return 1
	}
	workingDir, _ := os.Getwd()
	return Run(Options{Args: args, In: os.Stdin, Out: os.Stdout, HomeDir: homeDir, WorkingDir: workingDir, Services: services.NewService()})
}

// Run executes the command in opts.Args and returns the process exit code.
func Run(opts Options) int {
	args := opts.Args
	if len(args) > 0 && args[0] == "repo" {
		args = args[1:]
	}
	c := &command{opts: opts, prompt: &prompter{in: bufio.NewReader(opts.In), out: opts.Out}}
	var err error
	switch {
	case len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help":
		fmt.Fprintln(opts.Out, Usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	case args[0] == "add" && len(args) <= 2:
		err = c.add(args[1:])
	case (args[0] == "remove" || args[0] == "rm") && len(args) <= 2:
		err = c.remove(args[1:])
	case args[0] == "list" || args[0] == "ls":
		err = c.list()
	default:
		fmt.Fprintf(opts.Out, "unknown repo command: %s\n%s\n", strings.Join(args, " "), Usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(opts.Out, "wt-manager: %v\n", err)
		return 1
	}
	return 0
}

type command struct {
	opts   Options
	prompt *prompter
}

func (c *command) services() core.Services { return c.opts.Services }

func (c *command) compact(path string) string {
	return c.services().CompactPath(path, c.opts.HomeDir)
}

func (c *command) expand(path string) string {
	return c.services().ExpandPath(path, c.opts.HomeDir)
}

// absolute expands ~ and resolves a relative answer against the folder the
// command runs in.
func (c *command) absolute(path string) string {
	expanded := c.expand(path)
	if !filepath.IsAbs(expanded) && c.opts.WorkingDir != "" {
		expanded = filepath.Join(c.opts.WorkingDir, expanded)
	}
	return filepath.Clean(expanded)
}

func (c *command) samePath(left string, right string) bool {
	return c.services().CanonicalPath(c.expand(left)) == c.services().CanonicalPath(c.expand(right))
}

// loadConfig reads the config without rewriting it; a missing file starts
// from the defaults the app uses.
func (c *command) loadConfig() (core.AppConfig, string, bool, error) {
	path := services.DefaultConfigPath(c.opts.HomeDir)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return c.services().DefaultAppConfig(), path, false, nil
	}
	app, err := c.services().ReadAppConfig(path)
	if err != nil {
		return core.AppConfig{}, path, true, fmt.Errorf("%s has errors; fix it first: %w", c.compact(path), err)
	}
	return app, path, true, nil
}

func (c *command) save(path string, app core.AppConfig) error {
	normalized, _, err := c.services().NormalizeAppConfig(app)
	if err != nil {
		return err
	}
	return c.services().WriteAppConfig(path, normalized)
}

// gitRoot resolves a folder to the root of the git repository containing it.
func (c *command) gitRoot(folder string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	expanded := c.absolute(folder)
	root, ok := c.services().RepoRootFromDir(ctx, expanded)
	if !ok {
		return "", fmt.Errorf("%s is not inside a git repository", c.compact(expanded))
	}
	return root, nil
}

// workingRepository is the repository the command runs in, if any.
func (c *command) workingRepository() string {
	if c.opts.WorkingDir == "" {
		return ""
	}
	root, err := c.gitRoot(c.opts.WorkingDir)
	if err != nil {
		return ""
	}
	return root
}

func profileIndex(app core.AppConfig, name string) int {
	for idx, profile := range app.Profiles {
		if profile.Name == name {
			return idx
		}
	}
	return -1
}

// repositoryOwner finds the project and repository a folder belongs to.
func (c *command) repositoryOwner(app core.AppConfig, repoPath string) (int, core.Repository) {
	for idx, profile := range app.Profiles {
		for _, repository := range profile.RepositoryList() {
			if c.samePath(repository.RepositoryPath, repoPath) {
				return idx, repository
			}
		}
	}
	return -1, core.Repository{}
}

func projectSummary(profile core.RepositoryProfile) string {
	names := make([]string, 0, len(profile.RepositoryList()))
	for _, repository := range profile.RepositoryList() {
		names = append(names, repository.Name)
	}
	return fmt.Sprintf("%s (%s)", profile.Name, strings.Join(names, ", "))
}

func projectNames(app core.AppConfig) string {
	names := make([]string, 0, len(app.Profiles))
	for _, profile := range app.Profiles {
		names = append(names, profile.Name)
	}
	return strings.Join(names, ", ")
}

// defaultRemote prefers origin, then the repository's only remote.
func defaultRemote(remotes []string) string {
	if len(remotes) == 0 || slices.Contains(remotes, "origin") {
		return "origin"
	}
	return remotes[0]
}

func (c *command) add(args []string) error {
	app, path, existed, err := c.loadConfig()
	if err != nil {
		return err
	}
	fmt.Fprintln(c.opts.Out, "Add a repository to a wt-manager project. Press enter to accept the [default].")
	fmt.Fprintln(c.opts.Out)

	var repoPath string
	if len(args) == 1 {
		if repoPath, err = c.gitRoot(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(c.opts.Out, "Repository folder: %s\n", c.compact(repoPath))
	} else {
		suggested := ""
		if working := c.workingRepository(); working != "" {
			suggested = c.compact(working)
		}
		if repoPath, err = c.prompt.ask("Repository folder", suggested, c.gitRoot); err != nil {
			return err
		}
	}
	if owner, _ := c.repositoryOwner(app, repoPath); owner >= 0 {
		fmt.Fprintf(c.opts.Out, "  note: %s is already part of %s\n", c.compact(repoPath), app.Profiles[owner].Name)
	}

	// Projects: join the default project, or name a new one.
	defaultProject := app.DefaultProfile
	if defaultProject == "" {
		defaultProject = core.RepositoryNameFromPath(repoPath)
	}
	if len(app.Profiles) > 0 {
		summaries := make([]string, 0, len(app.Profiles))
		for _, profile := range app.Profiles {
			summaries = append(summaries, projectSummary(profile))
		}
		fmt.Fprintf(c.opts.Out, "Projects: %s\n", strings.Join(summaries, ", "))
	}
	projectName, err := c.prompt.ask("Project", defaultProject, func(name string) (string, error) {
		if idx := profileIndex(app, name); idx >= 0 {
			for _, repository := range app.Profiles[idx].RepositoryList() {
				if c.samePath(repository.RepositoryPath, repoPath) {
					return "", fmt.Errorf("%s is already part of %s; choose another project", c.compact(repoPath), name)
				}
			}
			return name, nil
		}
		if len(app.Profiles) == 0 {
			return name, nil
		}
		// With projects to choose from, an unknown name may be a typo.
		create, err := c.prompt.confirm(fmt.Sprintf("Project %q does not exist. Create it?", name), true)
		if err != nil {
			return "", err
		}
		if !create {
			return "", fmt.Errorf("choose one of: %s", projectNames(app))
		}
		return name, nil
	})
	if err != nil {
		return err
	}
	idx := profileIndex(app, projectName)

	// A new project has a single repository, named after its folder.
	name := core.RepositoryNameFromPath(repoPath)
	worktreesDefault := c.compact(filepath.Dir(repoPath))
	if idx >= 0 {
		profile := app.Profiles[idx]
		name, err = c.prompt.ask("Repository name", profile.UniqueRepositoryName(name), func(value string) (string, error) {
			if strings.ContainsAny(value, `/\`) {
				return "", errors.New("a repository name cannot contain slashes")
			}
			if profile.Repository(value) != nil {
				return "", fmt.Errorf("%s already has a repository named %s", projectName, value)
			}
			return value, nil
		})
		if err != nil {
			return err
		}
		worktreesDefault = core.SuggestWorktreesDir(profile.RepositoryList(), name, c.compact(repoPath))
	}
	worktrees, err := c.prompt.ask("Worktrees folder", worktreesDefault, func(value string) (string, error) {
		return c.compact(c.absolute(value)), nil
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	remotes := c.services().GitRemotes(ctx, repoPath)
	if len(remotes) > 1 {
		fmt.Fprintf(c.opts.Out, "Remotes: %s\n", strings.Join(remotes, ", "))
	}
	remote, err := c.prompt.ask("Git remote", defaultRemote(remotes), func(value string) (string, error) {
		if len(remotes) > 0 && !slices.Contains(remotes, value) {
			return "", fmt.Errorf("the repository has no remote %q; it has %s", value, strings.Join(remotes, ", "))
		}
		return value, nil
	})
	if err != nil {
		return err
	}
	// An empty githubRepo is inferred from the remote whenever it points to
	// GitHub, so "none" only means nothing is configured explicitly.
	slug := c.services().InferGitHubRepo(ctx, core.RepositoryProfile{RepositoryPath: repoPath, RemoteName: remote})
	githubDefault := slug
	if githubDefault == "" {
		githubDefault = "none"
	}
	github, err := c.prompt.ask("GitHub repository (owner/name)", githubDefault, func(value string) (string, error) {
		if strings.EqualFold(value, "none") || value == "-" {
			return "", nil
		}
		if parts := strings.Split(value, "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", errors.New("use the owner/name form, for example acme/shop")
		}
		return value, nil
	})
	if err != nil {
		return err
	}

	repository := core.Repository{Name: name, RepositoryPath: c.compact(repoPath), WorktreesDir: worktrees, RemoteName: remote, GitHubRepo: github}
	fmt.Fprintln(c.opts.Out)
	project := projectName + " (new project)"
	if idx >= 0 {
		project = projectSummary(app.Profiles[idx].WithRepositories(append(slices.Clone(app.Profiles[idx].RepositoryList()), repository)))
	}
	summary := tabwriter.NewWriter(c.opts.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(summary, "  Project:\t%s\n", project)
	fmt.Fprintf(summary, "  Repository:\t%s\t%s\n", name, repository.RepositoryPath)
	fmt.Fprintf(summary, "  Worktrees:\t%s\n", worktrees)
	fmt.Fprintf(summary, "  Remote:\t%s\n", remote)
	switch {
	case github != "":
		fmt.Fprintf(summary, "  GitHub:\t%s\n", github)
	case slug != "":
		fmt.Fprintf(summary, "  GitHub:\t%s (inferred from %s)\n", slug, remote)
	default:
		fmt.Fprintf(summary, "  GitHub:\tnone (%s is not a GitHub remote; PR features stay off)\n", remote)
	}
	summary.Flush()
	if idx >= 0 {
		for _, other := range app.Profiles[idx].RepositoryList() {
			if c.samePath(other.WorktreesDir, worktrees) {
				fmt.Fprintf(c.opts.Out, "  note: shares its worktrees folder with %s, so new worktree folders start with %q\n", other.Name, name+"-")
				break
			}
		}
	}
	save, err := c.prompt.confirm("Save to "+c.compact(path)+"?", true)
	if err != nil {
		return err
	}
	if !save {
		fmt.Fprintln(c.opts.Out, "Nothing saved.")
		return nil
	}

	if idx >= 0 {
		profile := app.Profiles[idx]
		app.Profiles = slices.Clone(app.Profiles)
		app.Profiles[idx] = profile.WithRepositories(append(slices.Clone(profile.RepositoryList()), repository))
	} else {
		app.Profiles = append(slices.Clone(app.Profiles), core.RepositoryProfile{
			Name:           projectName,
			RepositoryPath: repository.RepositoryPath,
			WorktreesDir:   repository.WorktreesDir,
			RemoteName:     repository.RemoteName,
			GitHubRepo:     repository.GitHubRepo,
		})
		if app.DefaultProfile == "" {
			app.DefaultProfile = projectName
		}
	}
	if err := os.MkdirAll(c.expand(worktrees), 0o755); err != nil {
		return fmt.Errorf("create worktrees folder: %w", err)
	}
	if err := c.save(path, app); err != nil {
		return err
	}
	if idx >= 0 {
		fmt.Fprintf(c.opts.Out, "Added %s to %s. A running wt-manager picks the change up within a few seconds.\n", name, projectName)
	} else {
		fmt.Fprintf(c.opts.Out, "Created project %s with %s.\n", projectName, name)
	}
	if !existed {
		fmt.Fprintf(c.opts.Out, "Created %s; run wt-manager --init to choose agent tools and JIRA boards.\n", c.compact(path))
	}
	return nil
}

func (c *command) remove(args []string) error {
	app, path, _, err := c.loadConfig()
	if err != nil {
		return err
	}
	if len(app.Profiles) == 0 {
		return errors.New("no projects are configured; run wt-manager repo add or wt-manager --init")
	}
	fmt.Fprintln(c.opts.Out, "Remove a repository from a wt-manager project. Its folder and worktrees stay on disk.")
	fmt.Fprintln(c.opts.Out)

	// The repository the command runs in is the likeliest one to remove.
	workingOwner, workingRepository := -1, core.Repository{}
	if working := c.workingRepository(); working != "" {
		workingOwner, workingRepository = c.repositoryOwner(app, working)
	}
	var idx int
	if len(app.Profiles) == 1 {
		idx = 0
		fmt.Fprintf(c.opts.Out, "Project: %s\n", app.Profiles[0].Name)
	} else {
		defaultProject := app.DefaultProfile
		if workingOwner >= 0 {
			defaultProject = app.Profiles[workingOwner].Name
		}
		projectName, err := c.prompt.ask(fmt.Sprintf("Project (%s)", projectNames(app)), defaultProject, func(name string) (string, error) {
			if profileIndex(app, name) < 0 {
				return "", fmt.Errorf("choose one of: %s", projectNames(app))
			}
			return name, nil
		})
		if err != nil {
			return err
		}
		idx = profileIndex(app, projectName)
	}
	profile := app.Profiles[idx]
	repositories := profile.RepositoryList()

	fmt.Fprintf(c.opts.Out, "Repositories of %s:\n", profile.Name)
	listing := tabwriter.NewWriter(c.opts.Out, 0, 0, 2, ' ', 0)
	for number, repository := range repositories {
		fmt.Fprintf(listing, "  %d\t%s\t%s\n", number+1, repository.Name, repository.RepositoryPath)
	}
	listing.Flush()
	resolve := func(value string) (string, error) {
		if number, err := strconv.Atoi(value); err == nil && number >= 1 && number <= len(repositories) {
			return repositories[number-1].Name, nil
		}
		if profile.Repository(value) != nil {
			return value, nil
		}
		return "", fmt.Errorf("%s has no repository %q; answer with its number or name", profile.Name, value)
	}
	var name string
	if len(args) == 1 {
		if name, err = resolve(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(c.opts.Out, "Repository: %s\n", name)
	} else {
		defaultRepository := ""
		switch {
		case workingOwner == idx:
			defaultRepository = workingRepository.Name
		case len(repositories) == 1:
			defaultRepository = repositories[0].Name
		}
		if name, err = c.prompt.ask("Repository to remove (number or name)", defaultRepository, resolve); err != nil {
			return err
		}
	}

	if len(repositories) == 1 {
		// A project cannot be empty; offer to drop the project itself.
		confirmed, err := c.prompt.confirm(fmt.Sprintf("%s is the only repository of %s. Remove the project %s from the config?", name, profile.Name, profile.Name), false)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(c.opts.Out, "Nothing removed.")
			return nil
		}
		app.Profiles = slices.Delete(slices.Clone(app.Profiles), idx, idx+1)
		if app.DefaultProfile == profile.Name {
			app.DefaultProfile = ""
		}
		if err := c.save(path, app); err != nil {
			return err
		}
		fmt.Fprintf(c.opts.Out, "Removed project %s. Its folders and worktrees stay on disk.\n", profile.Name)
		return nil
	}

	confirmed, err := c.prompt.confirm(fmt.Sprintf("Remove %s from %s?", name, profile.Name), false)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(c.opts.Out, "Nothing removed.")
		return nil
	}
	remaining := slices.DeleteFunc(slices.Clone(repositories), func(repository core.Repository) bool { return repository.Name == name })
	app.Profiles = slices.Clone(app.Profiles)
	app.Profiles[idx] = profile.WithRepositories(remaining)
	if err := c.save(path, app); err != nil {
		return err
	}
	fmt.Fprintf(c.opts.Out, "Removed %s from %s. Its folder and worktrees stay on disk.\n", name, profile.Name)
	return nil
}

func (c *command) list() error {
	app, _, _, err := c.loadConfig()
	if err != nil {
		return err
	}
	if len(app.Profiles) == 0 {
		fmt.Fprintln(c.opts.Out, "No projects are configured. Run wt-manager repo add or wt-manager --init.")
		return nil
	}
	out := tabwriter.NewWriter(c.opts.Out, 0, 0, 2, ' ', 0)
	for _, profile := range app.Profiles {
		header := profile.Name
		if profile.Name == app.DefaultProfile {
			header += " (default)"
		}
		fmt.Fprintf(out, "%s\n", header)
		for _, repository := range profile.RepositoryList() {
			fmt.Fprintf(out, "  %s\t%s\tworktrees %s\tgithub %s\n", repository.Name, repository.RepositoryPath, repository.WorktreesDir, emptyAs(repository.GitHubRepo, "inferred from "+emptyAs(repository.RemoteName, "origin")))
		}
	}
	return out.Flush()
}

func emptyAs(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// prompter asks questions on the terminal. Each shows its default in
// brackets; an empty answer accepts it.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
	eof bool
}

func (p *prompter) readLine() (string, bool) {
	if p.eof {
		return "", false
	}
	line, err := p.in.ReadString('\n')
	if err != nil {
		p.eof = true
		if line == "" {
			return "", false
		}
	}
	return strings.TrimSpace(line), true
}

// ask returns the answer, or the default for an empty one, after check
// accepts it; rejected answers are explained and asked again.
func (p *prompter) ask(question string, def string, check func(string) (string, error)) (string, error) {
	for {
		if def != "" {
			fmt.Fprintf(p.out, "%s [%s]: ", question, def)
		} else {
			fmt.Fprintf(p.out, "%s: ", question)
		}
		answer, ok := p.readLine()
		if !ok {
			fmt.Fprintln(p.out)
		}
		if answer == "" {
			answer = def
		}
		if answer == "" {
			if !ok {
				return "", fmt.Errorf("no answer for %q", question)
			}
			fmt.Fprintln(p.out, "  an answer is required")
			continue
		}
		if check == nil {
			return answer, nil
		}
		value, err := check(answer)
		if err == nil {
			return value, nil
		}
		fmt.Fprintf(p.out, "  %v\n", err)
		if !ok {
			return "", err
		}
	}
}

// confirm asks a yes/no question; an empty answer takes def.
func (p *prompter) confirm(question string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		fmt.Fprintf(p.out, "%s %s: ", question, hint)
		answer, ok := p.readLine()
		if !ok {
			fmt.Fprintln(p.out)
		}
		switch strings.ToLower(answer) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(p.out, "  answer y or n")
		if !ok {
			return false, fmt.Errorf("no answer for %q", question)
		}
	}
}
