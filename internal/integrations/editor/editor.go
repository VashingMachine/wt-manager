// Package editor opens worktrees and files in the user's editor: VS Code by
// default, IntelliJ IDEA with a macOS fallback, or any configured command.
package editor

import (
	"context"
	"fmt"
	"runtime"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

// Launcher starts editors.
type Launcher struct{}

func New() *Launcher {
	return &Launcher{}
}

// OpenWorktree opens a worktree folder in the editor.
func (l *Launcher) OpenWorktree(ctx context.Context, wt core.Worktree, editor core.EditorTool) error {
	return openEditorWorktree(ctx, wt, editor)
}

// OpenFile opens a single file, such as the config, in the editor.
func (l *Launcher) OpenFile(ctx context.Context, path string, editor core.EditorTool) error {
	return openEditorFile(ctx, path, editor)
}

// OpenVSCodeWorktree opens a worktree in a new VS Code window.
func (l *Launcher) OpenVSCodeWorktree(ctx context.Context, wt core.Worktree) error {
	return openVSCodeWorktree(ctx, wt)
}

func openEditorWorktree(ctx context.Context, wt core.Worktree, editor core.EditorTool) error {
	return openEditorPath(ctx, wt.Path, editor, func() error { return openVSCodeWorktree(ctx, wt) })
}

// openEditorFile opens one file in the editor; VS Code reuses its window
// rather than opening a new one for a single file.
func openEditorFile(ctx context.Context, path string, editor core.EditorTool) error {
	return openEditorPath(ctx, path, editor, func() error {
		_, err := shell.Run(ctx, "", "code", path)
		if err == nil || runtime.GOOS != "darwin" {
			return err
		}
		_, fallbackErr := shell.Run(ctx, "", "open", "-a", "Visual Studio Code", path)
		return fallbackErr
	})
}

func openEditorPath(ctx context.Context, path string, editor core.EditorTool, openVSCode func() error) error {
	editor, err := core.NormalizeEditorTool(editor)
	if err != nil {
		return err
	}
	if editor.Command == "code" {
		err = openVSCode()
	} else {
		// Commands may include editor flags or a quoted executable path. The
		// path remains a single literal argument even with shell punctuation.
		_, err = shell.Run(ctx, "", "/bin/sh", "-c", editor.Command+" "+shell.Quote(path))
		if err != nil && editor.Command == "idea" && runtime.GOOS == "darwin" {
			// Toolbox scripts can outlive the installation they point to. Let
			// Launch Services find the currently installed application instead.
			_, fallbackErr := shell.Run(ctx, "", "open", "-a", "IntelliJ IDEA", path)
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
