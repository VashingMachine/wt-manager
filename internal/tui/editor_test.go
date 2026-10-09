package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Assures that editor selection starts on the current preset or the custom command entry.
func TestEditorSelectorPreselectsConfiguredEditor(t *testing.T) {
	for _, editor := range append(append([]core.EditorTool(nil), editorPresets...), core.EditorTool{Name: "My IDE", Command: "my-ide --new-window"}) {
		t.Run(editor.Name, func(t *testing.T) {
			m, _ := enterActionTestModel()
			m.cfg.App.Editor = editor
			next, _ := m.openEditorSelector()
			got := next.(model)
			if !got.selectingEditor || got.editingEditor || got.editorCursor != editorPresetIndex(editor) {
				t.Fatalf("selector state = selecting %v, editing %v, cursor %d", got.selectingEditor, got.editingEditor, got.editorCursor)
			}
		})
	}
}

// Assures that each IDE preset is persisted while the other application settings are preserved.
func TestEditorSelectorPersistsPreset(t *testing.T) {
	for idx, editor := range editorPresets {
		t.Run(editor.Name, func(t *testing.T) {
			m, spy := enterActionTestModel()
			m.selectingEditor = true
			m.editorCursor = idx
			next, cmd := m.updateEditorSelector(tea.KeyMsg{Type: tea.KeyEnter})
			got := next.(model)
			if got.selectingEditor || got.cfg.App.Editor != editor {
				t.Fatalf("selected editor = %#v, selector open = %v", got.cfg.App.Editor, got.selectingEditor)
			}
			if cmd == nil {
				t.Fatal("preset selection did not return a save command")
			}
			result, ok := cmd().(actionResult)
			if !ok || result.Err != nil || result.Message != "Editor set to "+editor.Name {
				t.Fatalf("save result = %#v, want successful editor confirmation", result)
			}
			if spy.saveCount != 1 || spy.saved.Editor != editor || spy.saved.DefaultAgent != m.cfg.App.DefaultAgent {
				t.Fatalf("saved config = %#v, save count = %d", spy.saved, spy.saveCount)
			}
		})
	}
}

// Assures that editor selector navigation stops at the first preset and last custom option.
func TestEditorSelectorNavigationBounds(t *testing.T) {
	m, _ := enterActionTestModel()
	for i := 0; i < len(editorPresets)+3; i++ {
		next, _ := m.updateEditorSelector(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
	}
	if m.editorCursor != len(editorPresets) {
		t.Fatalf("last editor cursor = %d, want %d", m.editorCursor, len(editorPresets))
	}
	for i := 0; i < len(editorPresets)+3; i++ {
		next, _ := m.updateEditorSelector(teaKey("k"))
		m = next.(model)
	}
	if m.editorCursor != 0 {
		t.Fatalf("first editor cursor = %d, want 0", m.editorCursor)
	}
}

// Assures that Esc discards both a preset choice and edits to a custom IDE without saving.
func TestEditorConfigurationEscCancelsWithoutSaving(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "selector", true: "custom"}[custom], func(t *testing.T) {
			m, spy := enterActionTestModel()
			original := core.EditorTool{Name: "IntelliJ IDEA", Command: "idea"}
			m.cfg.App.Editor = original
			m.selectingEditor = true
			if custom {
				next, _ := m.openCustomEditorInput()
				m = next.(model)
				m.editorNameInput.SetValue("Changed")
				m.editorCommandInput.SetValue("changed-ide")
			}
			var next tea.Model
			var cmd tea.Cmd
			if custom {
				next, cmd = m.updateEditorInput(tea.KeyMsg{Type: tea.KeyEsc})
			} else {
				next, cmd = m.updateEditorSelector(tea.KeyMsg{Type: tea.KeyEsc})
			}
			got := next.(model)
			if got.editingEditor || got.selectingEditor || got.cfg.App.Editor != original {
				t.Fatalf("cancelled editor state = %#v", got.cfg.App.Editor)
			}
			if cmd != nil {
				cmd()
			}
			if spy.saveCount != 0 {
				t.Fatalf("cancellation saved config %d times", spy.saveCount)
			}
		})
	}
}

