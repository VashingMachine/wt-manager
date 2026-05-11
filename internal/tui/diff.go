package tui

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

const maxRenderedDiffLines = 600

func renderPullRequestDiff(diff string, width int) []string {
	width = max(1, width)
	diff = strings.TrimRight(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	if strings.TrimSpace(diff) == "" {
		return []string{styledWrappedLine("  not loaded", mutedStyle, width)}
	}

	rawLines := strings.Split(diff, "\n")
	lines := make([]string, 0, min(len(rawLines), maxRenderedDiffLines)+2)
	currentPath := ""
	for idx, raw := range rawLines {
		if idx == maxRenderedDiffLines {
			lines = append(lines, styledWrappedLine("... diff truncated; open the PR for the remaining lines", warnStyle, width))
			break
		}
		if path := diffPathFromLine(raw); path != "" {
			currentPath = path
		}
		lines = append(lines, renderDiffLine(raw, currentPath, width))
	}
	return lines
}

func renderDiffLine(raw string, currentPath string, width int) string {
	line := strings.ReplaceAll(raw, "\t", "    ")
	switch {
	case strings.HasPrefix(line, "diff --git "):
		return styleClampedLine(line, titleStyle, width)
	case strings.HasPrefix(line, "@@"):
		return styleClampedLine(line, infoStyle, width)
	case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
		return styleClampedLine(line, purpleStyle, width)
	case strings.HasPrefix(line, "+"):
		return renderCodeDiffLine("+", strings.TrimPrefix(line, "+"), currentPath, successStyle, width)
	case strings.HasPrefix(line, "-"):
		return renderCodeDiffLine("-", strings.TrimPrefix(line, "-"), currentPath, errorStyle, width)
	case strings.HasPrefix(line, " "):
		return renderCodeDiffLine(" ", strings.TrimPrefix(line, " "), currentPath, lipgloss.NewStyle(), width)
	case strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file ") || strings.HasPrefix(line, "deleted file "):
		return styleClampedLine(line, mutedStyle, width)
	default:
		return styleClampedLine(line, mutedStyle, width)
	}
}

func renderCodeDiffLine(prefix string, code string, path string, prefixStyle lipgloss.Style, width int) string {
	codeWidth := max(1, width-runewidthForPrefix(prefix))
	code = clampRunewidth(code, codeWidth)
	prefixText := prefixStyle.Render(prefix)
	if !canHighlightPath(path) {
		return prefixText + prefixStyle.Render(code)
	}
	return prefixText + syntaxHighlightCodeLine(path, code, lipgloss.NewStyle())
}

func syntaxHighlightCodeLine(path string, code string, fallback lipgloss.Style) string {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		return fallback.Render(code)
	}
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	style := styles.Get("github-dark")
	if style == nil {
		style = styles.Fallback
	}
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return fallback.Render(code)
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return fallback.Render(code)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func diffPathFromLine(line string) string {
	if strings.HasPrefix(line, "+++ b/") {
		return strings.TrimPrefix(line, "+++ b/")
	}
	if strings.HasPrefix(line, "--- a/") {
		return strings.TrimPrefix(line, "--- a/")
	}
	if strings.HasPrefix(line, "diff --git ") {
		parts := strings.Fields(line)
		if len(parts) >= 4 && strings.HasPrefix(parts[3], "b/") {
			return strings.TrimPrefix(parts[3], "b/")
		}
	}
	return ""
}

func canHighlightPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py":
		return true
	default:
		return false
	}
}

func styleClampedLine(line string, style lipgloss.Style, width int) string {
	return style.Render(clampRunewidth(line, width))
}

func runewidthForPrefix(prefix string) int {
	if prefix == "" {
		return 0
	}
	return 1
}
