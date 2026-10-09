package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	paletteBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("81")).
				Padding(0, 1).Background(lipgloss.Color("235")).Foreground(lipgloss.Color("252"))
	paletteTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	paletteAccentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	paletteSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("60")).Foreground(lipgloss.Color("255"))
	paletteSearchStyle   = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("255"))
)

// renderCommandPaletteModal keeps the palette useful without overflowing small terminals.
func renderCommandPaletteModal(m model) string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 82
	}
	if height <= 0 {
		height = 28
	}
	if width >= 34 {
		width -= 4
	}
	if height >= 12 {
		height -= 2
	}
	width, height = min(78, width), min(26, height)

	input := m.paletteInput
	input.Prompt = "› "
	input.PromptStyle = paletteTitleStyle
	input.TextStyle = paletteSearchStyle
	input.PlaceholderStyle = mutedStyle
	input.Placeholder = "Search commands, settings, worktrees…"
	input.Width = max(1, width-8)
	input.SetCursor(input.Position())
	return renderPalettePanel(width, height, input.View(), m.paletteMatches(), m.paletteCursor)
}

func renderPalettePanel(width, height int, search string, entries []paletteEntry, cursor int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if width < 8 || height < 4 {
		return ansi.Truncate("Command Palette", width, "")
	}
	contentWidth, contentHeight := width-4, height-2
	lines := []string{paletteSplitLine(paletteTitleStyle.Render("✦ Command Palette"), paletteAccentStyle.Render("⌘P / Ctrl+P"), contentWidth)}
	roomy := contentWidth >= 56 && contentHeight >= 14
	if roomy {
		lines = append(lines, mutedStyle.Render("Commands, settings & worktrees — all in one place"), "")
	}
	lines = append(lines, paletteSearchStyle.Render(paletteFit(search, contentWidth)), paletteAccentStyle.Render(strings.Repeat("─", contentWidth)))

	// Reserve the bottom line for navigation, and show the match count when space allows.
	footer := renderKeyHints([]keyHint{{"↑↓", "move"}, {"enter", "run"}, {"esc", "close"}})
	if contentWidth < 40 {
		footer = renderKeyHints([]keyHint{{"↑↓", "move"}, {"↵", "run"}, {"esc", "close"}})
	}
	resultHeight := max(0, contentHeight-len(lines)-2)
	resultLines, first, last := renderPaletteResults(entries, cursor, contentWidth, resultHeight, roomy)
	lines = append(lines, resultLines...)
	if len(entries) == 0 {
		if resultHeight > 0 {
			lines = append(lines, mutedStyle.Render(paletteFit("No matching commands or worktrees.", contentWidth)))
			lines = append(lines, paletteAccentStyle.Render("0 results"))
		}
	} else if resultHeight > 0 {
		count := fmt.Sprintf("%d results", len(entries))
		if first > 0 || last < len(entries) {
			count = fmt.Sprintf("%d–%d of %d results", first+1, last, len(entries))
		}
		lines = append(lines, paletteAccentStyle.Render(count))
	}
	lines = append(lines, paletteFit(footer, contentWidth))
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	for idx := range lines {
		lines[idx] = paletteFit(lines[idx], contentWidth)
	}
	return paletteBorderStyle.Render(strings.Join(lines, "\n"))
}

func renderPaletteResults(entries []paletteEntry, cursor, width, height int, descriptions bool) ([]string, int, int) {
	if len(entries) == 0 || height <= 0 {
		return nil, 0, 0
	}
	cursor = max(0, min(cursor, len(entries)-1))
	rowHeight := 1
	if descriptions {
		rowHeight = 2
	}
	visible := max(1, height/rowHeight)
	first := max(0, cursor-visible/2)
	first = min(first, max(0, len(entries)-visible))
	last := min(len(entries), first+visible)
	lines := make([]string, 0, (last-first)*rowHeight)
	for idx := first; idx < last; idx++ {
		entry := entries[idx]
		selected := idx == cursor
		rowStyle := lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("252"))
		if selected {
			rowStyle = paletteSelectedStyle
		}
		prefix := "  "
		if selected {
			prefix = "› "
		}
		prefix = paletteTitleStyle.Background(rowStyle.GetBackground()).Render(prefix)
		category := paletteCategoryStyle(entry.Category).Render(" " + paletteCategoryLabel(entry.Category, width) + " ")
		shortcut := ""
		if width >= 46 && entry.Shortcut != "" {
			shortcut = paletteAccentStyle.Background(rowStyle.GetBackground()).Render(singleLineCell(entry.Shortcut))
		}
		remaining := max(0, width-lipgloss.Width(prefix)-lipgloss.Width(category)-1)
		titleWidth := remaining
		if shortcut != "" {
			titleWidth = max(0, titleWidth-lipgloss.Width(shortcut)-2)
		}
		title := ansi.Truncate(singleLineCell(entry.Title), titleWidth, "…")
		titleStyle := rowStyle
		if selected {
			titleStyle = titleStyle.Bold(true)
		}
		// Every fragment owns its background; nested ANSI resets otherwise erase
		// the selection after the category badge, title, or description.
		line := prefix + category + rowStyle.Render(" ") + titleStyle.Render(paletteFit(title, remaining-lipgloss.Width(shortcut))) + shortcut
		lines = append(lines, line)
		if descriptions {
			description := "  " + ansi.Truncate(singleLineCell(entry.Description), max(0, width-2), "…")
			description = subtleStyle.Background(rowStyle.GetBackground()).Render(paletteFit(description, width))
			lines = append(lines, description)
		}
	}
	return lines, first, last
}

func paletteCategoryStyle(category string) lipgloss.Style {
	color := "81"
	switch strings.ToLower(category) {
	case "settings":
		color = "171"
	case "worktrees":
		color = "42"
	case "profiles":
		color = "214"
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color(color))
}

func paletteCategoryLabel(category string, width int) string {
	if width < 40 {
		switch strings.ToLower(category) {
		case "commands":
			return "CMD"
		case "settings":
			return "SET"
		case "worktrees":
			return "TREE"
		case "profiles":
			return "REPO"
		}
	}
	return strings.ToUpper(ansi.Truncate(singleLineCell(category), max(0, min(10, width-6)), ""))
}

func paletteSplitLine(left, right string, width int) string {
	rightWidth := lipgloss.Width(right)
	if width < 40 || rightWidth+2 >= width {
		return paletteFit(left, width)
	}
	left = ansi.Truncate(left, width-rightWidth-2, "…")
	return left + strings.Repeat(" ", width-lipgloss.Width(left)-rightWidth) + right
}

func paletteFit(line string, width int) string {
	line = ansi.Truncate(line, max(0, width), "…")
	return line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
}