// Assures that choosing Custom opens a form and preserves an existing custom launch command.
func TestEditorCustomSelectionOpensPrefilledForm(t *testing.T) {
	m, spy := enterActionTestModel()
	editor := core.EditorTool{Name: "My IDE", Command: "open -a 'My IDE'"}
	m.cfg.App.Editor = editor
	m.selectingEditor = true
	m.editorCursor = len(editorPresets)
	next, _ := m.updateEditorSelector(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.selectingEditor || !got.editingEditor || !got.editorNameInput.Focused() || got.editorCommandInput.Focused() {
		t.Fatal("custom selection did not focus the editor name form")
	}
	if got.editorNameInput.Value() != editor.Name || got.editorCommandInput.Value() != editor.Command || spy.saveCount != 0 {
		t.Fatalf("custom form = %q / %q, saves = %d", got.editorNameInput.Value(), got.editorCommandInput.Value(), spy.saveCount)
	}
}

// Assures that Tab, Shift-Tab, and the arrow keys move focus between the custom editor fields.
func TestEditorCustomFieldNavigation(t *testing.T) {
	for _, keyType := range []tea.KeyType{tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown} {
		t.Run(tea.KeyMsg{Type: keyType}.String(), func(t *testing.T) {
			m, _ := enterActionTestModel()
			next, _ := m.openCustomEditorInput()
			m = next.(model)
			next, _ = m.updateEditorInput(tea.KeyMsg{Type: keyType})
			m = next.(model)
			if m.editorField != 1 || m.editorNameInput.Focused() || !m.editorCommandInput.Focused() {
				t.Fatal("field navigation did not focus the command")
			}
			next, _ = m.updateEditorInput(tea.KeyMsg{Type: keyType})
			m = next.(model)
			if m.editorField != 0 || !m.editorNameInput.Focused() || m.editorCommandInput.Focused() {
				t.Fatal("field navigation did not return to the name")
			}
		})
	}
}

// Assures that custom editor fields require nonempty values before a configuration can be saved.
func TestEditorCustomRequiresNameAndCommand(t *testing.T) {
	m, spy := enterActionTestModel()
	next, _ := m.openCustomEditorInput()
	m = next.(model)
	m.editorNameInput.SetValue("   ")
	next, _ = m.updateEditorInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.editingEditor || !m.statusError || m.statusMessage != "Editor name is required" || m.editorField != 0 {
		t.Fatal("empty editor name was accepted")
	}
	m.editorNameInput.SetValue("My IDE")
	next, _ = m.updateEditorInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.editorField != 1 || m.statusError {
		t.Fatal("enter did not advance a valid editor name to the command")
	}
	m.editorCommandInput.SetValue("   ")
	next, cmd := m.updateEditorInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.editingEditor || !m.statusError || m.statusMessage != "Editor command is required" || cmd != nil || spy.saveCount != 0 {
		t.Fatal("empty editor command was accepted or saved")
	}
}

// Assures that custom launch commands retain flags and quoted application names when saved.
func TestEditorCustomPersistsCommand(t *testing.T) {
	for _, command := range []string{"my-ide --new-window", "open -a 'IntelliJ IDEA'"} {
		t.Run(command, func(t *testing.T) {
			m, spy := enterActionTestModel()
			next, _ := m.openCustomEditorInput()
			m = next.(model)
			next, _ = m.updateEditorInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" My IDE ")})
			m = next.(model)
			next, _ = m.updateEditorInput(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(model)
			next, _ = m.updateEditorInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" " + command + " ")})
			m = next.(model)
			next, cmd := m.updateEditorInput(tea.KeyMsg{Type: tea.KeyEnter})
			got := next.(model)
			want := core.EditorTool{Name: "My IDE", Command: command}
			if got.editingEditor || got.cfg.App.Editor != want || cmd == nil {
				t.Fatalf("custom editor = %#v, editing = %v, command = %v", got.cfg.App.Editor, got.editingEditor, cmd)
			}
			if result, ok := cmd().(actionResult); !ok || result.Err != nil || spy.saved.Editor != want || spy.saveCount != 1 {
				t.Fatalf("custom save result = %#v, saved editor = %#v", result, spy.saved.Editor)
			}
		})
	}
}

