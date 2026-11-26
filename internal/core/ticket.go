package core

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// JiraUser identifies a JIRA account. Email is often absent depending on the
// site's privacy settings, so DisplayName is the only reliably present field.
type JiraUser struct {
	AccountID   string
	DisplayName string
	Email       string
}

// Name is the best available human label for the user.
func (u JiraUser) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Email != "" {
		return u.Email
	}
	return u.AccountID
}

type TicketComment struct {
	Author  string
	Body    string
	Created string
}

// Ticket is a JIRA work item. Description/Comments are only populated once the
// detail fetch has run (Detail == true); Worktree/PR are local joins computed by
// AttachTicketContext rather than anything JIRA returns.
type Ticket struct {
	Key            string
	Summary        string
	Type           string
	Priority       string
	Status         string
	StatusCategory string
	Assignee       JiraUser
	Reporter       JiraUser
	Labels         []string
	URL            string
	Description    string
	Comments       []TicketComment
	Detail         bool
	Worktree       *Worktree
	PR             *PullRequest
	LoadError      string
}

// TicketPatch is a local edit that is shown before JIRA confirms it, and is
// written into the cache once it does. Empty fields are left untouched.
type TicketPatch struct {
	Key            string
	Status         string
	StatusCategory string
	Assignee       *JiraUser
	Comment        *TicketComment
}

// Apply writes the patch into ticket when it is the patched ticket.
func (p TicketPatch) Apply(ticket *Ticket) {
	if ticket == nil || ticket.Key != p.Key {
		return
	}
	if p.Status != "" {
		ticket.Status = p.Status
		if p.StatusCategory != "" {
			ticket.StatusCategory = p.StatusCategory
		}
	}
	if p.Assignee != nil {
		ticket.Assignee = *p.Assignee
	}
	if p.Comment != nil {
		ticket.Comments = append(append([]TicketComment(nil), ticket.Comments...), *p.Comment)
	}
}

// JIRA status category keys, used for colouring instead of status names so that
// arbitrary project workflows render correctly.
const (
	StatusCategoryNew        = "new"
	StatusCategoryInProgress = "indeterminate"
	StatusCategoryDone       = "done"
	TicketAssigneeSelf       = "@me"
)

