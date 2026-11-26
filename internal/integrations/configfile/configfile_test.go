package configfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
)

func TestLoadAppConfigMissingDoesNotWrite(t *testing.T) {
	homeDir := t.TempDir()
	oldConfigPath := filepath.Join(homeDir, ".bwt-manager.json")
	if err := os.WriteFile(oldConfigPath, []byte(`{"defaultAgent":"copilot"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	appConfig, configPath, existed, err := loadAppConfig(homeDir)
	if err != nil {
		t.Fatalf("loadAppConfig() error = %v", err)
	}

	if existed {
		t.Fatal("existed = true, want false")
	}
	if configPath != filepath.Join(homeDir, ".wt-manager.json") {
		t.Fatalf("configPath = %q, want config under temp home", configPath)
	}
	if appConfig.DefaultAgent != "codex" {
		t.Fatalf("DefaultAgent = %q, want codex", appConfig.DefaultAgent)
	}
	if appConfig.DefaultEnterAction != core.EnterActionVSCode {
		t.Fatalf("DefaultEnterAction = %q, want vscode", appConfig.DefaultEnterAction)
	}
	for _, name := range []string{"codex", "opencode", "claude", "copilot"} {
		if core.FindAgent(appConfig.Agents, name) == nil {
			t.Fatalf("expected %s agent in %#v", name, appConfig.Agents)
		}
	}
	if len(appConfig.Profiles) != 0 {
		t.Fatalf("Profiles = %#v, want none when no git repo is inferred", appConfig.Profiles)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("new config should not be written before setup, stat error = %v", err)
	}
	if _, err := os.Stat(oldConfigPath); err != nil {
		t.Fatalf("old config should be untouched: %v", err)
	}
}

func TestLoadOrCreateAppConfigNormalizesExistingConfig(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	raw := `{
  "defaultProfile": "",
  "profiles": [
    {"name":"solen","repositoryPath":"~/solen/app","worktreesDir":"~/solen","remoteName":"","githubRepo":""}
  ],
  "defaultAgent":"missing",
  "agents":[{"name":"opencode","command":"opencode"}]
}`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	appConfig, _, _, err := loadAppConfig(homeDir)
	if err != nil {
		t.Fatalf("loadAppConfig() error = %v", err)
	}

	if appConfig.DefaultProfile != "solen" {
		t.Fatalf("DefaultProfile = %q, want solen", appConfig.DefaultProfile)
	}
	if appConfig.DefaultAgent != "opencode" {
		t.Fatalf("DefaultAgent = %q, want opencode", appConfig.DefaultAgent)
	}
	if appConfig.DefaultEnterAction != core.EnterActionVSCode {
		t.Fatalf("DefaultEnterAction = %q, want vscode", appConfig.DefaultEnterAction)
	}
	if appConfig.Profiles[0].RemoteName != "origin" {
		t.Fatalf("RemoteName = %q, want origin", appConfig.Profiles[0].RemoteName)
	}
	if len(appConfig.Agents) != 1 || appConfig.Agents[0].Name != "opencode" {
		t.Fatalf("Agents = %#v, want existing configured agents only", appConfig.Agents)
	}
}

func TestLoadOrCreateAppConfigRejectsInvalidProfiles(t *testing.T) {
	tests := map[string]string{
		"duplicate": `{"defaultProfile":"solen","profiles":[{"name":"solen","repositoryPath":"/repo","worktreesDir":"/w","remoteName":"origin"},{"name":"solen","repositoryPath":"/repo2","worktreesDir":"/w2","remoteName":"origin"}]}`,
		"invalid":   `{"defaultProfile":"broken","profiles":[{"name":"broken","repositoryPath":"","worktreesDir":"/w","remoteName":"origin"}]}`,
		"missing":   `{"defaultProfile":"missing","profiles":[{"name":"solen","repositoryPath":"/repo","worktreesDir":"/w","remoteName":"origin"}]}`,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			homeDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(homeDir, ".wt-manager.json"), []byte(raw), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, _, _, err := loadAppConfig(homeDir); err == nil {
				t.Fatal("loadAppConfig() error = nil, want error")
			}
		})
	}
}

// Loading legacy configs persists the editor default while preserving the legacy vscode Enter action.
func TestLoadAppConfigMigratesEditor(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	if err := os.WriteFile(configPath, []byte(`{"defaultEnterAction":"vscode"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, existed, err := loadAppConfig(homeDir)
	if err != nil || !existed {
		t.Fatalf("loadAppConfig() existed = %v, error = %v", existed, err)
	}
	if app.Editor != core.DefaultAppConfig().Editor || app.DefaultEnterAction != core.EnterActionVSCode {
		t.Fatalf("migrated config = %#v", app)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	var editor core.EditorTool
	if err := json.Unmarshal(saved["editor"], &editor); err != nil || editor != app.Editor {
		t.Fatalf("saved editor = %#v, error = %v", editor, err)
	}
}

// Custom IDE names and shell commands survive writing and loading configuration.
func TestAppConfigEditorRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	app := core.DefaultAppConfig()
	app.Editor = core.EditorTool{Name: "Custom IDE", Command: `"/Applications/My IDE.app/Contents/MacOS/ide" --new-window`}
	if err := writeAppConfig(filepath.Join(homeDir, ".wt-manager.json"), app); err != nil {
		t.Fatal(err)
	}
	loaded, _, _, err := loadAppConfig(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Editor != app.Editor {
		t.Fatalf("loaded Editor = %#v, want %#v", loaded.Editor, app.Editor)
	}
}

// Assures that keybindings survive being written to the config file and read
// back, including a command left without a key, and that a config without
// customized keys has no keybindings block at all.
func TestKeybindingsRoundTripThroughTheConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wt-manager.json")
	app := core.DefaultAppConfig()
	app.Keybindings = map[string][]string{"refresh": {"f5", "g r"}, "help": {}}
	if err := writeAppConfig(path, app); err != nil {
		t.Fatalf("writeAppConfig() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if text := string(data); !strings.Contains(text, `"keybindings": {`) || !strings.Contains(text, `"help": []`) || !strings.Contains(text, `"g r"`) {
		t.Fatalf("config file does not list the keybindings:\n%s", text)
	}
	read, err := readAppConfig(path)
	if err != nil {
		t.Fatalf("readAppConfig() error = %v", err)
	}
	if !reflect.DeepEqual(read.Keybindings, app.Keybindings) {
		t.Fatalf("read back %#v, want %#v", read.Keybindings, app.Keybindings)
	}

	if err := writeAppConfig(path, core.DefaultAppConfig()); err != nil {
		t.Fatalf("writeAppConfig() error = %v", err)
	}
	if data, _ = os.ReadFile(path); strings.Contains(string(data), "keybindings") {
		t.Fatalf("a config with default keys mentions keybindings:\n%s", data)
	}
}

// Assures that reading the config for a reload normalizes it without rewriting the file the user is editing.
func TestReadAppConfigDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wt-manager.json")
	raw := `{"defaultProfile":"shop","profiles":[{"name":"shop","repositories":[{"repositoryPath":"/src/api","worktreesDir":"/wt"}]}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app, err := readAppConfig(path)
	if err != nil {
		t.Fatalf("readAppConfig() error = %v", err)
	}
	if repository := app.Profiles[0].Repositories[0]; repository.Name != "api" || repository.RemoteName != "origin" {
		t.Fatalf("repository = %#v, want defaults filled in", repository)
	}
	if data, _ := os.ReadFile(path); string(data) != raw {
		t.Fatalf("file was rewritten: %s", data)
	}
	if err := os.WriteFile(path, []byte(`{"profiles":[`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := readAppConfig(path); err == nil {
		t.Fatal("readAppConfig() accepted invalid JSON")
	}
}
