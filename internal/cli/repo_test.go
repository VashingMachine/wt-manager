package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
)

type sandbox struct {
	home string
	svc  *services.Service
}

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	return sandbox{home: t.TempDir(), svc: services.NewService()}
}

// repo creates a git repository under ~/projects with the given remotes
// (name=url pairs).
func (s sandbox) repo(t *testing.T, name string, remotes ...string) string {
	t.Helper()
	path := filepath.Join(s.home, "projects", name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	git(t, path, "init", "-q")
	for _, remote := range remotes {
		name, url, _ := strings.Cut(remote, "=")
		git(t, path, "remote", "add", name, url)
	}
	return path
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func (s sandbox) configPath() string { return services.DefaultConfigPath(s.home) }

func (s sandbox) writeConfig(t *testing.T, app core.AppConfig) {
	t.Helper()
	if err := s.svc.WriteAppConfig(s.configPath(), app); err != nil {
		t.Fatalf("WriteAppConfig() error = %v", err)
	}
}

func (s sandbox) readConfig(t *testing.T) core.AppConfig {
	t.Helper()
	app, err := s.svc.ReadAppConfig(s.configPath())
	if err != nil {
		t.Fatalf("ReadAppConfig() error = %v", err)
	}
	return app
}

// run executes `wt-manager repo <args>` in workingDir, answering each question
// with the next line of answers.
func (s sandbox) run(t *testing.T, workingDir string, answers string, args ...string) (int, string) {
	t.Helper()
	var out strings.Builder
	code := Run(Options{
		Args:       append([]string{"repo"}, args...),
		In:         strings.NewReader(answers),
		Out:        &out,
		HomeDir:    s.home,
		WorkingDir: workingDir,
		Services:   s.svc,
	})
	return code, out.String()
}

func shopConfig(s sandbox) core.AppConfig {
	app := s.svc.DefaultAppConfig()
	app.DefaultProfile = "shop"
	app.Profiles = []core.RepositoryProfile{{Name: "shop", RepositoryPath: "~/projects/api", WorktreesDir: "~/worktrees/api", RemoteName: "origin", GitHubRepo: "acme/api"}}
	return app
}

// Assures that adding the repository the command runs in to the default project works by accepting every default, which follow the project's layout and the repository's remote.
func TestRepoAddJoinsDefaultProjectWithDefaults(t *testing.T) {
	s := newSandbox(t)
	s.repo(t, "api", "origin=git@github.com:acme/api.git")
	web := s.repo(t, "web", "origin=git@github.com:acme/web.git")
	s.writeConfig(t, shopConfig(s))

	code, out := s.run(t, web, "\n\n\n\n\n\n\n", "add")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, question := range []string{"Repository folder [~/projects/web]", "Project [shop]", "Repository name [web]", "Worktrees folder [~/worktrees/web]", "Git remote [origin]", "GitHub repository (owner/name) [acme/web]", "Save to ~/.wt-manager.json? [Y/n]"} {
		if !strings.Contains(out, question) {
			t.Fatalf("output lacks %q:\n%s", question, out)
		}
	}
	profile := s.readConfig(t).Profiles[0]
	want := []core.Repository{
		{Name: "api", RepositoryPath: "~/projects/api", WorktreesDir: "~/worktrees/api", RemoteName: "origin", GitHubRepo: "acme/api"},
		{Name: "web", RepositoryPath: "~/projects/web", WorktreesDir: "~/worktrees/web", RemoteName: "origin", GitHubRepo: "acme/web"},
	}
	if profile.RepositoryPath != "" || len(profile.Repositories) != 2 || profile.Repositories[0] != want[0] || profile.Repositories[1] != want[1] {
		t.Fatalf("profile = %#v, want %#v", profile, want)
	}
	if _, err := os.Stat(filepath.Join(s.home, "worktrees", "web")); err != nil {
		t.Fatalf("worktrees folder was not created: %v", err)
	}
}

// Assures that without any config the command creates the first project from the repository, without asking whether to create it.
func TestRepoAddCreatesFirstProject(t *testing.T) {
	s := newSandbox(t)
	api := s.repo(t, "api", "origin=https://github.com/acme/api.git")

	code, out := s.run(t, api, "\n\n\n\n\n\n", "add")
	if code != 0 || strings.Contains(out, "Create it?") || strings.Contains(out, "Repository name") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "run wt-manager --init") {
		t.Fatalf("output does not point to setup for agents:\n%s", out)
	}
	app := s.readConfig(t)
	profile := app.Profiles[0]
	if app.DefaultProfile != "api" || profile.Name != "api" || profile.RepositoryPath != "~/projects/api" || profile.WorktreesDir != "~/projects" || profile.GitHubRepo != "acme/api" || len(profile.Repositories) != 0 {
		t.Fatalf("config = %#v / %#v", app.DefaultProfile, profile)
	}
}

// Assures that invalid answers are explained and asked again: folders outside git, repositories already in the project, taken names, unknown remotes, and malformed GitHub names.
func TestRepoAddAsksAgainAfterInvalidAnswers(t *testing.T) {
	s := newSandbox(t)
	s.repo(t, "api")
	s.repo(t, "web", "origin=git@github.com:acme/web.git", "upstream=git@github.com:upstream/web.git")
	notGit := filepath.Join(s.home, "notes")
	if err := os.MkdirAll(notGit, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	s.writeConfig(t, shopConfig(s))

	// Folder: not a repository, then one the default project already has.
	code, out := s.run(t, s.home, "~/notes\n~/projects/api\n\n", "add")
	if code == 0 || !strings.Contains(out, "~/notes is not inside a git repository") || !strings.Contains(out, "~/projects/api is already part of shop") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}

	// Name taken, unknown remote, and malformed GitHub name before valid answers.
	code, out = s.run(t, s.home, "~/projects/web\n\napi\nfront\n\nfork\nupstream\nupstream\nacme/ui\ny\n", "add")
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	for _, message := range []string{"shop already has a repository named api", `the repository has no remote "fork"; it has origin, upstream`, "use the owner/name form"} {
		if !strings.Contains(out, message) {
			t.Fatalf("output lacks %q:\n%s", message, out)
		}
	}
	added := s.readConfig(t).Profiles[0].Repository("front")
	if added == nil || added.RemoteName != "upstream" || added.GitHubRepo != "acme/ui" || added.WorktreesDir != "~/worktrees/front" {
		t.Fatalf("added repository = %#v", added)
	}
}

// Assures that a positional folder skips the folder question, and declining the summary leaves the config untouched.
func TestRepoAddFolderArgumentAndDeclinedSave(t *testing.T) {
	s := newSandbox(t)
	s.repo(t, "api")
	s.repo(t, "web")
	s.writeConfig(t, shopConfig(s))
	before, _ := os.ReadFile(s.configPath())

	code, out := s.run(t, s.home, "\n\n\n\n\nn\n", "add", "projects/web")
	if code != 0 || strings.Contains(out, "Repository folder [") || !strings.Contains(out, "Repository folder: ~/projects/web") || !strings.Contains(out, "Nothing saved.") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if after, _ := os.ReadFile(s.configPath()); string(after) != string(before) {
		t.Fatalf("declined save changed the config:\n%s", after)
	}
}

func shopWithWeb(s sandbox) core.AppConfig {
	app := s.svc.DefaultAppConfig()
	app.DefaultProfile = "shop"
	app.Profiles = []core.RepositoryProfile{
		{Name: "shop", Repositories: []core.Repository{
			{Name: "api", RepositoryPath: "~/projects/api", WorktreesDir: "~/worktrees/api"},
			{Name: "web", RepositoryPath: "~/projects/web", WorktreesDir: "~/worktrees/web"},
		}, LastRepository: "web"},
		{Name: "docs", RepositoryPath: "~/projects/docs", WorktreesDir: "~/worktrees/docs", RemoteName: "origin"},
	}
	return app
}

// Assures that removal defaults to the repository the command runs in, only happens after an explicit yes, and keeps the project's other repositories.
func TestRepoRemoveAsksForConfirmation(t *testing.T) {
	s := newSandbox(t)
	s.repo(t, "api")
	web := s.repo(t, "web")
	s.writeConfig(t, shopWithWeb(s))

	code, out := s.run(t, web, "\n\n\n", "remove")
	if code != 0 || !strings.Contains(out, "Project (shop, docs) [shop]") || !strings.Contains(out, "Repository to remove (number or name) [web]") || !strings.Contains(out, "[y/N]") || !strings.Contains(out, "Nothing removed.") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if len(s.readConfig(t).Profiles[0].Repositories) != 2 {
		t.Fatal("declined removal changed the config")
	}

	code, out = s.run(t, s.home, "shop\n2\ny\n", "remove")
	profile := s.readConfig(t).Profiles[0]
	if code != 0 || len(profile.Repositories) != 1 || profile.Repositories[0].Name != "api" || profile.LastRepository != "" {
		t.Fatalf("exit %d, profile = %#v, output:\n%s", code, profile, out)
	}
}

// Assures that removing a project's only repository offers to remove the project, and the next project becomes the default.
func TestRepoRemoveLastRepositoryRemovesProject(t *testing.T) {
	s := newSandbox(t)
	app := shopWithWeb(s)
	app.DefaultProfile = "docs"
	s.writeConfig(t, app)

	code, out := s.run(t, s.home, "docs\n\ny\n", "remove")
	if code != 0 || !strings.Contains(out, "docs is the only repository of docs. Remove the project docs from the config? [y/N]") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	written := s.readConfig(t)
	if len(written.Profiles) != 1 || written.Profiles[0].Name != "shop" || written.DefaultProfile != "shop" {
		t.Fatalf("config = %#v", written)
	}
}

// Assures that list shows every project, the default one, and each repository's folders and GitHub source.
func TestRepoListShowsProjects(t *testing.T) {
	s := newSandbox(t)
	code, out := s.run(t, s.home, "", "list")
	if code != 0 || !strings.Contains(out, "No projects are configured") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	s.writeConfig(t, shopWithWeb(s))
	code, out = s.run(t, s.home, "", "list")
	for _, want := range []string{"shop (default)", "web  ~/projects/web  worktrees ~/worktrees/web  github inferred from origin", "docs\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("exit %d, output lacks %q:\n%s", code, want, out)
		}
	}
}

