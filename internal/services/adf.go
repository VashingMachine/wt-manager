package services

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Atlassian Document Format rendering.
//
// acli returns `description` and comment bodies as ADF JSON rather than text, so
// the ticket reader has to render it. The goal is readable plain text with light
// markdown, not a faithful ADF implementation: unknown node types recurse and
// emit their text leaves, so content is never silently dropped when Atlassian
// adds a node type we have not seen.

type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
	Content []adfNode      `json:"content"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// renderADF turns an ADF document into plain text. Raw input that is a bare JSON
// string (older Jira fields, and some acli responses) is passed through as-is.
func renderADF(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return strings.TrimSpace(plain)
	}

	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}

	var blocks []string
	renderADFBlock(doc, "", &blocks)
	return strings.TrimSpace(strings.Join(blocks, "\n"))
}

// renderADFBlock appends the rendered lines for a block-level node to blocks.
func renderADFBlock(node adfNode, indent string, blocks *[]string) {
	switch node.Type {
	case "doc", "", "expand", "nestedExpand", "bodiedExtension", "layoutSection", "layoutColumn":
		if title := adfStringAttr(node.Attrs, "title"); title != "" {
			appendADFBlock(blocks, indent+title)
		}
		renderADFChildBlocks(node.Content, indent, blocks)

	case "paragraph":
		if text := renderADFInlines(node.Content); strings.TrimSpace(text) != "" {
			appendADFBlock(blocks, prefixADFLines(text, indent))
		}

	case "heading":
		level := adfIntAttr(node.Attrs, "level", 1)
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		text := renderADFInlines(node.Content)
		if strings.TrimSpace(text) != "" {
			appendADFBlock(blocks, indent+strings.Repeat("#", level)+" "+text)
		}

	case "bulletList":
		for _, item := range node.Content {
			renderADFListItem(item, indent, "- ", blocks)
		}

	case "orderedList":
		start := adfIntAttr(node.Attrs, "order", 1)
		for i, item := range node.Content {
			renderADFListItem(item, indent, fmt.Sprintf("%d. ", start+i), blocks)
		}

	case "taskList":
		for _, item := range node.Content {
			marker := "[ ] "
			if adfStringAttr(item.Attrs, "state") == "DONE" {
				marker = "[x] "
			}
			renderADFListItem(item, indent, marker, blocks)
		}

	case "codeBlock":
		fence := indent + "```"
		if lang := adfStringAttr(node.Attrs, "language"); lang != "" {
			fence += lang
		}
		lines := []string{fence}
		for _, line := range strings.Split(renderADFInlines(node.Content), "\n") {
			lines = append(lines, indent+line)
		}
		lines = append(lines, indent+"```")
		appendADFBlock(blocks, strings.Join(lines, "\n"))

	case "blockquote", "panel":
		var inner []string
		renderADFChildBlocks(node.Content, "", &inner)
		body := strings.Join(inner, "\n")
		if panelType := adfStringAttr(node.Attrs, "panelType"); panelType != "" && body != "" {
			body = strings.ToUpper(panelType) + ": " + strings.TrimLeft(body, " ")
		}
		if strings.TrimSpace(body) == "" {
			return
		}
		appendADFBlock(blocks, prefixADFLines(body, indent+"> "))

	case "rule":
		appendADFBlock(blocks, indent+"---")

	case "table":
		renderADFTable(node, indent, blocks)

	case "mediaSingle", "mediaGroup", "media":
		if label := adfMediaLabel(node); label != "" {
			appendADFBlock(blocks, indent+label)
		}

	default:
		// Unknown block: keep whatever text it carries rather than dropping it.
		if text := renderADFInlines([]adfNode{node}); strings.TrimSpace(text) != "" {
			appendADFBlock(blocks, prefixADFLines(text, indent))
			return
		}
		renderADFChildBlocks(node.Content, indent, blocks)
	}
}

func renderADFChildBlocks(nodes []adfNode, indent string, blocks *[]string) {
	for _, child := range nodes {
		renderADFBlock(child, indent, blocks)
	}
}