// Assures that editor persistence errors are returned to the application's action error handler.
func TestEditorSaveReportsFailure(t *testing.T) {
	m, spy := enterActionTestModel()
	m.services = editorWriteFailureServices{enterActionSpyServices: spy}
	_, cmd := m.saveEditor(editorPresets[1])
	result, ok := cmd().(actionResult)
	if !ok || result.Err == nil || !strings.Contains(result.Err.Error(), "cannot save") {
		t.Fatalf("failed editor save = %#v, want action error", result)
	}
}

type editorWriteFailureServices struct {
	*enterActionSpyServices
}

func (editorWriteFailureServices) WriteAppConfig(string, AppConfig) error {
	return errors.New("cannot save")
}

// Assures that legacy editor Enter actions display the selected IDE and an absent setting defaults to VS Code.
func TestConfiguredEditorAndEnterLabels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		editor core.EditorTool
		want   core.EditorTool
	}{
		{name: "default", want: editorPresets[0]},
		{name: "preset command", editor: core.EditorTool{Command: "idea"}, want: editorPresets[1]},
		{name: "custom command", editor: core.EditorTool{Command: "custom"}, want: core.EditorTool{Name: "custom", Command: "custom"}},
		{name: "custom name", editor: core.EditorTool{Name: " My IDE ", Command: " custom --flag "}, want: core.EditorTool{Name: "My IDE", Command: "custom --flag"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := enterActionTestModel()
			m.cfg.App.Editor = tc.editor
			if got := m.configuredEditor(); got != tc.want {
				t.Fatalf("configuredEditor() = %#v, want %#v", got, tc.want)
			}
			if got := m.configuredEnterActionLabel(); got != tc.want.Name {
				t.Fatalf("configuredEnterActionLabel() = %q, want %q", got, tc.want.Name)
			}
			for _, action := range []string{core.EnterActionAgent, core.EnterActionTerminal} {
				m.cfg.App.DefaultEnterAction = action
				if got := m.configuredEnterActionLabel(); got != enterActionLabel(action) {
					t.Fatalf("configuredEnterActionLabel(%s) = %q", action, got)
				}
			}
		})
	}
}

// Assures that editor forms show available IDEs, launch examples, keyboard hints, and validation errors.
func TestEditorConfigurationModalGuidance(t *testing.T) {
	m, _ := enterActionTestModel()
	view := renderEditorSelectorModal(m)
	for _, editor := range editorPresets {
		if !strings.Contains(view, editor.Name) || !strings.Contains(view, editor.Command) {
			t.Fatalf("editor selector is missing %q", editor.Name)
		}
	}
	if !strings.Contains(view, "Custom command") || !strings.Contains(view, "pressing v") {
		t.Fatal("editor selector is missing custom configuration or opener guidance")
	}
	next, _ := m.openCustomEditorInput()
	m = next.(model)
	m.statusError = true
	m.statusMessage = "Editor name is required"
	view = renderEditorInputModal(m)
	for _, expected := range []string{"Custom Editor", "code --new-window", "open -a 'IntelliJ IDEA'", "path is added automatically", "continue / save", "cancel", m.statusMessage} {
		if !strings.Contains(view, expected) {
			t.Fatalf("custom editor modal is missing %q", expected)
		}
	}
}

