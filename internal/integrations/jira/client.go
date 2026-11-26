// Package jira is the JIRA integration. It talks to JIRA through acli, the
// Atlassian command line, so it uses whatever site acli is logged in to.
//
// Client is the whole surface the rest of the app sees. jira.go holds the
// calls and the JSON shapes acli answers with; adf.go turns Atlassian Document
// Format (descriptions and comments) into plain text.
package jira

import (
	"context"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Client runs acli.
type Client struct{}

func New() *Client {
	return &Client{}
}

// CheckCLI returns the problems that would keep tickets from loading: acli
// missing, not runnable or not logged in.
func (c *Client) CheckCLI(ctx context.Context) []string {
	return checkAtlassianCLI(ctx)
}

// Site returns the Atlassian site acli is logged in to.
func (c *Client) Site(ctx context.Context) (string, error) {
	return jiraSite(ctx)
}

// ResolveBoard turns a board link or id into a board, and any other link or
// key inside a space into that whole space.
func (c *Client) ResolveBoard(ctx context.Context, link string) (core.JiraBoard, error) {
	return resolveJiraBoard(ctx, link)
}

// SearchBoards lists the boards whose name matches.
func (c *Client) SearchBoards(ctx context.Context, name string) ([]core.JiraBoard, error) {
	return searchJiraBoards(ctx, name)
}

// Tickets lists the tickets of one board or space, narrowed to the user's own
// and to the active sprints as asked.
func (c *Client) Tickets(ctx context.Context, cfg core.Config, board core.JiraBoard, mineOnly bool, sprintOnly bool) ([]core.Ticket, error) {
	return fetchTickets(ctx, cfg, board, mineOnly, sprintOnly)
}

// TicketDetail reads one ticket with its description and comments.
func (c *Client) TicketDetail(ctx context.Context, cfg core.Config, key string) (core.Ticket, error) {
	return loadTicketDetail(ctx, cfg, key)
}

// Transition moves a ticket to another status.
func (c *Client) Transition(ctx context.Context, key string, status string) error {
	return transitionTicket(ctx, key, status)
}

// Comment adds a comment to a ticket.
func (c *Client) Comment(ctx context.Context, key string, body string) error {
	return commentTicket(ctx, key, body)
}

// Assign assigns a ticket; an empty assignee means the logged-in user.
func (c *Client) Assign(ctx context.Context, key string, assignee string) error {
	return assignTicket(ctx, key, assignee)
}