// renderADFListItem renders one list item, hanging any nested blocks under the
// marker so nested lists stay visually attached to their parent.
func renderADFListItem(item adfNode, indent string, marker string, blocks *[]string) {
	var inner []string
	renderADFChildBlocks(item.Content, "", &inner)
	if len(inner) == 0 {
		return
	}
	hang := indent + strings.Repeat(" ", len([]rune(marker)))
	lines := strings.Split(strings.Join(inner, "\n"), "\n")
	rendered := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			rendered = append(rendered, indent+marker+line)
			continue
		}
		rendered = append(rendered, hang+line)
	}
	appendADFBlock(blocks, strings.Join(rendered, "\n"))
}

func renderADFTable(node adfNode, indent string, blocks *[]string) {
	var rows []string
	for _, row := range node.Content {
		if row.Type != "tableRow" {
			continue
		}
		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			var inner []string
			renderADFChildBlocks(cell.Content, "", &inner)
			cells = append(cells, strings.TrimSpace(strings.ReplaceAll(strings.Join(inner, " "), "\n", " ")))
		}
		if len(cells) == 0 {
			continue
		}
		rows = append(rows, indent+"| "+strings.Join(cells, " | ")+" |")
	}
	if len(rows) == 0 {
		return
	}
	appendADFBlock(blocks, strings.Join(rows, "\n"))
}

// renderADFInlines renders inline-level nodes into a single string.
func renderADFInlines(nodes []adfNode) string {
	var b strings.Builder
	for _, node := range nodes {
		switch node.Type {
		case "text":
			b.WriteString(applyADFMarks(node.Text, node.Marks))
		case "hardBreak":
			b.WriteString("\n")
		case "mention":
			name := adfStringAttr(node.Attrs, "text")
			if name == "" {
				name = adfStringAttr(node.Attrs, "id")
			}
			if name != "" {
				// exactly one leading @, however the attr was stored
				b.WriteString("@" + strings.TrimPrefix(name, "@"))
			}
		case "emoji":
			if short := adfStringAttr(node.Attrs, "shortName"); short != "" {
				b.WriteString(short)
			} else {
				b.WriteString(node.Text)
			}
		case "inlineCard", "blockCard", "embedCard":
			b.WriteString(adfStringAttr(node.Attrs, "url"))
		case "date":
			b.WriteString(adfStringAttr(node.Attrs, "timestamp"))
		case "status":
			if text := adfStringAttr(node.Attrs, "text"); text != "" {
				b.WriteString("[" + text + "]")
			}
		case "media":
			b.WriteString(adfMediaLabel(node))
		default:
			if node.Text != "" {
				b.WriteString(applyADFMarks(node.Text, node.Marks))
			}
			if len(node.Content) > 0 {
				b.WriteString(renderADFInlines(node.Content))
			}
		}
	}
	return b.String()
}

func applyADFMarks(text string, marks []adfMark) string {
	if text == "" {
		return ""
	}
	for _, mark := range marks {
		switch mark.Type {
		case "code":
			text = "`" + text + "`"
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "*" + text + "*"
		case "strike":
			text = "~~" + text + "~~"
		case "link":
			if href := adfStringAttr(mark.Attrs, "href"); href != "" && href != text {
				text = text + " (" + href + ")"
			}
		}
	}
	return text
}

func adfMediaLabel(node adfNode) string {
	name := adfStringAttr(node.Attrs, "alt")
	for _, child := range node.Content {
		if name != "" {
			break
		}
		name = adfStringAttr(child.Attrs, "alt")
		if name == "" {
			name = adfStringAttr(child.Attrs, "id")
		}
	}
	if name == "" {
		name = adfStringAttr(node.Attrs, "id")
	}
	if name == "" {
		return "[attachment]"
	}
	return "[attachment: " + name + "]"
}

func appendADFBlock(blocks *[]string, block string) {
	if len(*blocks) > 0 {
		*blocks = append(*blocks, "")
	}
	*blocks = append(*blocks, block)
}

func prefixADFLines(text string, prefix string) string {
	if prefix == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func adfStringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	if value, ok := attrs[key].(string); ok {
		return value
	}
	return ""
}

func adfIntAttr(attrs map[string]any, key string, fallback int) int {
	if attrs == nil {
		return fallback
	}
	if value, ok := attrs[key].(float64); ok {
		return int(value)
	}
	return fallback
}
