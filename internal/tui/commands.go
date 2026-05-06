package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

const refreshTimeout = 45 * time.Second
const prDetailFetchDelay = 200 * time.Millisecond

type loadResult struct {
	Worktrees []Worktree
	Warning   string
	Err       error
}

type remotePRListResult struct {
	PullRequests []RemotePullRequest
	CurrentUser  string
	Err          error
}

func loadWorktreesCmd(services core.WorktreeService, cfg Config, showAll bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		worktrees, warning, err := services.CollectWorktrees(ctx, cfg, showAll)
		return loadResult{Worktrees: worktrees, Warning: warning, Err: err}
	}
}

func loadRemotePullRequestsCmd(services core.PullRequestService, cfg Config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		pullRequests, err := services.LoadRemotePullRequests(ctx, cfg)
		currentUser := ""
		if err == nil {
			currentUser, _ = services.LoadGitHubCurrentUser(ctx, cfg)
		}
		return remotePRListResult{PullRequests: pullRequests, CurrentUser: currentUser, Err: err}
	}
}

func loadRemotePullRequestDetailCmd(services core.PullRequestService, ctx context.Context, cfg Config, number int, requestID int) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-time.After(prDetailFetchDelay):
		case <-ctx.Done():
			return remotePRDetailResult{Number: number, RequestID: requestID, Err: ctx.Err()}
		}

		pullRequest, err := services.LoadRemotePullRequestDetail(ctx, cfg, number)
		return remotePRDetailResult{Number: number, RequestID: requestID, PullRequest: pullRequest, Err: err}
	}
}

func askPullRequestAgentCmd(services core.AgentService, cfg Config, agent AgentTool, pullRequest RemotePullRequest, question string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		prompt := services.BuildPullRequestChatPrompt(pullRequest, question)
		answer, err := services.RunAgentWithPrompt(ctx, cfg.ActiveProfile.RepositoryPath, agent.Command, prompt)
		return askPRResult{Question: question, Answer: strings.TrimSpace(answer), Err: err}
	}
}

func openPRWorktreeInVSCodeCmd(worktrees core.WorktreeService, opener core.OpenerService, cfg Config, pullRequest RemotePullRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		wt, _, err := worktrees.EnsurePullRequestWorktree(ctx, cfg, pullRequest)
		if err != nil {
			return openPRWorktreeResult{Err: err}
		}
		if err := opener.OpenVSCodeWorktree(ctx, wt); err != nil {
			return openPRWorktreeResult{Err: err}
		}
		return openPRWorktreeResult{Message: fmt.Sprintf("Opened PR #%d worktree in VS Code", pullRequest.Number)}
	}
}

func openPRWorktreeAgentCmd(worktrees core.WorktreeService, opener core.OpenerService, cfg Config, pullRequest RemotePullRequest, agent AgentTool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		wt, _, err := worktrees.EnsurePullRequestWorktree(ctx, cfg, pullRequest)
		if err != nil {
			return openPRWorktreeResult{Err: err}
		}
		if err := opener.OpenAgent(ctx, wt, agent); err != nil {
			return openPRWorktreeResult{Err: err}
		}
		return openPRWorktreeResult{Message: fmt.Sprintf("Opened %s for PR #%d worktree", agent.Name, pullRequest.Number)}
	}
}

func saveAppConfigCmd(services core.ConfigService, cfg Config) tea.Cmd {
	return func() tea.Msg {
		if err := services.WriteAppConfig(cfg.ConfigPath, cfg.App); err != nil {
			return actionResult{Err: fmt.Errorf("save config failed: %w", err)}
		}
		return actionResult{Message: fmt.Sprintf("Saved config %s", cfg.ConfigPath)}
	}
}

func saveAgentConfigCmd(services core.ConfigService, cfg Config) tea.Cmd {
	return func() tea.Msg {
		if err := services.WriteAppConfig(cfg.ConfigPath, cfg.App); err != nil {
			return actionResult{Err: fmt.Errorf("save config failed: %w", err)}
		}
		return actionResult{Message: fmt.Sprintf("Default AI agent set to %s", cfg.App.DefaultAgent)}
	}
}

func removeWorktreeCmd(services core.WorktreeService, cfg Config, wt Worktree) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		return deleteResult{Path: wt.Path, Name: wt.Name, Err: services.RemoveWorktree(ctx, cfg, wt)}
	}
}

func createWorktreeCmd(services core.WorktreeService, cfg Config, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		path := services.CreateWorktreePath(cfg, name)
		existing, err := services.CreateWorktree(ctx, cfg, name, path)
		return createResult{Path: path, Name: name, Existing: existing, Err: err}
	}
}

func openBrowserCmd(services core.OpenerService, url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return actionResult{Message: "Opened PR in browser", Err: services.OpenBrowser(ctx, url)}
	}
}

func openVSCodeWorktreeCmd(services core.OpenerService, wt Worktree) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := services.OpenVSCodeWorktree(ctx, wt); err == nil {
			return actionResult{Message: fmt.Sprintf("Opened %s in VS Code", wt.Name)}
		} else {
			return actionResult{Err: err}
		}
	}
}

func openAgentCmd(services core.OpenerService, wt Worktree, agent AgentTool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := services.OpenAgent(ctx, wt, agent); err == nil {
			return actionResult{Message: fmt.Sprintf("Opened %s for %s", agent.Name, wt.Name)}
		} else {
			return actionResult{Err: err}
		}
	}
}
