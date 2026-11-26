package editor

import (
	"context"
	"runtime"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func openVSCodeWorktree(ctx context.Context, wt core.Worktree) error {
	_, err := shell.Run(ctx, "", "code", "--new-window", wt.Path)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return err
	}
	_, fallbackErr := shell.Run(ctx, "", "open", "-a", "Visual Studio Code", wt.Path)
	return fallbackErr
}
