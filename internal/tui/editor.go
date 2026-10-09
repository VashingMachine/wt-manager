package tui

import (
	"fmt"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var editorPresets = []core.EditorTool{
	{Name: "VS Code", Command: "code"},
	{Name: "IntelliJ IDEA", Command: "idea"},
	{Name: "Cursor", Command: "cursor"},
	{Name: "Zed", Command: "zed"},
	{Name: "PyCharm", Command: "pycharm"},
	{Name: "WebStorm", Command: "webstorm"},
}

func (m model) configuredEditor() core.EditorTool {
	editor := m.cfg.App.Editor
	editor.Name = strings.TrimSpace(editor.Name)
	editor.Command = strings.TrimSpace(editor.Command)
	if editor.Command == "" {
		return editorPresets[0]
	}
	if editor.Name == "" {
		editor.Name = editor.Command
		for _, preset := range editorPresets {
			if preset.Command == editor.Command {
				editor.Name = preset.Name
				break
			}
		}
	}
	return editor
}

func (m model) configuredEnterActionLabel() string {
	if normalizedEnterAction(m.cfg.App.DefaultEnterAction) == core.EnterActionVSCode {
		return m.configuredEditor().Name
	}
	return enterActionLabel(m.cfg.App.DefaultEnterAction)
}

func editorPresetIndex(editor core.EditorTool) int {
	for idx, preset := range editorPresets {
		if preset == editor {
			return idx
		}
	}
	return len(editorPresets)
}

func (m model) openEditorSelector() (tea.Model, tea.Cmd) {
	m.selectingEditor = true
	m.editingEditor = false
	m.editorCursor = editorPresetIndex(m.configuredEditor())
	m.statusError = false
	return m, nil
}

func (m model) updateEditorSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingEditor = false
		return m, m.setTransientStatus("Editor selection cancelled")
	case "up", "k":
		m.editorCursor = max(0, m.editorCursor-1)
	case "down", "j":
		m.editorCursor = min(len(editorPresets), m.editorCursor+1)
	case "enter":
		m.editorCursor = min(max(m.editorCursor, 0), len(editorPresets))
		m.selectingEditor = false
		if m.editorCursor == len(editorPresets) {
			return m.openCustomEditorInput()
		}
		return m.saveEditor(editorPresets[m.editorCursor])
	}
	return m, nil
}

func (m model) openCustomEditorInput() (tea.Model, tea.Cmd) {
	editor := m.configuredEditor()
	m.selectingEditor = false
	m.editorNameInput = textinput.New()
	m.editorNameInput.Prompt = "Name     "
	m.editorNameInput.Placeholder = "My IDE"
	m.editorNameInput.CharLimit = 80
	m.editorNameInput.Width = 48
	m.editorCommandInput = textinput.New()
	m.editorCommandInput.Prompt = "Command  "
	m.editorCommandInput.Placeholder = "code --new-window"
	m.editorCommandInput.CharLimit = 600
	m.editorCommandInput.Width = 48
	if editorPresetIndex(editor) == len(editorPresets) {
		m.editorNameInput.SetValue(editor.Name)
		m.editorCommandInput.SetValue(editor.Command)
	}
	m.editorField = 0
	m.editingEditor = true
	m.statusMessage = "Enter the editor name and launch command"
	m.statusError = false
	return m, m.editorNameInput.Focus()
}

func (m model) updateEditorInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.editingEditor = false
		m.editorNameInput.Blur()
		m.editorCommandInput.Blur()
		return m, m.setTransientStatus("Editor configuration cancelled")
	case "tab", "shift+tab", "up", "down":
		m.editorField = 1 - m.editorField
		return m.focusEditorField()
	case "enter":
		name := strings.TrimSpace(m.editorNameInput.Value())
		command := strings.TrimSpace(m.editorCommandInput.Value())
		if name == "" {
			m.statusMessage = "Editor name is required"
			m.statusError = true
			m.editorField = 0
			return m.focusEditorField()
		}
		if m.editorField == 0 {
			m.editorField = 1
			m.statusError = false
			return m.focusEditorField()
		}
		if command == "" {
			m.statusMessage = "Editor command is required"
			m.statusError = true
			return m, nil
		}
		m.editingEditor = false
		m.editorNameInput.Blur()
		m.editorCommandInput.Blur()
		return m.saveEditor(core.EditorTool{Name: name, Command: command})
	}
	return m.updateEditorInputMessage(msg)
}

func (m model) updateEditorInputMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.editorField == 0 {
		m.editorNameInput, cmd = m.editorNameInput.Update(msg)
	} else {
		m.editorCommandInput, cmd = m.editorCommandInput.Update(msg)
	}
	return m, cmd
}

