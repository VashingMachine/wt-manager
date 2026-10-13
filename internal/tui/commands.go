package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

const refreshTimeout = 45 * time.Second
const prDetailFetchDelay = 200 * time.Millisecond

type loadResult struct {
	Worktrees      []Worktree
	Warning        string
	Err            error
	RepositoryPath string
	ShowAll        bool
}

type remotePRListResult struct {
	RepoSlug     string
	PullRequests []RemotePullRequest
	CurrentUser  string
	FetchedAt    time.Time
	Err          error
	// Background results come from prefetching or quiet revalidation and must
	// not take over the status bar or selection.
	Background bool
}

type ticketListResult struct {
	Source     string
	Tickets    []Ticket
	Worktrees  []Worktree
	Warning    string
	FetchedAt  time.Time
	Err        error
	Background bool
}

type ticketDetailResult struct {
	Key       string
	RequestID int
	Ticket    Ticket
	Err       error
}

type ticketMutationResult struct {
	ID      int
	Key     string
	Action  string
	Message string
	Err     error
}

type ticketWorktreeResult struct {
	Key      string
	Worktree Worktree
	Existing bool
	Err      error
}

// loadWorktreesCmd only discovers worktrees (one fast git call); handling its
// result starts the per-worktree status reads and the PR label lookup.
func loadWorktreesCmd(services core.WorktreeService, cfg Config, showAll bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		worktrees, err := services.DiscoverWorktrees(ctx, cfg, showAll)
		return loadResult{Worktrees: worktrees, Err: err, RepositoryPath: cfg.ActiveProfile.RepositoryPath, ShowAll: showAll}
	}
}

func loadRemotePullRequestsCmd(services core.PullRequestService, cfg Config, background bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		var (
			wg           sync.WaitGroup
			pullRequests []RemotePullRequest
			err          error
			currentUser  string
		)
		// The user lookup is cached for a day, but on a cold cache it should not
		// add a round trip in front of the list.
		wg.Go(func() { pullRequests, err = services.LoadRemotePullRequests(ctx, cfg) })
		wg.Go(func() { currentUser, _ = services.LoadGitHubCurrentUser(ctx, cfg) })
		wg.Wait()
		return remotePRListResult{RepoSlug: cfg.RepoSlug, PullRequests: pullRequests, CurrentUser: currentUser, FetchedAt: time.Now(), Err: err, Background: background}
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

func loadTicketsCmd(services core.Services, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		var (
			tickets         []Ticket
			warning         string
			ticketErr       error
			worktrees       []Worktree
			worktreeWarning string
			worktreeErr     error
			pullRequests    []PullRequest
			pullRequestErr  error
			wg              sync.WaitGroup
		)
		wg.Add(3)
		go func() {
			defer wg.Done()
			tickets, warning, ticketErr = services.LoadTickets(ctx, cfg, board, mineOnly, sprintOnly, refresh)
		}()
		go func() {
			defer wg.Done()
			worktrees, worktreeWarning, worktreeErr = services.CollectTicketWorktrees(ctx, cfg)
		}()
		go func() {
			defer wg.Done()
			pullRequests, _, pullRequestErr = services.LoadPullRequestIndex(ctx, cfg, false)
		}()
		wg.Wait()

		source := fmt.Sprintf("%s|%d|%t|%t", cfg.ActiveProfile.RepositoryPath, board.ID, mineOnly, sprintOnly)
		if ticketErr != nil {
			return ticketListResult{Source: source, Err: ticketErr}
		}

		if worktreeErr != nil {
			if warning == "" {
				warning = fmt.Sprintf("worktree refresh failed: %v", worktreeErr)
			}
		} else if worktreeWarning != "" && warning == "" {
			warning = worktreeWarning
		}

		if pullRequestErr != nil {
			if warning == "" {
				warning = fmt.Sprintf("PR matching refresh failed: %v", pullRequestErr)
			}
		}

		return ticketListResult{
			Source:    source,
			Tickets:   services.AttachTicketContext(tickets, worktrees, pullRequests),
			Worktrees: worktrees,
			Warning:   warning,
			FetchedAt: time.Now(),
		}
	}
}

func loadTicketDetailCmd(services core.JiraService, ctx context.Context, cfg Config, key string, requestID int) tea.Cmd {
	return func() tea.Msg {
		ticket, err := services.LoadTicketDetail(ctx, cfg, key)
		return ticketDetailResult{Key: key, RequestID: requestID, Ticket: ticket, Err: err}
	}
}

func ensureTicketWorktreeCmd(services core.Services, cfg Config, ticket Ticket) tea.Cmd {
	return func() tea.Msg {
		if ticket.Worktree != nil && !ticket.Worktree.Missing && !ticket.Worktree.Prunable {
			return ticketWorktreeResult{Key: ticket.Key, Worktree: *ticket.Worktree, Existing: true}
		}
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		branch := services.TicketBranchName(ticket)
		path := services.CreateWorktreePath(cfg, branch)
		existing, err := services.CreateWorktree(ctx, cfg, branch, path)
		return ticketWorktreeResult{
			Key:      ticket.Key,
			Worktree: Worktree{Name: branch, Branch: branch, Path: path, Managed: true},
			Existing: existing,
			Err:      err,
		}
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

func approveRemotePullRequestCmd(services core.PullRequestService, cfg Config, number int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		return approvePRResult{Number: number, Err: services.ApproveRemotePullRequest(ctx, cfg, number)}
	}
}

func openPRWorktreeInEditorCmd(worktrees core.WorktreeService, opener core.OpenerService, cfg Config, pullRequest RemotePullRequest, editor core.EditorTool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		wt, _, err := worktrees.EnsurePullRequestWorktree(ctx, cfg, pullRequest)
		if err != nil {
			return openPRWorktreeResult{Err: err}
		}
		if err := opener.OpenEditorWorktree(ctx, wt, editor); err != nil {
			return openPRWorktreeResult{Err: err}
		}
		return openPRWorktreeResult{Message: fmt.Sprintf("Opened PR #%d worktree in %s", pullRequest.Number, editor.Name)}
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
	return openBrowserMessageCmd(services, url, "Opened PR in browser")
}

func openBrowserMessageCmd(services core.OpenerService, url string, message string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return actionResult{Message: message, Err: services.OpenBrowser(ctx, url)}
	}
}

func openEditorWorktreeCmd(services core.OpenerService, wt Worktree, editor core.EditorTool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := services.OpenEditorWorktree(ctx, wt, editor); err == nil {
			return actionResult{Message: fmt.Sprintf("Opened %s in %s", wt.Name, editor.Name)}
		} else {
			return actionResult{Err: err}
		}
	}
}

func openAgentCmd(services core.OpenerService, wt Worktree, agent AgentTool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := services.OpenAgent(ctx, wt, agent); err == nil {
			return actionResult{Message: fmt.Sprintf("Opened %s for %s", agent.Name, wt.Name)}
		} else {
			return actionResult{Err: err}
		}
	}
}

func openTerminalWorktreeCmd(services core.OpenerService, wt Worktree) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := services.OpenTerminal(ctx, wt); err == nil {
			return actionResult{Message: fmt.Sprintf("Opened terminal for %s", wt.Name)}
		} else {
			return actionResult{Err: err}
		}
	}
}

func notifyCmd(services core.OpenerService, title string, body string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// The notification is a courtesy; failing to deliver it is not worth surfacing.
		_ = services.Notify(ctx, title, body)
		return nil
	}
}
