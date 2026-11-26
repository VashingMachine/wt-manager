package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/testkit"
)

func TestRunAgentWithPrompt(t *testing.T) {
	bin := t.TempDir()
	agentPath := filepath.Join(bin, "fake-agent")
	testkit.WriteExecutable(t, agentPath, "#!/bin/sh\ninput=\"\"\nwhile IFS= read -r line; do input=\"$input $line\"; done\ncase \"$input\" in *risk*) echo 'risk found';; *) echo 'no risk';; esac\n")
	t.Setenv("PATH", bin)

	output, err := runAgentWithPrompt(context.Background(), "", "fake-agent", "tell me risk\n")
	if err != nil {
		t.Fatalf("runAgentWithPrompt() error = %v", err)
	}
	if strings.TrimSpace(output) != "risk found" {
		t.Fatalf("output = %q, want risk found", output)
	}
}
