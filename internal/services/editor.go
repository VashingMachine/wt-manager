package services

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

func normalizeEditorTool(editor EditorTool) (EditorTool, error) {
	editor.Name = strings.TrimSpace(editor.Name)
	editor.Command = strings.TrimSpace(editor.Command)
	if editor.Name == "" && editor.Command == "" {
		return EditorTool{Name: "VS Code", Command: "code"}, nil
	}
	if editor.Command == "" {
		return EditorTool{}, fmt.Errorf("editor %q command is required", editor.Name)
	}
	if editor.Name == "" {
		switch editor.Command {
		case "code":
			editor.Name = "VS Code"
		case "idea":
			editor.Name = "IntelliJ IDEA"
		case "cursor":
			editor.Name = "Cursor"
		default:
			editor.Name = editor.Command
		}
	}
	return editor, nil
}

func openEditorWorktree(ctx context.Context, wt Worktree, editor EditorTool) error {
	return openEditorPath(ctx, wt.Path, editor, func() error { return openVSCodeWorktree(ctx, wt) })
}

// openEditorFile opens one file in the editor; VS Code reuses its window
// rather than opening a new one for a single file.
func openEditorFile(ctx context.Context, path string, editor EditorTool) error {
	return openEditorPath(ctx, path, editor, func() error {
		_, err := runCommand(ctx, "", "code", path)
		if err == nil || runtime.GOOS != "darwin" {
			return err
		}
		_, fallbackErr := runCommand(ctx, "", "open", "-a", "Visual Studio Code", path)
		return fallbackErr
	})
}

func openEditorPath(ctx context.Context, path string, editor EditorTool, openVSCode func() error) error {
	editor, err := normalizeEditorTool(editor)
	if err != nil {
		return err
	}
	if editor.Command == "code" {
		err = openVSCode()
	} else {
		// Commands may include editor flags or a quoted executable path. The
		// path remains a single literal argument even with shell punctuation.
		_, err = runCommand(ctx, "", "/bin/sh", "-c", editor.Command+" "+shellQuote(path))
		if err != nil && editor.Command == "idea" && runtime.GOOS == "darwin" {
			// Toolbox scripts can outlive the installation they point to. Let
			// Launch Services find the currently installed application instead.
			_, fallbackErr := runCommand(ctx, "", "open", "-a", "IntelliJ IDEA", path)
			if fallbackErr == nil {
				err = nil
			} else {
				err = fmt.Errorf("idea launcher failed: %w; macOS application fallback failed: %w", err, fallbackErr)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", editor.Name, err)
	}
	return nil
}