func (m model) focusEditorField() (tea.Model, tea.Cmd) {
	m.editorNameInput.Blur()
	m.editorCommandInput.Blur()
	if m.editorField == 0 {
		return m, m.editorNameInput.Focus()
	}
	return m, m.editorCommandInput.Focus()
}

func (m model) saveEditor(editor core.EditorTool) (tea.Model, tea.Cmd) {
	m.selectingEditor = false
	m.editingEditor = false
	m.cfg.App.Editor = editor
	m.statusMessage = fmt.Sprintf("Saving editor %s...", editor.Name)
	m.statusError = false
	return m, m.persistConfig(fmt.Sprintf("Editor set to %s", editor.Name))
}

func renderEditorSelectorModal(m model) string {
	editor := m.configuredEditor()
	_, height := editorModalDimensions(m)
	contentHeight := max(0, height-2)
	body := []string{}
	roomy := contentHeight >= 14
	if roomy {
		body = append(body, "Choose which IDE opens when pressing v.", "")
	}
	visible := max(1, contentHeight-2-len(body))
	if roomy {
		visible -= 2
	}
	cursor := min(max(m.editorCursor, 0), len(editorPresets))
	first := min(max(0, cursor-visible/2), max(0, len(editorPresets)+1-visible))
	last := min(len(editorPresets)+1, first+visible)
	for idx := first; idx < last; idx++ {
		if idx == len(editorPresets) {
			body = append(body, selectorLine(idx == cursor, "Custom command  Configure another IDE", editorPresetIndex(editor) == len(editorPresets)))
			continue
		}
		preset := editorPresets[idx]
		label := fmt.Sprintf("%-15s %s", preset.Name, preset.Command)
		body = append(body, selectorLine(idx == cursor, label, preset == editor))
	}
	if roomy {
		body = append(body, "", purpleStyle.Render("CLI commands must be available in your PATH."))
	}
	return renderEditorModal(m, "Editor / IDE", body, selectHints)
}

func renderEditorInputModal(m model) string {
	width, height := editorModalDimensions(m)
	contentWidth, contentHeight := max(1, width-4), max(0, height-2)
	nameInput, commandInput := m.editorNameInput, m.editorCommandInput
	nameInput.Width = max(1, min(48, contentWidth-lipgloss.Width(nameInput.Prompt)-1))
	commandInput.Width = max(1, min(48, contentWidth-lipgloss.Width(commandInput.Prompt)-1))
	nameInput.SetCursor(nameInput.Position())
	commandInput.SetCursor(commandInput.Position())
	body := []string{}
	if contentHeight >= 14 {
		body = append(body, "Give your IDE a name and a command to launch it.", "")
	}
	body = append(body, nameInput.View(), commandInput.View())
	if m.statusError && contentHeight >= 6 {
		body = append(body, "", errorStyle.Render(m.statusMessage))
	}
	if contentHeight >= 10 {
		body = append(body,
			"",
			purpleStyle.Render("Examples")+subtleStyle.Render("  code --new-window"),
			subtleStyle.Render("          open -a 'IntelliJ IDEA'"),
			mutedStyle.Render("The selected worktree path is added automatically."),
		)
	}
	return renderEditorModal(m, "Custom Editor", body, []keyHint{{"tab / ↑↓", "field"}, {"enter", "continue / save"}, {"esc", "cancel"}})
}

func editorModalDimensions(m model) (int, int) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 74
	}
	if height <= 0 {
		height = 24
	}
	if width >= 34 {
		width -= 4
	}
	if height >= 12 {
		height -= 2
	}
	return min(70, width), min(22, height)
}

func renderEditorModal(m model, title string, body []string, hints []keyHint) string {
	width, height := editorModalDimensions(m)
	if width < 8 || height < 4 {
		return ansi.Truncate(title, max(0, width), "")
	}
	contentWidth, contentHeight := width-4, height-2
	if contentWidth < 40 {
		hints = []keyHint{{"↑↓", ""}, {"↵", ""}, {"esc", ""}}
		if m.editingEditor {
			hints[0].Key = "tab"
		}
	}
	lines := []string{titleStyle.Render(title)}
	lines = append(lines, body[:min(len(body), max(0, contentHeight-2))]...)
	lines = append(lines, renderKeyHints(hints))
	for idx, line := range lines {
		line = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(line)
		line = ansi.Truncate(line, contentWidth, "…")
		lines[idx] = line + strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(line)))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("141")).
		Background(lipgloss.Color("235")).Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}