// Assures that only repo commands bypass the UI and that misuse prints the usage with a usage exit code.
func TestRepoCommandUsage(t *testing.T) {
	if !IsCommand([]string{"repo", "add"}) || IsCommand([]string{"--init"}) || IsCommand(nil) {
		t.Fatal("IsCommand misclassified arguments")
	}
	s := newSandbox(t)
	for _, args := range [][]string{{}, {"rename"}, {"add", "a", "b"}} {
		if code, out := s.run(t, s.home, "", args...); code != 2 || !strings.Contains(out, "wt-manager repo add [folder]") {
			t.Fatalf("args %v: exit %d, output:\n%s", args, code, out)
		}
	}
}

// Assures that a config with errors is reported instead of being overwritten.
func TestRepoCommandsRefuseBrokenConfig(t *testing.T) {
	s := newSandbox(t)
	web := s.repo(t, "web")
	broken := `{"profiles": [`
	if err := os.WriteFile(s.configPath(), []byte(broken), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	code, out := s.run(t, web, "\n\n\n\n\n\n", "add")
	if code != 1 || !strings.Contains(out, "has errors; fix it first") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if data, _ := os.ReadFile(s.configPath()); string(data) != broken {
		t.Fatalf("broken config was overwritten: %s", data)
	}
}