// Assures that IDE selectors and custom forms fit the terminal even with long Unicode values and errors.
func TestEditorConfigurationFitsTerminalBounds(t *testing.T) {
	for _, terminal := range []struct{ width, height int }{{30, 10}, {40, 14}, {60, 16}, {82, 24}, {120, 40}, {8, 4}, {5, 2}} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/custom=%t", terminal.width, terminal.height, custom), func(t *testing.T) {
				m, _ := enterActionTestModel()
				m.width, m.height = terminal.width, terminal.height
				m.selectingEditor = true
				m.editorCursor = len(editorPresets)
				if custom {
					next, _ := m.openCustomEditorInput()
					m = next.(model)
					m.editorNameInput.SetValue(strings.Repeat("長いIDE", 40))
					m.editorCommandInput.SetValue(strings.Repeat("long-command --flag ", 40))
					m.statusError = true
					m.statusMessage = strings.Repeat("long error ", 40) + "\nsecond line"
				}
				view := m.View()
				if got := lipgloss.Height(view); got > terminal.height {
					t.Fatalf("editor modal height = %d, want <= %d\n%s", got, terminal.height, ansi.Strip(view))
				}
				for _, line := range strings.Split(view, "\n") {
					if got := lipgloss.Width(line); got > terminal.width {
						t.Fatalf("editor modal line width = %d, want <= %d: %q", got, terminal.width, ansi.Strip(line))
					}
				}
			})
		}
	}
}

// Assures that a short IDE selector keeps its highlighted option and essential navigation keys visible.
func TestEditorSelectorScrollsInShortTerminal(t *testing.T) {
	m, _ := enterActionTestModel()
	m.width, m.height = 30, 10
	m.editorCursor = len(editorPresets)
	view := ansi.Strip(renderEditorSelectorModal(m))
	for _, expected := range []string{icons.Pointer, "Custom command", "↑↓", "↵", "esc"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("short selector is missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "VS Code") {
		t.Fatalf("short selector did not scroll down to its selected custom entry:\n%s", view)
	}
}

// Assures that narrow custom editor forms retain both fields, validation feedback, and save/cancel hints.
func TestEditorCustomNarrowFormKeepsFieldsAndValidation(t *testing.T) {
	m, _ := enterActionTestModel()
	m.width, m.height = 40, 14
	next, _ := m.openCustomEditorInput()
	m = next.(model)
	m.editorNameInput.SetValue("My IDE")
	m.statusError = true
	m.statusMessage = "Editor command is required"
	view := ansi.Strip(renderEditorInputModal(m))
	for _, expected := range []string{"Name", "Command", "My IDE", m.statusMessage, "tab", "↵", "esc"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("narrow custom form is missing %q:\n%s", expected, view)
		}
	}
}

// Assures that custom editor inputs continue blinking after non-key cursor messages arrive.
func TestEditorCustomHandlesCursorBlinkMessages(t *testing.T) {
	for _, field := range []int{0, 1} {
		t.Run(fmt.Sprintf("field-%d", field), func(t *testing.T) {
			m, _ := enterActionTestModel()
			next, _ := m.openCustomEditorInput()
			m = next.(model)
			m.editorField = field
			next, _ = m.focusEditorField()
			m = next.(model)
			m.editorNameInput.Cursor.BlinkSpeed = time.Millisecond
			m.editorCommandInput.Cursor.BlinkSpeed = time.Millisecond
			next, cmd := m.Update(textinput.Blink())
			m = next.(model)
			if cmd == nil {
				t.Fatal("custom editor input did not schedule its cursor blink")
			}
			next, _ = m.Update(cmd())
			m = next.(model)
			blink := m.editorNameInput.Cursor.Blink
			if field == 1 {
				blink = m.editorCommandInput.Cursor.Blink
			}
			if !blink {
				t.Fatal("focused custom editor cursor did not blink")
			}
		})
	}
}

// Assures that shrinking the terminal preserves the command text around the active input cursor.
func TestEditorCustomNarrowFormKeepsCursorTextVisible(t *testing.T) {
	m, _ := enterActionTestModel()
	m.width, m.height = 40, 14
	next, _ := m.openCustomEditorInput()
	m = next.(model)
	m.editorNameInput.SetValue("My IDE")
	m.editorCommandInput.SetValue(strings.Repeat("long-command ", 15) + "TAIL-MARKER")
	m.editorCommandInput.CursorEnd()
	m.editorField = 1
	next, _ = m.focusEditorField()
	m = next.(model)
	view := ansi.Strip(renderEditorInputModal(m))
	if !strings.Contains(view, "TAIL-MARKER") {
		t.Fatalf("narrow command field lost the text around its cursor:\n%s", view)
	}
}
