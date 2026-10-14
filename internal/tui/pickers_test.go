package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}
}

func gitInit(t *testing.T, path string) string {
	t.Helper()
	mkdirs(t, path)
	runGit(t, path, "init", "-q")
	return path
}

func labels(svc *services.Service, home string, paths []string) string {
	var out []string
	for _, path := range paths {
		out = append(out, svc.CompactPath(path, home))
	}
	return strings.Join(out, ",")
}

// Assures that the repository picker lists only git repositories: no plain folders, nothing inside a repository, no linked worktrees, and no excluded repositories, even when the home folder itself is kept in git.
func TestRepositoryPickerListsOnlyRepositories(t *testing.T) {
	svc := services.NewService()
	home := t.TempDir()
	gitInit(t, home)
	api := gitInit(t, filepath.Join(home, "projects", "api"))
	gitInit(t, filepath.Join(home, "projects", "api", "nested"))
	gitInit(t, filepath.Join(home, "projects", "web"))
	mkdirs(t, filepath.Join(home, "projects", "notes"), filepath.Join(home, "projects", "api-feature"))
	if err := os.WriteFile(filepath.Join(home, "projects", "api-feature", ".git"), []byte("gitdir: "+api+"/.git/worktrees/api-feature\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	picker := newRepositoryPicker(svc, home, []string{api}, "~/projects", []string{api})
	if got := labels(svc, home, picker.filtered); got != "~/projects/web" {
		t.Fatalf("repositories = %q, want only ~/projects/web", got)
	}
	picker.input.SetValue("")
	picker.refilter()
	if got := labels(svc, home, picker.filtered); !strings.Contains(got, "~/projects/web") || strings.Contains(got, "notes") || strings.Contains(got, "nested") || strings.Contains(got, "api") {
		t.Fatalf("all repositories = %q", got)
	}
	if view := ansi.Strip(picker.View(80)); strings.Contains(view, "notes") {
		t.Fatalf("view lists a plain folder: %q", view)
	}
}

// Assures that a typed repository the search did not reach is offered first, while typed plain folders and excluded repositories are not.
func TestRepositoryPickerOffersTypedRepository(t *testing.T) {
	svc := services.NewService()
	home := t.TempDir()
	deep := gitInit(t, filepath.Join(home, "a", "b", "c", "d", "e", "deep"))
	excluded := gitInit(t, filepath.Join(home, "projects", "api"))
	mkdirs(t, filepath.Join(home, "projects", "notes"))
	picker := newRepositoryPicker(svc, home, nil, "", []string{excluded})

	for _, tc := range []struct {
		typed string
		want  string
	}{
		{"~/a/b/c/d/e/deep", svc.CanonicalPath(deep)},
		{"~/projects/notes", ""},
		{"~/projects/api", ""},
	} {
		picker.input.SetValue(tc.typed)
		picker.refilter()
		got := ""
		if len(picker.filtered) > 0 && picker.filtered[0] == tc.want {
			got = tc.want
		}
		if tc.want != "" && got == "" {
			t.Fatalf("typed %s: filtered = %v, want it first", tc.typed, picker.filtered)
		}
		if tc.want == "" {
			for _, path := range picker.filtered {
				if strings.HasSuffix(path, filepath.Base(tc.typed)) {
					t.Fatalf("typed %s was offered: %v", tc.typed, picker.filtered)
				}
			}
		}
	}
}

// Assures that enter takes the highlighted row even when the typed text names an existing folder.
func TestDirectoryPickerEnterUsesHighlightedRow(t *testing.T) {
	svc := services.NewService()
	home := t.TempDir()
	mkdirs(t, filepath.Join(home, "projects", "api"), filepath.Join(home, "projects", "web"))
	picker := newDirectoryPicker(svc, home, setupDirectorySeeds(svc, home), filepath.Join(home, "projects"))
	if svc.CanonicalPath(picker.SelectedPath()) != svc.CanonicalPath(filepath.Join(home, "projects")) {
		t.Fatalf("initial selection = %q, want the typed folder", picker.SelectedPath())
	}
	picker, _ = picker.Update(teaKeyType("down"))
	if selected := picker.SelectedPath(); selected != picker.filtered[1] || svc.CanonicalPath(selected) == svc.CanonicalPath(filepath.Join(home, "projects")) {
		t.Fatalf("after moving down, selection = %q, want the highlighted %q", selected, picker.filtered[1])
	}
}

// Assures that long lists scroll with the highlight and say how many rows there are.
func TestPickerListScrollsWithCursor(t *testing.T) {
	svc := services.NewService()
	home := t.TempDir()
	for i := 1; i <= 12; i++ {
		gitInit(t, filepath.Join(home, "projects", fmt.Sprintf("repo-%02d", i)))
	}
	picker := newRepositoryPicker(svc, home, nil, "~/projects", nil)
	for range 11 {
		picker, _ = picker.Update(teaKeyType("down"))
	}
	view := ansi.Strip(picker.View(80))
	if !strings.Contains(view, "repo-12") || strings.Contains(view, "repo-01") || !strings.Contains(view, "of 12") {
		t.Fatalf("scrolled view = %q", view)
	}
}

// Assures that worktrees folder suggestions leave out checkouts and their contents, and offer a planned folder before it exists.
func TestWorktreesPickerSkipsCheckoutsAndOffersPlannedFolder(t *testing.T) {
	svc := services.NewService()
	home := t.TempDir()
	gitInit(t, filepath.Join(home, "projects", "api"))
	mkdirs(t, filepath.Join(home, "projects", "api", "src"), filepath.Join(home, "projects", "shared"))
	planned := filepath.Join(home, "worktrees", "api")
	picker := newWorktreesPicker(svc, home, setupDirectorySeeds(svc, home, planned), planned)
	picker.plan(planned)
	if got := labels(svc, home, picker.filtered); got != "~/worktrees/api" {
		t.Fatalf("filtered = %q, want the planned folder first", got)
	}
	if view := ansi.Strip(picker.View(80)); !strings.Contains(view, "new folder") {
		t.Fatalf("planned folder is not marked new: %q", view)
	}
	picker.input.SetValue("~/projects")
	picker.refilter()
	if got := labels(svc, home, picker.filtered); strings.Contains(got, "api") || !strings.Contains(got, "~/projects/shared") {
		t.Fatalf("folders = %q, want checkouts left out", got)
	}
}

func teaKeyType(name string) tea.KeyMsg {
	switch name {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return teaKey(name)
}
