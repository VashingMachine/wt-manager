package core

import (
	"reflect"
	"testing"
)

// Assures that keybindings written by hand in the config are read in one
// canonical spelling: lower-case modifiers in a fixed order, lower-case named
// keys with common aliases resolved, single characters keeping their case,
// and the keys of a sequence separated by one space.
func TestNormalizeKeybinding(t *testing.T) {
	for value, want := range map[string]string{
		"r":                "r",
		"R":                "R",
		" ctrl+p ":         "ctrl+p",
		"Ctrl+P":           "ctrl+p",
		"CONTROL+Alt+x":    "alt+ctrl+x",
		"shift+p":          "P",
		"alt+shift+p":      "alt+P",
		"Shift+Tab":        "shift+tab",
		"ctrl+shift+up":    "ctrl+shift+up",
		"Enter":            "enter",
		"Return":           "enter",
		"Escape":           "esc",
		"PageDown":         "pgdown",
		"Del":              "delete",
		"Spacebar":         "space",
		"F5":               "f5",
		"option+Left":      "alt+left",
		"+":                "+",
		"ctrl++":           "ctrl++",
		"g   L":            "g L",
		"Ctrl+K   Ctrl+S":  "ctrl+k ctrl+s",
		"":                 "",
		"   ":              "",
		"shift+1":          "shift+1",
		"/":                "/",
		"unknownmod+x":     "unknownmod+x",
		"ctrl+Enter":       "ctrl+enter",
		"g\tenter":         "g enter",
		"alt+ctrl+shift+a": "alt+ctrl+shift+a",
	} {
		if got := NormalizeKeybinding(value); got != want {
			t.Errorf("NormalizeKeybinding(%q) = %q, want %q", value, got, want)
		}
	}
}

// Assures that customized keybindings in the config are normalized on
// load: canonical key spelling, no blank or repeated keys, a command without
// keys kept as an empty list, and unknown commands left in place.
func TestNormalizeAppConfigCleansKeybindings(t *testing.T) {
	app := DefaultAppConfig()
	app.Keybindings = map[string][]string{
		" refresh ":    {" Ctrl+R ", "ctrl+r", "", "g   L"},
		"help":         nil,
		"":             {"x"},
		"profile:gone": {"1"},
	}
	normalized, changed, err := NormalizeAppConfig(app)
	if err != nil {
		t.Fatalf("NormalizeAppConfig() error = %v", err)
	}
	want := map[string][]string{"refresh": {"ctrl+r", "g L"}, "help": {}, "profile:gone": {"1"}}
	if !changed || !reflect.DeepEqual(normalized.Keybindings, want) {
		t.Fatalf("keybindings = %#v (changed %v), want %#v", normalized.Keybindings, changed, want)
	}

	again, changed, err := NormalizeAppConfig(normalized)
	if err != nil || changed || !reflect.DeepEqual(again.Keybindings, want) {
		t.Fatalf("normalizing twice changed the keybindings: %#v (changed %v, err %v)", again.Keybindings, changed, err)
	}

	app.Keybindings = map[string][]string{}
	if normalized, changed, _ = NormalizeAppConfig(app); !changed || normalized.Keybindings != nil {
		t.Fatalf("an empty keybindings block was kept: %#v", normalized.Keybindings)
	}
	app.Keybindings = nil
	if normalized, changed, _ = NormalizeAppConfig(app); changed || normalized.Keybindings != nil {
		t.Fatalf("a config without keybindings was changed: %#v", normalized.Keybindings)
	}
}
