package services

import (
	"context"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// OpenBrowser shows a page in the terminal app's own browser when it has one,
// and in the system browser otherwise.
func (s *Service) OpenBrowser(ctx context.Context, url string) error {
	ctx = s.commandContext(ctx)
	if s.terminal.OpenURL(ctx, url) {
		return nil
	}
	return s.browser.Open(ctx, url)
}

func (s *Service) OpenVSCodeWorktree(ctx context.Context, wt core.Worktree) error {
	ctx = s.commandContext(ctx)
	return s.editor.OpenVSCodeWorktree(ctx, wt)
}

func (s *Service) OpenEditorWorktree(ctx context.Context, wt core.Worktree, editor core.EditorTool) error {
	ctx = s.commandContext(ctx)
	return s.editor.OpenWorktree(ctx, wt, editor)
}

func (s *Service) OpenEditorFile(ctx context.Context, path string, editor core.EditorTool) error {
	ctx = s.commandContext(ctx)
	return s.editor.OpenFile(ctx, path, editor)
}

func (s *Service) OpenAgent(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	ctx = s.commandContext(ctx)
	return s.terminal.OpenAgent(ctx, wt, agent)
}

func (s *Service) OpenTerminal(ctx context.Context, wt core.Worktree) error {
	ctx = s.commandContext(ctx)
	return s.terminal.OpenTerminal(ctx, wt)
}

func (s *Service) Notify(ctx context.Context, title string, body string) error {
	ctx = s.commandContext(ctx)
	return s.terminal.Notify(ctx, title, body)
}

func (s *Service) RunAgentWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error) {
	ctx = s.commandContext(ctx)
	return s.agents.RunWithPrompt(ctx, dir, command, prompt)
}
