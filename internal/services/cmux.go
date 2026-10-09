package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	cmuxBundledCLI    = "/Applications/cmux.app/Contents/Resources/bin/cmux"
	cmuxJiraStatusKey = "jira"
	cmuxDecorateLimit = 20 * time.Second
)

var (
	cmuxWorkspaceRefPattern = regexp.MustCompile(`workspace:\d+`)
	cmuxSurfaceRefPattern   = regexp.MustCompile(`surface:\d+`)
)

// inCmux reports whether the process runs inside a cmux terminal. cmux sets
// TERM_PROGRAM=ghostty, so only its own per-terminal variables identify it.
func inCmux(getenv func(string) string) bool {
	return getenv("CMUX_WORKSPACE_ID") != "" || getenv("CMUX_SURFACE_ID") != ""
}

var cmuxBinary = sync.OnceValue(func() string {
	if path, err := exec.LookPath("cmux"); err == nil {
		return path
	}
	if _, err := os.Stat(cmuxBundledCLI); err == nil {
		return cmuxBundledCLI
	}
	return "cmux"
})

// runCmux and cmuxBackground are seams so tests can avoid a real cmux.
var runCmux = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, cmuxBinary(), args...)
	cmd.Dir = dir
	// Legacy verb aliases print a deprecation notice that would corrupt JSON output.
	cmd.Env = append(os.Environ(), "CMUX_QUIET=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
			return "", fmt.Errorf("%w: %s", err, trimmed)
		}
		return "", err
	}
	return string(output), nil
}

var cmuxBackground = func(fn func()) { go fn() }

type cmuxSurface struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Command string `json:"command,omitempty"`
	Focus   bool   `json:"focus,omitempty"`
}

type cmuxLayout struct {
	Pane struct {
		Surfaces []cmuxSurface `json:"surfaces"`
	} `json:"pane"`
}

