package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	HomeDir       string
	ConfigPath    string
	App           AppConfig
	ActiveProfile RepositoryProfile
	RepoSlug      string
	ConfigExists  bool
	SetupNeeded   bool
	SetupReason   string
	SetupRepo     string
}

type AppConfig struct {
	DefaultProfile     string              `json:"defaultProfile"`
	Profiles           []RepositoryProfile `json:"profiles"`
	DefaultAgent       string              `json:"defaultAgent"`
	Agents             []AgentTool         `json:"agents"`
	DefaultEnterAction string              `json:"defaultEnterAction"`
	Editor             EditorTool          `json:"editor"`
	Sweep              SweepConfig         `json:"sweep"`
	// Keybindings lists the commands whose keys differ from the built-in
	// ones, by command ID. A command that is not listed keeps its default
	// keys; an empty list leaves it without a key. Each key is spelled the
	// way the app shows it ("r", "ctrl+p", "enter"), and keys pressed one
	// after another are separated by a space ("g l").
	Keybindings map[string][]string `json:"keybindings,omitempty"`
}

// DefaultSweepOlderThanDays is how long a worktree stays untouched before the
// sweep command offers it, unless the config says otherwise.
const DefaultSweepOlderThanDays = 7

// SweepConfig tunes the sweep command, which removes worktrees nobody has
// worked in for a while.
type SweepConfig struct {
	// OlderThanDays is how many days a worktree must have gone without a
	// commit, checkout, pull or rebase before sweep offers to remove it.
	OlderThanDays int `json:"olderThanDays"`
}

// OlderThan is the idle time after which a worktree is offered for sweeping.
func (c SweepConfig) OlderThan() time.Duration {
	days := c.OlderThanDays
	if days <= 0 {
		days = DefaultSweepOlderThanDays
	}
	return time.Duration(days) * 24 * time.Hour
}

const (
	EnterActionVSCode   = "vscode"
	EnterActionAgent    = "agent"
	EnterActionTerminal = "terminal"
)

type AgentTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

type EditorTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// DefaultAppConfig is the config a first run starts from.
func DefaultAppConfig() AppConfig {
	return AppConfig{
		DefaultAgent:       "codex",
		DefaultEnterAction: EnterActionVSCode,
		Editor:             EditorTool{Name: "VS Code", Command: "code"},
		Sweep:              SweepConfig{OlderThanDays: DefaultSweepOlderThanDays},
		Agents: []AgentTool{
			{Name: "codex", Command: "codex"},
			{Name: "opencode", Command: "opencode"},
			{Name: "claude", Command: "claude"},
			{Name: "copilot", Command: "copilot"},
		},
	}
}

// NormalizeAppConfig fills in defaults, trims what the user typed and
// validates the result. It reports whether anything changed, which is when
// the config file is worth writing back.
func NormalizeAppConfig(appConfig AppConfig) (AppConfig, bool, error) {
	changed := false
	defaults := DefaultAppConfig()
	editor, err := NormalizeEditorTool(appConfig.Editor)
	if err != nil {
		return AppConfig{}, false, err
	}
	if editor != appConfig.Editor {
		appConfig.Editor = editor
		changed = true
	}
	if normalized := normalizeEnterAction(appConfig.DefaultEnterAction); normalized != appConfig.DefaultEnterAction {
		appConfig.DefaultEnterAction = normalized
		changed = true
	}
	if appConfig.Sweep.OlderThanDays <= 0 {
		appConfig.Sweep.OlderThanDays = defaults.Sweep.OlderThanDays
		changed = true
	}
	if len(appConfig.Agents) == 0 {
		appConfig.Agents = defaults.Agents
		changed = true
	}
	if keybindings, keybindingsChanged := normalizeKeybindings(appConfig.Keybindings); keybindingsChanged {
		appConfig.Keybindings = keybindings
		changed = true
	}

	if FindAgent(appConfig.Agents, appConfig.DefaultAgent) == nil {
		appConfig.DefaultAgent = defaults.DefaultAgent
		if FindAgent(appConfig.Agents, appConfig.DefaultAgent) == nil && len(appConfig.Agents) > 0 {
			appConfig.DefaultAgent = appConfig.Agents[0].Name
		}
		changed = true
	}

	seenProfiles := map[string]struct{}{}
	for i := range appConfig.Profiles {
		profile := &appConfig.Profiles[i]
		profile.Name = strings.TrimSpace(profile.Name)
		profile.RepositoryPath = strings.TrimSpace(profile.RepositoryPath)
		profile.WorktreesDir = strings.TrimSpace(profile.WorktreesDir)
		profile.RemoteName = strings.TrimSpace(profile.RemoteName)
		profile.GitHubRepo = strings.TrimSpace(profile.GitHubRepo)
		repositoriesChanged, err := normalizeProfileRepositories(profile)
		if err != nil {
			return AppConfig{}, false, err
		}
		if repositoriesChanged {
			changed = true
		}
		if normalizeJiraConfig(profile) {
			changed = true
		}
		if err := validateProfile(*profile); err != nil {
			return AppConfig{}, false, err
		}
		if _, ok := seenProfiles[profile.Name]; ok {
			return AppConfig{}, false, fmt.Errorf("duplicate profile %q", profile.Name)
		}
		seenProfiles[profile.Name] = struct{}{}
	}

	if appConfig.DefaultProfile != "" {
		appConfig.DefaultProfile = strings.TrimSpace(appConfig.DefaultProfile)
		if FindProfile(appConfig.Profiles, appConfig.DefaultProfile) == nil {
			return AppConfig{}, false, fmt.Errorf("defaultProfile %q does not match any profile", appConfig.DefaultProfile)
		}
	}
	if appConfig.DefaultProfile == "" && len(appConfig.Profiles) > 0 {
		appConfig.DefaultProfile = appConfig.Profiles[0].Name
		changed = true
	}

	return appConfig, changed, nil
}

func normalizeEnterAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case EnterActionVSCode:
		return EnterActionVSCode
	case EnterActionAgent:
		return EnterActionAgent
	case EnterActionTerminal:
		return EnterActionTerminal
	default:
		return EnterActionVSCode
	}
}

// FindAgent returns the agent with the given name, or nil.
func FindAgent(agents []AgentTool, name string) *AgentTool {
	for i := range agents {
		if agents[i].Name == name {
			return &agents[i]
		}
	}
	return nil
}

// FindProfile returns the project with the given name, or nil.
func FindProfile(profiles []RepositoryProfile, name string) *RepositoryProfile {
	for i := range profiles {
		if profiles[i].Name == name {
			return &profiles[i]
		}
	}
	return nil
}

// NormalizeEditorTool defaults an unset editor to VS Code and names an editor
// that only gives a command.
func NormalizeEditorTool(editor EditorTool) (EditorTool, error) {
	editor.Name = strings.TrimSpace(editor.Name)
	editor.Command = strings.TrimSpace(editor.Command)
	if editor.Name == "" && editor.Command == "" {
		return EditorTool{Name: "VS Code", Command: "code"}, nil
	}
	if editor.Command == "" {
		return EditorTool{}, fmt.Errorf("editor %q command is required", editor.Name)
	}
	if editor.Name == "" {
		switch editor.Command {
		case "code":
			editor.Name = "VS Code"
		case "idea":
			editor.Name = "IntelliJ IDEA"
		case "cursor":
			editor.Name = "Cursor"
		default:
			editor.Name = editor.Command
		}
	}
	return editor, nil
}

// DefaultConfigPath is where the config file lives.
func DefaultConfigPath(homeDir string) string {
	return filepath.Join(homeDir, ".wt-manager.json")
}
