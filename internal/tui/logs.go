package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type sessionLogReader interface {
	ReadSessionLog() (path, content string, err error)
}

type sessionLogsResult struct {
	Path, Content string
	Request       int
	Err           error
}

func (m model) readSessionLogsCmd() tea.Cmd {
	request := m.logsRequest
	return func() tea.Msg {
		reader, ok := m.services.(sessionLogReader)
		if !ok {
			return sessionLogsResult{Request: request, Err: fmt.Errorf("session logging is unavailable")}
		}
		path, content, err := reader.ReadSessionLog()
		return sessionLogsResult{Path: path, Content: content, Request: request, Err: err}
	}
}

func (m model) openSessionLogs() (tea.Model, tea.Cmd) {
	m.logsOpen = true
	m.logsPath = ""
	m.logsContent = "Loading session logs…"
	m.logsViewport = viewport.New(max(1, m.width), max(1, m.height-3))
	m.logsRequest++
	m.resizeSessionLogs()
	return m, m.readSessionLogsCmd()
}

func (m *model) resizeSessionLogs() {
	atBottom := m.logsViewport.AtBottom()
	offset := m.logsViewport.YOffset
	m.logsViewport.Width = max(1, m.width)
	m.logsViewport.Height = max(1, m.height-3)
	// Raw logs stay intact on disk. Strip terminal escapes for display and
	// wrap instead of clipping long command lines and error messages.
	content := strings.ReplaceAll(ansi.Strip(m.logsContent), "\t", "    ")
	m.logsViewport.SetContent(ansi.Hardwrap(content, m.logsViewport.Width, true))
	if atBottom {
		m.logsViewport.GotoBottom()
	} else {
		m.logsViewport.SetYOffset(offset)
	}
}

func (m model) updateSessionLogs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.logsOpen = false
		return m, nil
	case "ctrl+c", "q":
		return m, tea.Quit
	case "r":
		m.logsRequest++
		return m, m.readSessionLogsCmd()
	case "home", "g":
		m.logsViewport.GotoTop()
		return m, nil
	case "end", "G":
		m.logsViewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.logsViewport, cmd = m.logsViewport.Update(msg)
	return m, cmd
}

func (m model) sessionLogsView() string {
	width, height := max(1, m.width), max(1, m.height)
	path := m.logsPath
	if path == "" {
		path = "Temporary execution log for this session"
	}
	lines := []string{
		titleStyle.Render(ansi.Truncate("Session logs", width, "")),
		subtleStyle.Render(ansi.Truncate(path, width, "…")),
	}
	if height > 3 {
		lines = append(lines, strings.Split(m.logsViewport.View(), "\n")...)
	}
	lines = append(lines, subtleStyle.Render(ansi.Truncate("esc close · r refresh · ↑↓/PgUp/PgDn scroll · home/end", width, "")))
	if len(lines) > height {
		lines = lines[:height]
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
}
