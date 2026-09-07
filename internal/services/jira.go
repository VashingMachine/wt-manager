package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
)

type JiraConfig = core.JiraConfig
type JiraBoard = core.JiraBoard
type JiraUser = core.JiraUser
type Ticket = core.Ticket
type TicketComment = core.TicketComment

// Fields requested from acli. `workitem search` validates this list against a
// whitelist and rejects anything outside it (notably `updated`, `created` and
// `parent`), which is why list ordering has to come from the JQL ORDER BY.
const (
	jiraListFields   = "key,issuetype,assignee,priority,status,summary,labels"
	jiraDetailFields = "key,issuetype,assignee,reporter,priority,status,summary,labels,description,comment"
	jiraDefaultLimit = 100
)

var (
	// Board URLs come in two shapes plus a bare id:
	//   /jira/software/c/projects/EDPSL/boards/24731
	//   /secure/RapidBoard.jspa?rapidView=24731
	jiraBoardPathPattern = regexp.MustCompile(`/boards?/(\d+)`)
	jiraRapidViewPattern = regexp.MustCompile(`[?&]rapidView=(\d+)`)
	jiraSitePattern      = regexp.MustCompile(`(?im)^\s*Site:\s*(\S+)\s*$`)
	// Issue keys as they appear in branch names: EDPSL-4618-foo, feature/EDPSL-4592,
	// fix/edpsl-2913-bar, EDPSL-4567/changes8.
	jiraIssueKeyPattern = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])([A-Za-z][A-Za-z0-9]*-\d+)([^0-9]|$)`)
	jiraSlugPattern     = regexp.MustCompile(`[^a-z0-9]+`)
)

func jiraCLIAvailable() error {
	if _, err := exec.LookPath("acli"); err != nil {
		return errors.New("acli not found; install the Atlassian CLI to enable JIRA support")
	}
	return nil
}

// runACLI shells out to acli. acli reports failures as `✗ Error: …` on stdout
// with a non-zero exit, and runCommand already folds that into the error.
func runACLI(ctx context.Context, args ...string) (string, error) {
	if err := jiraCLIAvailable(); err != nil {
		return "", err
	}
	return runCommand(ctx, "", "acli", args...)
}

func checkAtlassianCLI(ctx context.Context) []string {
	if _, err := exec.LookPath("acli"); err != nil {
		return []string{"acli not found; JIRA tickets will be unavailable until the Atlassian CLI is installed"}
	}
	if _, err := runCommand(ctx, "", "acli", "--version"); err != nil {
		return []string{"acli is installed but not runnable; JIRA tickets may be unavailable"}
	}
	if _, err := runCommand(ctx, "", "acli", "jira", "auth", "status"); err != nil {
		return []string{"acli is not authenticated for Jira; run acli jira auth login to enable JIRA tickets"}
	}
	return nil
}

// jiraSite returns the Atlassian site host (e.g. example.atlassian.net). acli only
// prints this as plain text, so the Site: line is parsed out.
func jiraSite(ctx context.Context) (string, error) {
	output, err := runACLI(ctx, "jira", "auth", "status")
	if err != nil {
		return "", err
	}
	match := jiraSitePattern.FindStringSubmatch(output)
	if match == nil {
		return "", errors.New("could not determine the Atlassian site from acli auth status")
	}
	return match[1], nil
}

func jiraTicketURL(site string, key string) string {
	if site == "" || key == "" {
		return ""
	}
	return "https://" + strings.TrimSuffix(site, "/") + "/browse/" + key
}

// parseJiraBoardRef extracts a numeric board id from a pasted board URL or a
// bare id.
func parseJiraBoardRef(input string) (int, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return 0, errors.New("board link is required")
	}
	if id, err := strconv.Atoi(trimmed); err == nil && id > 0 {
		return id, nil
	}
	if match := jiraRapidViewPattern.FindStringSubmatch(trimmed); match != nil {
		return strconv.Atoi(match[1])
	}
	if match := jiraBoardPathPattern.FindStringSubmatch(trimmed); match != nil {
		return strconv.Atoi(match[1])
	}
	return 0, fmt.Errorf("could not find a board id in %q", trimmed)
}

type restJiraBoard struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Type     string `json:"type"`
}

type restJiraBoardSearch struct {
	Values []restJiraBoard `json:"values"`
}

type restJiraBoardProjects struct {
	Projects []struct {
		Key string `json:"key"`
	} `json:"projects"`
}

type restJiraSprints struct {
	Sprints []struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		State string `json:"state"`
	} `json:"sprints"`
}

// resolveJiraBoard turns a pasted board link into a fully resolved board, so the
// link only has to be parsed and looked up once (at setup) rather than per load.
func resolveJiraBoard(ctx context.Context, boardURLOrID string) (JiraBoard, error) {
	id, err := parseJiraBoardRef(boardURLOrID)
	if err != nil {
		return JiraBoard{}, err
	}

	output, err := runACLI(ctx, "jira", "board", "view", "--id", strconv.Itoa(id), "--json")
	if err != nil {
		return JiraBoard{}, fmt.Errorf("board %d lookup failed: %w", id, err)
	}
	var board restJiraBoard
	if err := json.Unmarshal([]byte(output), &board); err != nil {
		return JiraBoard{}, fmt.Errorf("acli returned invalid board JSON: %w", err)
	}

	projects, err := jiraBoardProjects(ctx, id)
	if err != nil {
		return JiraBoard{}, err
	}
	if len(projects) == 0 {
		return JiraBoard{}, fmt.Errorf("board %d (%s) has no associated projects", id, board.Name)
	}

	resolved := JiraBoard{URL: strings.TrimSpace(boardURLOrID), ID: id, Name: board.Name, Projects: projects}
	if resolved.Name == "" {
		resolved.Name = fmt.Sprintf("board %d", id)
	}
	return resolved, nil
}

func jiraBoardProjects(ctx context.Context, boardID int) ([]string, error) {
	output, err := runACLI(ctx, "jira", "board", "list-projects", "--id", strconv.Itoa(boardID), "--json")
	if err != nil {
		return nil, fmt.Errorf("board %d project lookup failed: %w", boardID, err)
	}
	var payload restJiraBoardProjects
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("acli returned invalid board project JSON: %w", err)
	}
	projects := make([]string, 0, len(payload.Projects))
	for _, project := range payload.Projects {
		if key := strings.TrimSpace(project.Key); key != "" {
			projects = append(projects, key)
		}
	}
	return projects, nil
}

func searchJiraBoards(ctx context.Context, name string) ([]JiraBoard, error) {
	args := []string{"jira", "board", "search", "--json", "--limit", "50"}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		args = append(args, "--name", trimmed)
	}
	output, err := runACLI(ctx, args...)
	if err != nil {
		return nil, err
	}
	var payload restJiraBoardSearch
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("acli returned invalid board search JSON: %w", err)
	}
	boards := make([]JiraBoard, 0, len(payload.Values))
	for _, board := range payload.Values {
		boards = append(boards, JiraBoard{ID: board.ID, Name: board.Name})
	}
	return boards, nil
}

func jiraActiveSprints(ctx context.Context, boardID int) ([]int, error) {
	output, err := runACLI(ctx, "jira", "board", "list-sprints", "--id", strconv.Itoa(boardID), "--state", "active", "--json")
	if err != nil {
		return nil, fmt.Errorf("board %d sprint lookup failed: %w", boardID, err)
	}
	var payload restJiraSprints
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("acli returned invalid sprint JSON: %w", err)
	}
	ids := make([]int, 0, len(payload.Sprints))
	for _, sprint := range payload.Sprints {
		if sprint.ID > 0 {
			ids = append(ids, sprint.ID)
		}
	}
	return ids, nil
}

type restJiraIssue struct {
	Key    string         `json:"key"`
	Fields restJiraFields `json:"fields"`
}

type restJiraFields struct {
	Summary   string          `json:"summary"`
	Labels    []string        `json:"labels"`
	IssueType *restJiraNamed  `json:"issuetype"`
	Priority  *restJiraNamed  `json:"priority"`
	Status    *restJiraStatus `json:"status"`
	Assignee  *restJiraUser   `json:"assignee"`
	Reporter  *restJiraUser   `json:"reporter"`
	// Description is ADF, or occasionally a bare string on older projects.
	Description json.RawMessage   `json:"description"`
	Comment     *restJiraComments `json:"comment"`
}

type restJiraNamed struct {
	Name string `json:"name"`
}

type restJiraStatus struct {
	Name           string `json:"name"`
	StatusCategory *struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

type restJiraUser struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

type restJiraComments struct {
	Comments []struct {
		Author  *restJiraUser   `json:"author"`
		Body    json.RawMessage `json:"body"`
		Created string          `json:"created"`
	} `json:"comments"`
}

// restJiraSprintIssues is the `sprint list-workitems` envelope. `workitem search`
// returns a bare array instead, but the inner issue shape is identical.
type restJiraSprintIssues struct {
	Issues []restJiraIssue `json:"issues"`
}

func jiraUserFromREST(user *restJiraUser) JiraUser {
	if user == nil {
		return JiraUser{}
	}
	return JiraUser{AccountID: user.AccountID, DisplayName: user.DisplayName, Email: user.Email}
}

func ticketFromREST(issue restJiraIssue, site string) Ticket {
	fields := issue.Fields
	ticket := Ticket{
		Key:      issue.Key,
		Summary:  strings.TrimSpace(fields.Summary),
		Labels:   fields.Labels,
		Assignee: jiraUserFromREST(fields.Assignee),
		Reporter: jiraUserFromREST(fields.Reporter),
		URL:      jiraTicketURL(site, issue.Key),
	}
	if fields.IssueType != nil {
		ticket.Type = fields.IssueType.Name
	}
	if fields.Priority != nil {
		ticket.Priority = fields.Priority.Name
	}
	if fields.Status != nil {
		ticket.Status = fields.Status.Name
		if fields.Status.StatusCategory != nil {
			ticket.StatusCategory = fields.Status.StatusCategory.Key
		}
	}
	ticket.Description = renderADF(fields.Description)
	if fields.Comment != nil {
		for _, comment := range fields.Comment.Comments {
			ticket.Comments = append(ticket.Comments, TicketComment{
				Author:  jiraUserFromREST(comment.Author).Name(),
				Body:    renderADF(comment.Body),
				Created: comment.Created,
			})
		}
	}
	return ticket
}

func jiraLimit(cfg Config) int {
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Limit > 0 {
		return cfg.ActiveProfile.Jira.Limit
	}
	return jiraDefaultLimit
}

// jiraSiteForConfig prefers the site cached in config and only asks acli when it
// is missing, since resolving it costs a round trip.
func jiraSiteForConfig(ctx context.Context, cfg Config) string {
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Site != "" {
		return cfg.ActiveProfile.Jira.Site
	}
	site, err := jiraSite(ctx)
	if err != nil {
		return ""
	}
	return site
}

func jiraListJQL(board JiraBoard, mineOnly bool) string {
	clauses := []string{}
	if len(board.Projects) > 0 {
		quoted := make([]string, 0, len(board.Projects))
		for _, project := range board.Projects {
			quoted = append(quoted, `"`+project+`"`)
		}
		clauses = append(clauses, "project IN ("+strings.Join(quoted, ", ")+")")
	}
	if mineOnly {
		clauses = append(clauses, "assignee = currentUser()")
	}
	if len(clauses) == 0 {
		return "ORDER BY updated DESC"
	}
	return strings.Join(clauses, " AND ") + " ORDER BY updated DESC"
}

func fetchTicketsByProject(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, site string) ([]Ticket, error) {
	output, err := runACLI(ctx, "jira", "workitem", "search",
		"--jql", jiraListJQL(board, mineOnly),
		"--fields", jiraListFields,
		"--limit", strconv.Itoa(jiraLimit(cfg)),
		"--json")
	if err != nil {
		return nil, err
	}
	issues, err := decodeJiraIssues(output)
	if err != nil {
		return nil, err
	}
	tickets := make([]Ticket, 0, len(issues))
	for _, issue := range issues {
		tickets = append(tickets, ticketFromREST(issue, site))
	}
	return tickets, nil
}

// fetchTicketsBySprint scopes to the board's active sprints. This instance
// rejects `sprint` as a JQL field, so the only route is sprint list-workitems,
// once per active sprint (a board can have several running at the same time).
func fetchTicketsBySprint(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, site string) ([]Ticket, error) {
	sprints, err := jiraActiveSprints(ctx, board.ID)
	if err != nil {
		return nil, err
	}
	if len(sprints) == 0 {
		return nil, fmt.Errorf("board %s has no active sprint", board.Name)
	}

	limit := strconv.Itoa(jiraLimit(cfg))
	seen := map[string]struct{}{}
	tickets := []Ticket{}
	for _, sprint := range sprints {
		args := []string{"jira", "sprint", "list-workitems",
			"--board", strconv.Itoa(board.ID),
			"--sprint", strconv.Itoa(sprint),
			"--fields", jiraListFields,
			"--limit", limit,
			"--json"}
		if mineOnly {
			args = append(args, "--jql", "assignee = currentUser()")
		}
		output, err := runACLI(ctx, args...)
		if err != nil {
			return nil, err
		}
		issues, err := decodeJiraIssues(output)
		if err != nil {
			return nil, err
		}
		for _, issue := range issues {
			if _, ok := seen[issue.Key]; ok {
				continue
			}
			seen[issue.Key] = struct{}{}
			tickets = append(tickets, ticketFromREST(issue, site))
		}
	}
	return tickets, nil
}

// decodeJiraIssues accepts the bare array returned by workitem search and the
// {"issues":[…]} envelope returned by sprint listings. Some ACLI versions emit
// paginated envelopes as consecutive JSON documents, so decode the output as a
// stream instead of requiring exactly one top-level value.
func decodeJiraIssues(output string) ([]restJiraIssue, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	issues := []restJiraIssue{}
	for document := 1; ; document++ {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return issues, nil
			}
			return nil, fmt.Errorf("acli returned invalid work item JSON document %d: %w", document, err)
		}
		decoded, err := decodeJiraIssueDocument(raw)
		if err != nil {
			return nil, fmt.Errorf("acli returned invalid work item JSON document %d: %w", document, err)
		}
		issues = append(issues, decoded...)
	}
}

func decodeJiraIssueDocument(raw json.RawMessage) ([]restJiraIssue, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var issues []restJiraIssue
		if err := json.Unmarshal(raw, &issues); err != nil {
			return nil, err
		}
		return issues, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, errors.New("expected a JSON array or object")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	issueJSON, ok := envelope["issues"]
	if !ok {
		return nil, errors.New(`expected object field "issues"`)
	}
	var issues []restJiraIssue
	if err := json.Unmarshal(issueJSON, &issues); err != nil {
		return nil, err
	}
	return issues, nil
}

func fetchTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) ([]Ticket, error) {
	site := jiraSiteForConfig(ctx, cfg)
	if sprintOnly {
		return fetchTicketsBySprint(ctx, cfg, board, mineOnly, site)
	}
	return fetchTicketsByProject(ctx, cfg, board, mineOnly, site)
}

func loadTicketDetail(ctx context.Context, cfg Config, key string) (Ticket, error) {
	if strings.TrimSpace(key) == "" {
		return Ticket{}, errors.New("ticket key is required")
	}
	output, err := runACLI(ctx, "jira", "workitem", "view", key, "--fields", jiraDetailFields, "--json")
	if err != nil {
		return Ticket{}, err
	}
	var issue restJiraIssue
	if err := json.Unmarshal([]byte(output), &issue); err != nil {
		return Ticket{}, fmt.Errorf("acli returned invalid work item JSON: %w", err)
	}
	if issue.Key == "" {
		issue.Key = key
	}
	ticket := ticketFromREST(issue, jiraSiteForConfig(ctx, cfg))
	ticket.Detail = true
	return ticket, nil
}

func transitionTicket(ctx context.Context, key string, status string) error {
	key = strings.TrimSpace(key)
	status = strings.TrimSpace(status)
	if key == "" {
		return errors.New("ticket key is required")
	}
	if status == "" {
		return errors.New("status is required")
	}
	_, err := runACLI(ctx, "jira", "workitem", "transition", "--key", key, "--status", status, "--yes")
	return err
}

func commentTicket(ctx context.Context, key string, body string) error {
	key = strings.TrimSpace(key)
	body = strings.TrimSpace(body)
	if key == "" {
		return errors.New("ticket key is required")
	}
	if body == "" {
		return errors.New("comment body is required")
	}
	_, err := runACLI(ctx, "jira", "workitem", "comment", "create", "--key", key, "--body", body)
	return err
}

func assignTicket(ctx context.Context, key string, assignee string) error {
	key = strings.TrimSpace(key)
	assignee = strings.TrimSpace(assignee)
	if key == "" {
		return errors.New("ticket key is required")
	}
	if assignee == "" {
		assignee = core.TicketAssigneeSelf
	}
	_, err := runACLI(ctx, "jira", "workitem", "assign", "--key", key, "--assignee", assignee, "--yes")
	return err
}

// ticketKeyFromBranch pulls the JIRA key out of a branch name. Real branches in
// the wild look like EDPSL-4618-foo, feature/EDPSL-4592, fix/edpsl-2913-bar and
// EDPSL-4567/changes8, so the key can sit anywhere and in any case.
func ticketKeyFromBranch(branch string) string {
	match := jiraIssueKeyPattern.FindStringSubmatch(branch)
	if match == nil {
		return ""
	}
	return strings.ToUpper(match[2])
}

// ticketBranchName builds the default branch/worktree name for a ticket,
// matching the convention already used in these repos: EDPSL-4618-short-summary.
func ticketBranchName(ticket Ticket) string {
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

// attachTicketContext joins tickets to local worktrees and to open PRs by issue
// key. Nothing here talks to the network; it is pure so it can be table-tested.
func attachTicketContext(tickets []Ticket, worktrees []Worktree, pullRequests []PullRequest) []Ticket {
	worktreeByKey := map[string]*Worktree{}
	for i := range worktrees {
		wt := &worktrees[i]
		key := ticketKeyFromBranch(wt.Branch)
		if key == "" {
			key = ticketKeyFromBranch(wt.Name)
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
		key := ticketKeyFromBranch(pr.HeadRefName)
		if key == "" {
			key = ticketKeyFromBranch(pr.Title)
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

func filterTickets(tickets []Ticket, query string, status string, openOnly bool) []Ticket {
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
	if strings.EqualFold(strings.TrimSpace(ticket.StatusCategory), core.StatusCategoryDone) {
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

// ticketStatuses harvests the distinct statuses present in the loaded list. acli
// has no way to enumerate a work item's available transitions, so the statuses
// actually in use on the board are the best picker options available.
func ticketStatuses(tickets []Ticket) []string {
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

func ticketAssignees(tickets []Ticket) []JiraUser {
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

// buildTicketChatPrompt mirrors buildPullRequestChatPrompt: flat labelled lines
// with rune budgets so a long ticket cannot blow out the agent's context.
func buildTicketChatPrompt(ticket Ticket, question string) string {
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
