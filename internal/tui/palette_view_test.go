package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

// Selection fills both result lines through their right edge and never bleeds into adjacent entries.
func TestCommandPaletteSelectionBackgroundCoversWholeEntry(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
	view := renderPalettePanel(78, 24, "› search", paletteRenderingEntries(4), 1)
	buffer := cellbuf.NewBuffer(lipgloss.Width(view), lipgloss.Height(view))
	cellbuf.SetContent(buffer, view)
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		selected := strings.Contains(line, "Result 01") || strings.Contains(line, "description 01")
		for x := 2; x < 76; x++ {
			if selected && strings.Contains(line, "Result 01") && x >= 4 && x < 14 {
				continue // Category badges keep their own background.
			}
			cell := buffer.Cell(x, y)
			if cell == nil {
				t.Fatalf("missing cell at (%d, %d)", x, y)
			}
			highlighted := cell.Style.Bg == ansi.IndexedColor(60)
			if highlighted != selected {
				t.Fatalf("cell (%d, %d) background = %v, selected = %v; line: %q", x, y, cell.Style.Bg, selected, line)
			}
		}
	}
}

// The palette stays inside terminal bounds, even with long or multiline result labels.
func TestCommandPaletteRenderFitsTerminalBounds(t *testing.T) {
	entries := paletteRenderingEntries(30)
	entries[0].Title = strings.Repeat("長いworktree", 30) + "\nsecond line"
	entries[0].Description = strings.Repeat("long-path/", 50) + "\tmore"
	entries[0].Shortcut = "very-long-shortcut\nsecond line"
	for _, terminal := range []struct{ width, height int }{{30, 10}, {40, 12}, {60, 16}, {82, 24}, {120, 40}, {8, 4}, {5, 2}} {
		t.Run(fmt.Sprintf("%dx%d", terminal.width, terminal.height), func(t *testing.T) {
			view := renderPalettePanel(min(78, terminal.width), min(26, terminal.height), "› "+strings.Repeat("query", 30), entries, 0)
			if got := lipgloss.Height(view); got > terminal.height {
				t.Fatalf("palette height = %d, want <= %d\n%s", got, terminal.height, ansi.Strip(view))
			}
			for _, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > terminal.width {
					t.Fatalf("palette line width = %d, want <= %d: %q", got, terminal.width, ansi.Strip(line))
				}
			}
		})
	}
}

// Scrolling keeps the selected result visible and reports the displayed result range.
func TestCommandPaletteRenderScrollsToSelection(t *testing.T) {
	entries := paletteRenderingEntries(30)
	view := ansi.Strip(renderPalettePanel(50, 10, "› ", entries, 29))
	for _, want := range []string{"Result 29", "28–30 of 30 results", "↑↓", "enter", "esc"} {
		if !strings.Contains(view, want) {
			t.Fatalf("scrolled palette missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Result 00") {
		t.Fatalf("scrolled palette still displays first result:\n%s", view)
	}
}

// Spacious palettes show result descriptions and the Command-P / Control-P shortcut.
func TestCommandPaletteRenderShowsDescriptionsAndShortcut(t *testing.T) {
	view := ansi.Strip(renderPalettePanel(78, 24, "› search", paletteRenderingEntries(4), 0))
	for _, want := range []string{"Command Palette", "⌘P / Ctrl+P", "description 00", "4 results", "COMMANDS", "SETTINGS", "WORKTREES", "PROFILES"} {
		if !strings.Contains(view, want) {
			t.Fatalf("spacious palette missing %q in:\n%s", want, view)
		}
	}
}

// A search with no matches gives a clear empty state while preserving search and navigation.
func TestCommandPaletteRenderEmptyState(t *testing.T) {
	view := ansi.Strip(renderPalettePanel(78, 20, "› no-match", nil, 0))
	for _, want := range []string{"Command Palette", "no-match", "No matching commands or worktrees", "esc"} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty palette missing %q in:\n%s", want, view)
		}
	}
}

// Terminal color support produces distinct category colors and a highlighted selected result.
func TestCommandPaletteRenderColorsCategoriesAndSelection(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
	view := renderPalettePanel(78, 24, "› search", paletteRenderingEntries(4), 1)
	for _, background := range []string{"48;5;81", "48;5;171", "48;5;42", "48;5;214", "48;5;60"} {
		if !strings.Contains(view, background) {
			t.Fatalf("palette missing terminal color %q: %q", background, view)
		}
	}
	if !strings.Contains(ansi.Strip(view), "›  SETTINGS  Result 01") {
		t.Fatalf("selected result has no visible cursor:\n%s", ansi.Strip(view))
	}
}

// Ordinary result text uses a bright foreground against the palette's dark panel in either terminal theme.
func TestCommandPaletteRenderDefaultTextContrast(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
	view := renderPalettePanel(78, 24, "› search", paletteRenderingEntries(2), 0)
	if !strings.Contains(view, "38;5;252") || !strings.Contains(view, "48;5;235") {
		t.Fatalf("palette does not provide bright ordinary text on its dark panel: %q", view)
	}
	if !strings.Contains(ansi.Strip(view), "Result 01") {
		t.Fatalf("palette did not render an unselected result: %q", view)
	}
}

// Resizing the palette keeps a long search's cursor end visible and restores its prefix when widened.
func TestCommandPaletteLongSearchViewportFollowsResize(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.paletteOpen = true
	m.paletteInput.Focus()
	query := "LONG_QUERY_BEGIN" + strings.Repeat("x", 48) + "TAIL"
	m.paletteInput.SetValue(query)
	m.paletteInput.CursorEnd()
	for _, width := range []int{40, 120, 30, 80, 120} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = next.(model)
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "TAIL") {
			t.Fatalf("search cursor end disappeared at width %d:\n%s", width, view)
		}
		if width >= 80 && !strings.Contains(view, "LONG_QUERY_BEGIN") {
			t.Fatalf("widening did not restore the search prefix at width %d:\n%s", width, view)
		}
		if m.paletteInput.Value() != query || m.paletteInput.Position() != len([]rune(query)) {
			t.Fatalf("resizing changed search text or cursor at width %d", width)
		}
	}
}

func paletteRenderingEntries(count int) []paletteEntry {
	categories := []string{"Commands", "Settings", "Worktrees", "Profiles"}
	entries := make([]paletteEntry, count)
	for idx := range entries {
		entries[idx] = paletteEntry{
			ID:          fmt.Sprintf("entry-%d", idx),
			Title:       fmt.Sprintf("Result %02d", idx),
			Description: fmt.Sprintf("description %02d", idx),
			Category:    categories[idx%len(categories)],
			Shortcut:    "v",
		}
	}
	return entries
}
