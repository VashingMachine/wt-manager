package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NormalizeKeybinding returns the canonical spelling of a keybinding, the way
// the config file stores it and the app shows it: the keys of a sequence
// separated by single spaces ("g l"), modifiers in a fixed order and lower
// case ("alt+ctrl+x"), named keys in lower case ("enter", "pgup") and common
// aliases resolved ("return" is "enter"). A single character keeps its case,
// since "p" and "P" are different keys.
func NormalizeKeybinding(value string) string {
	steps := strings.Fields(value)
	for idx, step := range steps {
		steps[idx] = normalizeKeyStep(step)
	}
	return strings.Join(steps, " ")
}

var keyModifiers = map[string]string{
	"alt": "alt", "opt": "alt", "option": "alt", "meta": "alt",
	"ctrl": "ctrl", "control": "ctrl",
	"shift": "shift",
}

var keyAliases = map[string]string{
	"return":   "enter",
	"escape":   "esc",
	"spacebar": "space",
	"pageup":   "pgup",
	"pagedown": "pgdown",
	"del":      "delete",
	"ins":      "insert",
	"bksp":     "backspace",
}

func normalizeKeyStep(step string) string {
	var alt, ctrl, shift bool
	key := step
	for {
		// A "+" that ends the step is the plus key itself, as in "ctrl++".
		idx := strings.Index(key, "+")
		if idx <= 0 || idx == len(key)-1 {
			break
		}
		modifier, ok := keyModifiers[strings.ToLower(key[:idx])]
		if !ok {
			break
		}
		switch modifier {
		case "alt":
			alt = true
		case "ctrl":
			ctrl = true
		case "shift":
			shift = true
		}
		key = key[idx+1:]
	}

	if utf8.RuneCountInString(key) == 1 {
		r, _ := utf8.DecodeRuneInString(key)
		switch {
		case ctrl:
			// Terminals do not tell ctrl+p from ctrl+P.
			key = string(unicode.ToLower(r))
		case shift && unicode.IsLetter(r):
			key = string(unicode.ToUpper(r))
			shift = false
		}
	} else {
		key = strings.ToLower(key)
		if alias, ok := keyAliases[key]; ok {
			key = alias
		}
	}

	var normalized strings.Builder
	if alt {
		normalized.WriteString("alt+")
	}
	if ctrl {
		normalized.WriteString("ctrl+")
	}
	if shift {
		normalized.WriteString("shift+")
	}
	normalized.WriteString(key)
	return normalized.String()
}
