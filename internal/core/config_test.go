package core

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeAppConfigDefaultEnterAction(t *testing.T) {
	for name, tc := range map[string]struct {
		raw     string
		want    string
		changed bool
	}{
		"missing": {raw: "", want: EnterActionVSCode, changed: true},
		"invalid": {raw: "bad", want: EnterActionVSCode, changed: true},
		"trimmed": {raw: " terminal ", want: EnterActionTerminal, changed: true},
		"valid":   {raw: EnterActionAgent, want: EnterActionAgent, changed: false},
	} {
		t.Run(name, func(t *testing.T) {
			appConfig := DefaultAppConfig()
			appConfig.DefaultEnterAction = tc.raw

			got, changed, err := NormalizeAppConfig(appConfig)
			if err != nil {
				t.Fatalf("NormalizeAppConfig() error = %v", err)
			}

			if got.DefaultEnterAction != tc.want {
				t.Fatalf("DefaultEnterAction = %q, want %q", got.DefaultEnterAction, tc.want)
			}
			if changed != tc.changed {
				t.Fatalf("changed = %v, want %v", changed, tc.changed)
			}
		})
	}
}

func TestNormalizeAppConfigAddsDefaultJiraFilters(t *testing.T) {
	app := AppConfig{
		DefaultProfile: "app",
		DefaultAgent:   "codex",
		Agents:         []AgentTool{{Name: "codex", Command: "codex"}},
		Profiles: []RepositoryProfile{{
			Name:           "app",
			RepositoryPath: "/tmp/app",
			WorktreesDir:   "/tmp/worktrees",
			RemoteName:     "origin",
			Jira: &JiraConfig{
				Boards: []JiraBoard{{ID: 123, Name: "App Board"}},
			},
		}},
	}
	normalized, changed, err := NormalizeAppConfig(app)
	if err != nil {
		t.Fatalf("NormalizeAppConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("NormalizeAppConfig() did not report JIRA filter migration")
	}
	jira := normalized.Profiles[0].Jira
	if jira == nil || !jira.FiltersConfigured || !jira.MineOnly || !jira.SprintOnly || !jira.OpenOnly {
		t.Fatalf("normalized JIRA filters = %#v", jira)
	}
}

// Assures that JIRA spaces, which have no board id, survive config
// normalization, that an unnamed space is named after its key, and that
// tickets default to every board and space together.
func TestNormalizeAppConfigKeepsJiraSpaces(t *testing.T) {
	app := AppConfig{
		DefaultProfile: "app",
		DefaultAgent:   "codex",
		Agents:         []AgentTool{{Name: "codex", Command: "codex"}},
		Profiles: []RepositoryProfile{{
			Name:           "app",
			RepositoryPath: "/tmp/app",
			WorktreesDir:   "/tmp/worktrees",
			RemoteName:     "origin",
			Jira: &JiraConfig{
				Boards: []JiraBoard{
					{URL: "https://site.atlassian.net/browse/OPS-42", Name: "Operations (OPS)", Projects: []string{"OPS"}},
					{Projects: []string{"WEB"}},
					{Name: "nothing to list"},
				},
			},
		}},
	}
	normalized, _, err := NormalizeAppConfig(app)
	if err != nil {
		t.Fatalf("NormalizeAppConfig() error = %v", err)
	}
	jira := normalized.Profiles[0].Jira
	if jira == nil || len(jira.Boards) != 2 || jira.Boards[0].Name != "Operations (OPS)" || jira.Boards[1].Name != "WEB" || jira.DefaultBoard != JiraAllSources {
		t.Fatalf("normalized JIRA = %+v", jira)
	}
}

// New installations use VS Code and keep the existing Enter action default.
func TestDefaultAppConfigEditor(t *testing.T) {
	app := DefaultAppConfig()
	if want := (EditorTool{Name: "VS Code", Command: "code"}); app.Editor != want {
		t.Fatalf("Editor = %#v, want %#v", app.Editor, want)
	}
	if app.DefaultEnterAction != EnterActionVSCode {
		t.Fatalf("DefaultEnterAction = %q, want vscode", app.DefaultEnterAction)
	}
}

// Editor settings normalize whitespace, supply missing defaults and labels, and reject a named editor without a command.
func TestNormalizeAppConfigEditor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		editor  EditorTool
		want    EditorTool
		changed bool
		wantErr bool
	}{
		{name: "missing", want: EditorTool{Name: "VS Code", Command: "code"}, changed: true},
		{name: "empty", editor: EditorTool{Name: " ", Command: "\t"}, want: EditorTool{Name: "VS Code", Command: "code"}, changed: true},
		{name: "configured", editor: EditorTool{Name: "IntelliJ IDEA", Command: "idea --new-window"}, want: EditorTool{Name: "IntelliJ IDEA", Command: "idea --new-window"}},
		{name: "trimmed", editor: EditorTool{Name: " Cursor ", Command: " cursor --reuse-window "}, want: EditorTool{Name: "Cursor", Command: "cursor --reuse-window"}, changed: true},
		{name: "command only", editor: EditorTool{Command: "idea"}, want: EditorTool{Name: "IntelliJ IDEA", Command: "idea"}, changed: true},
		{name: "custom command only", editor: EditorTool{Command: "my-ide --new-window"}, want: EditorTool{Name: "my-ide --new-window", Command: "my-ide --new-window"}, changed: true},
		{name: "missing command", editor: EditorTool{Name: "IntelliJ IDEA"}, wantErr: true},
		{name: "blank command", editor: EditorTool{Name: "Custom IDE", Command: " \t "}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := DefaultAppConfig()
			app.Editor = tc.editor
			got, changed, err := NormalizeAppConfig(app)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "command is required") {
					t.Fatalf("NormalizeAppConfig() error = %v, want missing editor command", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeAppConfig() error = %v", err)
			}
			if got.Editor != tc.want || changed != tc.changed {
				t.Fatalf("Editor = %#v, changed = %v; want %#v, %v", got.Editor, changed, tc.want, tc.changed)
			}
			if _, changedAgain, err := NormalizeAppConfig(got); err != nil || changedAgain {
				t.Fatalf("second normalization changed = %v, error = %v; want false, nil", changedAgain, err)
			}
		})
	}
}

// Assures that the sweep age defaults to a week and that a configured age is kept.
func TestNormalizeAppConfigSweepAge(t *testing.T) {
	normalized, changed, err := NormalizeAppConfig(AppConfig{})
	if err != nil || !changed || normalized.Sweep.OlderThanDays != DefaultSweepOlderThanDays {
		t.Fatalf("default sweep = %#v changed=%v err=%v", normalized.Sweep, changed, err)
	}
	if got := normalized.Sweep.OlderThan(); got != 7*24*time.Hour {
		t.Fatalf("OlderThan() = %s, want 7 days", got)
	}

	configured := DefaultAppConfig()
	configured.Sweep.OlderThanDays = 3
	normalized, changed, err = NormalizeAppConfig(configured)
	if err != nil || changed || normalized.Sweep.OlderThanDays != 3 {
		t.Fatalf("configured sweep = %#v changed=%v err=%v", normalized.Sweep, changed, err)
	}
}