var (
	// Issue keys as they appear in branch names: APP-123-foo, feature/APP-124,
	// fix/app-125-bar, APP-126/changes8.
	jiraIssueKeyPattern = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])([A-Za-z][A-Za-z0-9]*-\d+)([^0-9]|$)`)
	jiraSlugPattern     = regexp.MustCompile(`[^a-z0-9]+`)
)

// TicketKeyFromBranch pulls the JIRA key out of a branch name. Real branches in
// the wild look like APP-123-foo, feature/APP-124, fix/app-125-bar and
// APP-126/changes8, so the key can sit anywhere and in any case.
func TicketKeyFromBranch(branch string) string {
	match := jiraIssueKeyPattern.FindStringSubmatch(branch)
	if match == nil {
		return ""
	}
	return strings.ToUpper(match[2])
}

// TicketBranchName builds the default branch/worktree name for a ticket,
// matching the common convention: APP-123-short-summary.
func TicketBranchName(ticket Ticket) string {
	key := strings.ToUpper(strings.TrimSpace(ticket.Key))
	slug := jiraSlugPattern.ReplaceAllString(strings.ToLower(ticket.Summary), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
		// avoid ending on a partial word when there is a boundary to fall back to
		if idx := strings.LastIndex(slug, "-"); idx > 15 {
			slug = slug[:idx]
		}
	}
	if key == "" {
		return slug
	}
	if slug == "" {
		return key
	}
	return key + "-" + slug
}

// AttachTicketContext joins tickets to local worktrees and to open PRs by issue
// key. Nothing here talks to the network; it is pure so it can be table-tested.
func AttachTicketContext(tickets []Ticket, worktrees []Worktree, pullRequests []PullRequest) []Ticket {
	worktreeByKey := map[string]*Worktree{}
	for i := range worktrees {
		wt := &worktrees[i]
		key := TicketKeyFromBranch(wt.Branch)
		if key == "" {
			key = TicketKeyFromBranch(wt.Name)
		}
		if key == "" {
			continue
		}
		// Prefer a managed, present worktree when several branches share a key.
		if existing, ok := worktreeByKey[key]; ok && !preferWorktree(wt, existing) {
			continue
		}
		worktreeByKey[key] = wt
	}

	prByKey := map[string]*PullRequest{}
	for i := range pullRequests {
		pr := &pullRequests[i]
		key := TicketKeyFromBranch(pr.HeadRefName)
		if key == "" {
			key = TicketKeyFromBranch(pr.Title)
		}
		if key == "" {
			continue
		}
		if existing, ok := prByKey[key]; ok && !preferPullRequest(pr, existing) {
			continue
		}
		prByKey[key] = pr
	}

	joined := make([]Ticket, len(tickets))
	copy(joined, tickets)
	for i := range joined {
		key := strings.ToUpper(joined[i].Key)
		joined[i].Worktree = worktreeByKey[key]
		joined[i].PR = prByKey[key]
	}
	return joined
}

func preferWorktree(candidate *Worktree, existing *Worktree) bool {
	score := func(wt *Worktree) int {
		points := 0
		if wt.Managed {
			points += 2
		}
		if !wt.Missing && !wt.Prunable {
			points += 1
		}
		return points
	}
	return score(candidate) > score(existing)
}

func preferPullRequest(candidate *PullRequest, existing *PullRequest) bool {
	open := func(pr *PullRequest) bool { return strings.EqualFold(pr.State, "open") }
	if open(candidate) != open(existing) {
		return open(candidate)
	}
	return candidate.Number > existing.Number
}

// FilterTickets narrows a ticket list by text, by status and to tickets that
// are not done yet.
func FilterTickets(tickets []Ticket, query string, status string, openOnly bool) []Ticket {
	query = strings.ToLower(strings.TrimSpace(query))
	status = strings.TrimSpace(status)
	filtered := make([]Ticket, 0, len(tickets))
	for _, ticket := range tickets {
		if openOnly && ticketIsDone(ticket) {
			continue
		}
		if status != "" && !strings.EqualFold(ticket.Status, status) {
			continue
		}
		if query != "" && !ticketMatchesQuery(ticket, query) {
			continue
		}
		filtered = append(filtered, ticket)
	}
	return filtered
}

func ticketIsDone(ticket Ticket) bool {
	if strings.EqualFold(strings.TrimSpace(ticket.StatusCategory), StatusCategoryDone) {
		return true
	}
	if strings.TrimSpace(ticket.StatusCategory) != "" {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(ticket.Status))
	return strings.Contains(status, "done") ||
		strings.Contains(status, "closed") ||
		strings.Contains(status, "resolved")
}

func ticketMatchesQuery(ticket Ticket, query string) bool {
	haystacks := []string{ticket.Key, ticket.Summary, ticket.Status, ticket.Type, ticket.Assignee.Name()}
	if ticket.Worktree != nil {
		haystacks = append(haystacks, ticket.Worktree.Name, ticket.Worktree.Branch)
	}
	if ticket.PR != nil {
		haystacks = append(haystacks, strconv.Itoa(ticket.PR.Number), ticket.PR.Title)
	}
	haystacks = append(haystacks, ticket.Labels...)
	for _, haystack := range haystacks {
		if strings.Contains(strings.ToLower(haystack), query) {
			return true
		}
	}
	return false
}

// TicketStatuses harvests the distinct statuses present in the loaded list. acli
// has no way to enumerate a work item's available transitions, so the statuses
// actually in use on the board are the best picker options available.
func TicketStatuses(tickets []Ticket) []string {
	seen := map[string]struct{}{}
	statuses := []string{}
	for _, ticket := range tickets {
		status := strings.TrimSpace(ticket.Status)
		if status == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(status)]; ok {
			continue
		}
		seen[strings.ToLower(status)] = struct{}{}
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		return jiraStatusRank(statuses[i]) < jiraStatusRank(statuses[j])
	})
	return statuses
}

// jiraStatusRank orders harvested statuses roughly by workflow position so the
// picker reads like a board left to right.
func jiraStatusRank(status string) int {
	switch normalized := strings.ToLower(status); {
	case strings.Contains(normalized, "backlog"):
		return 0
	case strings.Contains(normalized, "open"), strings.Contains(normalized, "to do"), strings.Contains(normalized, "todo"), strings.Contains(normalized, "new"):
		return 1
	case strings.Contains(normalized, "progress"), strings.Contains(normalized, "development"):
		return 2
	case strings.Contains(normalized, "review"):
		return 3
	case strings.Contains(normalized, "test"), strings.Contains(normalized, "qa"):
		return 4
	case strings.Contains(normalized, "block"), strings.Contains(normalized, "hold"):
		return 5
	case strings.Contains(normalized, "done"), strings.Contains(normalized, "closed"), strings.Contains(normalized, "resolved"):
		return 7
	default:
		return 6
	}
}

// TicketAssignees lists the distinct assignees of the tickets.
func TicketAssignees(tickets []Ticket) []JiraUser {
	seen := map[string]struct{}{}
	users := []JiraUser{}
	for _, ticket := range tickets {
		user := ticket.Assignee
		if user.Name() == "" {
			continue
		}
		if _, ok := seen[user.Name()]; ok {
			continue
		}
		seen[user.Name()] = struct{}{}
		users = append(users, user)
	}
	sort.SliceStable(users, func(i, j int) bool { return users[i].Name() < users[j].Name() })
	return users
}

// BuildTicketChatPrompt mirrors BuildPullRequestChatPrompt: flat labelled lines
// with rune budgets so a long ticket cannot blow out the agent's context.
func BuildTicketChatPrompt(ticket Ticket, question string) string {
	var b strings.Builder
	b.WriteString("You are helping with a JIRA work item.\n\n")
	fmt.Fprintf(&b, "Key: %s\n", ticket.Key)
	fmt.Fprintf(&b, "Summary: %s\n", ticket.Summary)
	fmt.Fprintf(&b, "Type: %s\n", emptyDash(ticket.Type))
	fmt.Fprintf(&b, "Status: %s\n", emptyDash(ticket.Status))
	fmt.Fprintf(&b, "Priority: %s\n", emptyDash(ticket.Priority))
	fmt.Fprintf(&b, "Assignee: %s\n", emptyDash(ticket.Assignee.Name()))
	if len(ticket.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(ticket.Labels, ", "))
	}
	if ticket.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", ticket.URL)
	}
	if ticket.Worktree != nil {
		fmt.Fprintf(&b, "Local worktree: %s (%s)\n", ticket.Worktree.Name, ticket.Worktree.Path)
	}
	if ticket.PR != nil {
		fmt.Fprintf(&b, "Pull request: #%d %s\n", ticket.PR.Number, ticket.PR.Title)
	}
	if description := strings.TrimSpace(ticket.Description); description != "" {
		fmt.Fprintf(&b, "\nDescription:\n%s\n", truncateRunes(description, 3000))
	}
	if len(ticket.Comments) > 0 {
		b.WriteString("\nComments:\n")
		for i, comment := range ticket.Comments {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "- %s: %s\n", comment.Author, truncateRunes(strings.TrimSpace(comment.Body), 600))
		}
	}
	fmt.Fprintf(&b, "\nQuestion: %s\n", question)
	return b.String()
}
