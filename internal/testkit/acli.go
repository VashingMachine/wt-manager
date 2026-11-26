package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeACLIScript answers the space lookup, an APP board without an active
// sprint, and work item searches, and logs every call with its arguments
// joined by spaces.
const fakeACLIScript = `#!/bin/sh
echo "$*" >> "$WT_FAKE_ACLI_LOG"
case "$*" in
  "jira project view --key OPS --json") echo '{"key":"OPS","name":"Operations"}' ;;
  "jira board list-sprints --id 123 --state active --json") echo '{"sprints":[]}' ;;
  "jira workitem search --jql project IN (\"APP\") "*) echo '[{"key":"APP-7","fields":{"summary":"Board ticket"}},{"key":"OPS-43","fields":{"summary":"Also on the board"}}]' ;;
  "jira workitem search "*) echo '[{"key":"OPS-43","fields":{"summary":"Second","status":{"name":"To Do","statusCategory":{"key":"new"}}}},{"key":"OPS-41","fields":{"summary":"First"}}]' ;;
  *) echo "✗ Error: unexpected acli call: $*"; exit 2 ;;
esac
`

// InstallFakeACLI puts a scripted acli on PATH that answers the JIRA calls the
// app makes, and returns a function listing the invocations so far.
func InstallFakeACLI(t *testing.T) func() []string {
	t.Helper()
	bin := t.TempDir()
	WriteExecutable(t, filepath.Join(bin, "acli"), fakeACLIScript)
	log := filepath.Join(bin, "calls.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_FAKE_ACLI_LOG", log)
	return func() []string {
		data, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}
