package core

import "testing"

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