// cmuxWorkspaceLayout describes one pane: the agent tab (focused) followed by a
// plain shell tab. Without an agent it is just the shell tab.
func cmuxWorkspaceLayout(agent *AgentTool) (string, error) {
	var layout cmuxLayout
	if agent != nil {
		layout.Pane.Surfaces = append(layout.Pane.Surfaces, cmuxSurface{Type: "terminal", Name: agent.Name, Command: agent.Command, Focus: true})
		layout.Pane.Surfaces = append(layout.Pane.Surfaces, cmuxSurface{Type: "terminal", Name: "shell"})
	} else {
		layout.Pane.Surfaces = append(layout.Pane.Surfaces, cmuxSurface{Type: "terminal", Name: "shell", Focus: true})
	}
	encoded, err := json.Marshal(layout)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func openAgentInCmux(ctx context.Context, wt Worktree, agent AgentTool) error {
	return openWorkspaceInCmux(ctx, wt, &agent)
}

func openTerminalInCmux(ctx context.Context, wt Worktree) error {
	return openWorkspaceInCmux(ctx, wt, nil)
}

// openWorkspaceInCmux focuses the worktree's existing cmux workspace, or
// creates one rooted at the worktree.
func openWorkspaceInCmux(ctx context.Context, wt Worktree, agent *AgentTool) error {
	// A workspace created moments ago may be missing from the listing and has not
	// reported its directory yet, so try our own record of it first. Selecting a
	// closed workspace fails, which tells us the record is stale.
	if ref := createdCmuxWorkspace(wt.Path); ref != "" {
		if _, err := runCmux(ctx, "", "select-workspace", "--workspace", ref); err == nil {
			return nil
		}
		forgetCmuxWorkspace(wt.Path)
	}
	if ref := findCmuxWorkspace(ctx, wt); ref != "" {
		if _, err := runCmux(ctx, "", "select-workspace", "--workspace", ref); err == nil {
			scheduleCmuxDecoration(ref, wt)
			return nil
		}
	}
	ref, err := createCmuxWorkspace(ctx, wt, agent)
	if err != nil {
		return fmt.Errorf("open cmux workspace failed: %w", err)
	}
	rememberCmuxWorkspace(wt.Path, ref)
	scheduleCmuxDecoration(ref, wt)
	return nil
}

func createCmuxWorkspace(ctx context.Context, wt Worktree, agent *AgentTool) (string, error) {
	base := []string{"new-workspace", "--name", wt.Name, "--cwd", wt.Path, "--focus", "true"}
	layout, err := cmuxWorkspaceLayout(agent)
	if err != nil {
		return "", err
	}
	output, layoutErr := runCmux(ctx, "", append(base, "--layout", layout)...)
	if layoutErr == nil {
		return parseCmuxCreatedWorkspace(output), nil
	}

	// Fall back to the layout-free form so an unsupported layout never blocks opening.
	fallback := base
	if agent != nil {
		fallback = append(append([]string{}, base...), "--command", agent.Command)
	}
	output, err = runCmux(ctx, "", fallback...)
	if err != nil {
		return "", layoutErr
	}
	ref := parseCmuxCreatedWorkspace(output)
	if agent != nil && ref != "" {
		_, _ = runCmux(ctx, "", "new-surface", "--type", "terminal", "--workspace", ref, "--focus", "false")
	}
	return ref, nil
}

// parseCmuxCreatedWorkspace reads the ref from "OK workspace:<id>".
func parseCmuxCreatedWorkspace(output string) string {
	return cmuxWorkspaceRefPattern.FindString(output)
}

// findCmuxWorkspace returns the ref of a workspace already named after the
// worktree, or "" when none is found or the listing cannot be understood.
func findCmuxWorkspace(ctx context.Context, wt Worktree) string {
	output, err := runCmux(ctx, "", "--json", "--id-format", "refs", "workspace", "list")
	if err != nil {
		return ""
	}
	return matchCmuxWorkspace(output, wt.Name, wt.Path)
}

var createdCmuxWorkspaces = struct {
	sync.Mutex
	refs map[string]string
}{refs: map[string]string{}}

func rememberCmuxWorkspace(path string, ref string) {
	if ref == "" {
		return
	}
	createdCmuxWorkspaces.Lock()
	defer createdCmuxWorkspaces.Unlock()
	createdCmuxWorkspaces.refs[canonicalPath(path)] = ref
}

func createdCmuxWorkspace(path string) string {
	createdCmuxWorkspaces.Lock()
	defer createdCmuxWorkspaces.Unlock()
	return createdCmuxWorkspaces.refs[canonicalPath(path)]
}

func forgetCmuxWorkspace(path string) {
	createdCmuxWorkspaces.Lock()
	defer createdCmuxWorkspaces.Unlock()
	delete(createdCmuxWorkspaces.refs, canonicalPath(path))
}

func matchCmuxWorkspace(output string, name string, path string) string {
	var decoded any
	if err := json.Unmarshal([]byte(output), &decoded); err == nil {
		return findWorkspaceRefInJSON(decoded, name, path)
	}
	return findWorkspaceRefInText(output, name)
}

func findWorkspaceRefInJSON(node any, name string, path string) string {
	switch value := node.(type) {
	case map[string]any:
		ref := firstCmuxString(value, "ref", "workspace_ref")
		title := firstCmuxString(value, "title", "name", "custom_title")
		if cmuxWorkspaceRefPattern.MatchString(ref) && title == name {
			cwd := firstCmuxString(value, "cwd", "current_directory", "working_directory")
			// The shell may have cd'd below the worktree root.
			if cwd == "" || path == "" || pathInsideDir(cwd, path) {
				return ref
			}
		}
		for _, child := range value {
			if found := findWorkspaceRefInJSON(child, name, path); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := findWorkspaceRefInJSON(child, name, path); found != "" {
				return found
			}
		}
	}
	return ""
}

func findWorkspaceRefInText(output string, name string) string {
	titlePattern := regexp.MustCompile(`(^|[\s"'])` + regexp.QuoteMeta(name) + `($|[\s"'])`)
	for _, line := range strings.Split(output, "\n") {
		if ref := cmuxWorkspaceRefPattern.FindString(line); ref != "" && titlePattern.MatchString(line) {
			return ref
		}
	}
	return ""
}

func firstCmuxString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := values[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func scheduleCmuxDecoration(ref string, wt Worktree) {
	if ref == "" {
		return
	}
	cmuxBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cmuxDecorateLimit)
		defer cancel()
		decorateCmuxWorkspace(ctx, ref, wt)
	})
}

// decorateCmuxWorkspace is best effort: the workspace already opened, so sidebar
// extras (linked PR, JIRA pill) must never turn into a user-visible failure.
func decorateCmuxWorkspace(ctx context.Context, ref string, wt Worktree) {
	if key := ticketKeyFromBranch(wt.Branch); key != "" {
		_, _ = runCmux(ctx, "", "set-status", cmuxJiraStatusKey, key, "--workspace", ref, "--icon", "ticket", "--color", "#4C8DFF")
	}
	if wt.PR != nil && wt.PR.Number > 0 {
		// cmux resolves the repository from the working directory.
		_, _ = runCmux(ctx, wt.Path, "pr", fmt.Sprint(wt.PR.Number), "--workspace", ref)
	}
}

var cmuxBrowserSurface struct {
	sync.Mutex
	ref string
}

// openURLInCmux shows a page in a cmux browser split beside the caller. Later
// calls navigate that same split instead of piling up new ones.
func openURLInCmux(ctx context.Context, url string) error {
	cmuxBrowserSurface.Lock()
	defer cmuxBrowserSurface.Unlock()

	if cmuxBrowserSurface.ref != "" {
		if _, err := runCmux(ctx, "", "browser", "--surface", cmuxBrowserSurface.ref, "navigate", url); err == nil {
			return nil
		}
		cmuxBrowserSurface.ref = ""
	}
	output, err := runCmux(ctx, "", "browser", "open", url)
	if err != nil {
		return err
	}
	cmuxBrowserSurface.ref = cmuxSurfaceRefPattern.FindString(output)
	return nil
}

func notifyCmux(ctx context.Context, title string, body string) error {
	if !inCmux(os.Getenv) {
		return nil
	}
	_, err := runCmux(ctx, "", "notify", "--title", title, "--body", body)
	return err
}
