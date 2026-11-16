package services

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Assures that customized keybindings in the config are normalized on
// load: canonical key spelling, no blank or repeated keys, a command without
// keys kept as an empty list, and unknown commands left in place.
func TestNormalizeAppConfigCleansKeybindings(t *testing.T) {
	app := defaultAppConfig()
	app.Keybindings = map[string][]string{
		" refresh ":    {" Ctrl+R ", "ctrl+r", "", "g   L"},
		"help":         nil,
		"":             {"x"},
		"profile:gone": {"1"},
	}
	normalized, changed, err := normalizeAppConfig(app)
	if err != nil {
		t.Fatalf("normalizeAppConfig() error = %v", err)
	}
	want := map[string][]string{"refresh": {"ctrl+r", "g L"}, "help": {}, "profile:gone": {"1"}}
	if !changed || !reflect.DeepEqual(normalized.Keybindings, want) {
		t.Fatalf("keybindings = %#v (changed %v), want %#v", normalized.Keybindings, changed, want)
	}

	again, changed, err := normalizeAppConfig(normalized)
	if err != nil || changed || !reflect.DeepEqual(again.Keybindings, want) {
		t.Fatalf("normalizing twice changed the keybindings: %#v (changed %v, err %v)", again.Keybindings, changed, err)
	}

	app.Keybindings = map[string][]string{}
	if normalized, changed, _ = normalizeAppConfig(app); !changed || normalized.Keybindings != nil {
		t.Fatalf("an empty keybindings block was kept: %#v", normalized.Keybindings)
	}
	app.Keybindings = nil
	if normalized, changed, _ = normalizeAppConfig(app); changed || normalized.Keybindings != nil {
		t.Fatalf("a config without keybindings was changed: %#v", normalized.Keybindings)
	}
}

// Assures that keybindings survive being written to the config file and read
// back, including a command left without a key, and that a config without
// customized keys has no keybindings block at all.
func TestKeybindingsRoundTripThroughTheConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wt-manager.json")
	app := defaultAppConfig()
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

	if err := writeAppConfig(path, defaultAppConfig()); err != nil {
		t.Fatalf("writeAppConfig() error = %v", err)
	}
	if data, _ = os.ReadFile(path); strings.Contains(string(data), "keybindings") {
		t.Fatalf("a config with default keys mentions keybindings:\n%s", data)
	}
}
