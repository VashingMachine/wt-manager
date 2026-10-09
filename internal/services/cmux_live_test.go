package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in live check against a running cmux: set WT_CMUX_LIVE=1 and run from a
// cmux terminal (cmux only accepts callers started inside it). It opens a real
// workspace and browser split, verifies reuse, then closes what it created.
func TestCmuxLiveIntegration(t *testing.T) {
	if os.Getenv("WT_CMUX_LIVE") != "1" || !inCmux(os.Getenv) {
		t.Skip("set WT_CMUX_LIVE=1 and run inside a cmux terminal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	name := fmt.Sprintf("wt-live-%d", time.Now().UnixNano())
	wt := Worktree{Name: name, Path: t.TempDir(), Branch: "LIVE-1-check"}
	agent := AgentTool{Name: "agent", Command: "echo agent-started"}

	// Make decoration synchronous so its output is visible before assertions.
	originalBackground := cmuxBackground
	cmuxBackground = func(fn func()) { fn() }
	t.Cleanup(func() { cmuxBackground = originalBackground })

	// Close by recorded ref (the listing can lag) and by name, so a failed
	// assertion below still cleans up after itself.
	t.Cleanup(func() {
		if ref := createdCmuxWorkspace(wt.Path); ref != "" {
			_, _ = runCmux(context.Background(), "", "close-workspace", "--workspace", ref)
		}
		closeCmuxWorkspacesNamed(name)
	})
	if err := openAgentInCmux(ctx, wt, agent); err != nil {
		t.Fatalf("openAgentInCmux: %v", err)
	}
	listing, err := runCmux(ctx, "", "--json", "--id-format", "refs", "workspace", "list")
	if err != nil {
		t.Fatalf("list-workspaces: %v", err)
	}
	var parsed struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	parseErr := json.Unmarshal([]byte(listing), &parsed)
	t.Logf("listing parse error: %v", parseErr)
	for _, w := range parsed.Workspaces {
		t.Logf("workspace %v title=%q cwd=%v", w["ref"], w["title"], w["current_directory"])
	}
	// The listing can lag a moment behind creation, so poll before asserting.
	ref := matchCmuxWorkspace(listing, wt.Name, "")
	for i := 0; ref == "" && i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		listing, _ = runCmux(ctx, "", "--json", "--id-format", "refs", "workspace", "list")
		ref = matchCmuxWorkspace(listing, wt.Name, "")
	}
	if ref == "" {
		t.Fatalf("created workspace %q never appeared in listing", wt.Name)
	}

	if err := openTerminalInCmux(ctx, wt); err != nil {
		t.Fatalf("second open: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	again, err := runCmux(ctx, "", "--json", "--id-format", "refs", "workspace", "list")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(again, `"title" : "`+name+`"`); got != 1 {
		t.Fatalf("expected exactly one workspace named %q after reopening, got %d", name, got)
	}

	tree, _ := runCmux(ctx, "", "tree", "--workspace", ref)
	t.Logf("tree:\n%s", tree)

	if err := openURLInCmux(ctx, "https://example.com/?wt-live=1"); err != nil {
		t.Fatalf("openURLInCmux: %v", err)
	}
	surface := cmuxBrowserSurface.ref
	t.Logf("browser surface ref: %q", surface)
	if surface == "" {
		t.Error("could not parse a surface ref from `cmux browser open` output")
	} else {
		if err := openURLInCmux(ctx, "https://example.com/?wt-live=2"); err != nil {
			t.Errorf("navigating reused browser: %v", err)
		}
		t.Cleanup(func() { _, _ = runCmux(context.Background(), "", "close-surface", "--surface", surface) })
	}

	if err := notifyCmux(ctx, "wt-manager live test", "integration check"); err != nil {
		t.Errorf("notifyCmux: %v", err)
	}
}

func closeCmuxWorkspacesNamed(name string) {
	ctx := context.Background()
	listing, err := runCmux(ctx, "", "--json", "--id-format", "refs", "workspace", "list")
	if err != nil {
		return
	}
	for {
		ref := matchCmuxWorkspace(listing, name, "")
		if ref == "" {
			return
		}
		_, _ = runCmux(ctx, "", "close-workspace", "--workspace", ref)
		listing = strings.Replace(listing, `"`+ref+`"`, `"closed"`, -1)
	}
}
