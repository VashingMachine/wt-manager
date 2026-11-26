package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

// Fields requested from acli. `workitem search` validates this list against a
// whitelist and rejects anything outside it (notably `updated`, `created` and
// `parent`), which is why list ordering has to come from the JQL ORDER BY.
const (
	jiraListFields   = "key,issuetype,assignee,priority,status,summary,labels"
	jiraDetailFields = "key,issuetype,assignee,reporter,priority,status,summary,labels,description,comment"
)

var (
	// Board URLs come in two shapes plus a bare id:
	//   /jira/software/c/projects/APP/boards/456
	//   /secure/RapidBoard.jspa?rapidView=456
	jiraBoardPathPattern = regexp.MustCompile(`/boards?/(\d+)`)
	jiraRapidViewPattern = regexp.MustCompile(`[?&]rapidView=(\d+)`)
	// Any other link inside a space names it through a work item or the space
	// itself:
	//   /browse/OPS-42, /browse/OPS
	//   /issues/?jql=…&selectedIssue=OPS-42
	//   /jira/software/c/projects/OPS/list, /jira/servicedesk/projects/OPS/queues
	jiraSelectedIssuePattern = regexp.MustCompile(`[?&]selectedIssue=([A-Za-z][A-Za-z0-9_]*)-\d+`)
	jiraBrowsePattern        = regexp.MustCompile(`/browse/([A-Za-z][A-Za-z0-9_]*)(?:-\d+)?(?:[/?#]|$)`)
	jiraSpacePathPattern     = regexp.MustCompile(`/(?:projects|spaces)/([A-Za-z][A-Za-z0-9_]*)(?:[/?#]|$)`)
	jiraBareKeyPattern       = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)(?:-\d+)?$`)
	jiraSitePattern          = regexp.MustCompile(`(?im)^\s*Site:\s*(\S+)\s*$`)
)

func jiraCLIAvailable() error {
	if _, err := exec.LookPath("acli"); err != nil {
		return errors.New("acli not found; install the Atlassian CLI to enable JIRA support")
	}
	return nil
}

// runACLI shells out to acli. acli reports failures as `✗ Error: …` on stdout
// with a non-zero exit, and shell.Run already folds that into the error.
func runACLI(ctx context.Context, args ...string) (string, error) {
	if err := jiraCLIAvailable(); err != nil {
		return "", err
	}
	return shell.Run(ctx, "", "acli", args...)
}

func checkAtlassianCLI(ctx context.Context) []string {
	if _, err := exec.LookPath("acli"); err != nil {
		return []string{"acli not found; JIRA tickets will be unavailable until the Atlassian CLI is installed"}
	}
	if _, err := shell.Run(ctx, "", "acli", "--version"); err != nil {
		return []string{"acli is installed but not runnable; JIRA tickets may be unavailable"}
	}
	if _, err := shell.Run(ctx, "", "acli", "jira", "auth", "status"); err != nil {
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

// jiraLinkRef is what a pasted JIRA link points at: a board by id, or a
// space by key.
type jiraLinkRef struct {
	BoardID int
	Space   string
}

// parseJiraLink reads a pasted board URL or id, or any link or key inside a
// space. Board links win, since they also carry the space in their path.
func parseJiraLink(input string) (jiraLinkRef, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return jiraLinkRef{}, errors.New("JIRA link is required")
	}
	if id, err := strconv.Atoi(trimmed); err == nil && id > 0 {
		return jiraLinkRef{BoardID: id}, nil
	}
	for _, pattern := range []*regexp.Regexp{jiraRapidViewPattern, jiraBoardPathPattern} {
		if match := pattern.FindStringSubmatch(trimmed); match != nil {
			id, err := strconv.Atoi(match[1])
			if err != nil {
				return jiraLinkRef{}, err
			}
			return jiraLinkRef{BoardID: id}, nil
		}
	}
	for _, pattern := range []*regexp.Regexp{jiraSelectedIssuePattern, jiraBrowsePattern, jiraSpacePathPattern, jiraBareKeyPattern} {
		if match := pattern.FindStringSubmatch(trimmed); match != nil {
			return jiraLinkRef{Space: strings.ToUpper(match[1])}, nil
		}
	}
	return jiraLinkRef{}, fmt.Errorf("could not find a board or space in %q", trimmed)
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

type restJiraProject struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// resolveJiraBoard turns a pasted link into a fully resolved board or space, so
// the link only has to be parsed and looked up once (at setup) rather than per
// load.
func resolveJiraBoard(ctx context.Context, link string) (core.JiraBoard, error) {
	ref, err := parseJiraLink(link)
	if err != nil {
		return core.JiraBoard{}, err
	}
	if ref.Space != "" {
		return resolveJiraSpace(ctx, link, ref.Space)
	}

	id := ref.BoardID
	output, err := runACLI(ctx, "jira", "board", "view", "--id", strconv.Itoa(id), "--json")
	if err != nil {
		return core.JiraBoard{}, fmt.Errorf("board %d lookup failed: %w", id, err)
	}
	var board restJiraBoard
	if err := json.Unmarshal([]byte(output), &board); err != nil {
		return core.JiraBoard{}, fmt.Errorf("acli returned invalid board JSON: %w", err)
	}

	projects, err := jiraBoardProjects(ctx, id)
	if err != nil {
		return core.JiraBoard{}, err
	}
	if len(projects) == 0 {
		return core.JiraBoard{}, fmt.Errorf("board %d (%s) has no associated projects", id, board.Name)
	}

	resolved := core.JiraBoard{URL: strings.TrimSpace(link), ID: id, Name: board.Name, Projects: projects}
	if resolved.Name == "" {
		resolved.Name = fmt.Sprintf("board %d", id)
	}
	return resolved, nil
}

// resolveJiraSpace looks the space up so a mistyped key fails at setup, and
// names it like JIRA's own board locations: "Operations (OPS)".
func resolveJiraSpace(ctx context.Context, link string, key string) (core.JiraBoard, error) {
	output, err := runACLI(ctx, "jira", "project", "view", "--key", key, "--json")
	if err != nil {
		return core.JiraBoard{}, fmt.Errorf("space %s lookup failed: %w", key, err)
	}
	var project restJiraProject
	if err := json.Unmarshal([]byte(output), &project); err != nil {
		return core.JiraBoard{}, fmt.Errorf("acli returned invalid project JSON: %w", err)
	}
	// JIRA answers with the space's current key, which differs when the link
	// predates a key change.
	if current := strings.ToUpper(strings.TrimSpace(project.Key)); current != "" {
		key = current
	}
	name := key
	if trimmed := strings.TrimSpace(project.Name); trimmed != "" {
		name = fmt.Sprintf("%s (%s)", trimmed, key)
	}
	return core.JiraBoard{URL: strings.TrimSpace(link), Name: name, Projects: []string{key}}, nil
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

func searchJiraBoards(ctx context.Context, name string) ([]core.JiraBoard, error) {
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
	boards := make([]core.JiraBoard, 0, len(payload.Values))
	for _, board := range payload.Values {
		boards = append(boards, core.JiraBoard{ID: board.ID, Name: board.Name})
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

func jiraUserFromREST(user *restJiraUser) core.JiraUser {
	if user == nil {
		return core.JiraUser{}
	}
	return core.JiraUser{AccountID: user.AccountID, DisplayName: user.DisplayName, Email: user.Email}
}

func ticketFromREST(issue restJiraIssue, site string) core.Ticket {
	fields := issue.Fields
	ticket := core.Ticket{
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
			ticket.Comments = append(ticket.Comments, core.TicketComment{
				Author:  jiraUserFromREST(comment.Author).Name(),
				Body:    renderADF(comment.Body),
				Created: comment.Created,
			})
		}
	}
	return ticket
}

func jiraLimit(cfg core.Config) int {
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Limit > 0 {
		return cfg.ActiveProfile.Jira.Limit
	}
	return core.JiraDefaultLimit
}

// jiraSiteForConfig prefers the site cached in config and only asks acli when it
// is missing, since resolving it costs a round trip.
func jiraSiteForConfig(ctx context.Context, cfg core.Config) string {
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Site != "" {
		return cfg.ActiveProfile.Jira.Site
	}
	site, err := jiraSite(ctx)
	if err != nil {
		return ""
	}
	return site
}

func jiraListJQL(board core.JiraBoard, mineOnly bool) string {
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

func fetchTicketsByProject(ctx context.Context, cfg core.Config, board core.JiraBoard, mineOnly bool, site string) ([]core.Ticket, error) {
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
	tickets := make([]core.Ticket, 0, len(issues))
	for _, issue := range issues {
		tickets = append(tickets, ticketFromREST(issue, site))
	}
	return tickets, nil
}

// fetchTicketsBySprint scopes to the board's active sprints. This instance
// rejects `sprint` as a JQL field, so the only route is sprint list-workitems,
// once per active sprint (a board can have several running at the same time).
func fetchTicketsBySprint(ctx context.Context, cfg core.Config, board core.JiraBoard, mineOnly bool, site string) ([]core.Ticket, error) {
	sprints, err := jiraActiveSprints(ctx, board.ID)
	if err != nil {
		return nil, err
	}
	if len(sprints) == 0 {
		return nil, fmt.Errorf("board %s has no active sprint", board.Name)
	}

	limit := strconv.Itoa(jiraLimit(cfg))
	seen := map[string]struct{}{}
	tickets := []core.Ticket{}
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

func fetchTickets(ctx context.Context, cfg core.Config, board core.JiraBoard, mineOnly bool, sprintOnly bool) ([]core.Ticket, error) {
	site := jiraSiteForConfig(ctx, cfg)
	if board.SprintScoped(sprintOnly) {
		return fetchTicketsBySprint(ctx, cfg, board, mineOnly, site)
	}
	return fetchTicketsByProject(ctx, cfg, board, mineOnly, site)
}

func loadTicketDetail(ctx context.Context, cfg core.Config, key string) (core.Ticket, error) {
	if strings.TrimSpace(key) == "" {
		return core.Ticket{}, errors.New("ticket key is required")
	}
	output, err := runACLI(ctx, "jira", "workitem", "view", key, "--fields", jiraDetailFields, "--json")
	if err != nil {
		return core.Ticket{}, err
	}
	var issue restJiraIssue
	if err := json.Unmarshal([]byte(output), &issue); err != nil {
		return core.Ticket{}, fmt.Errorf("acli returned invalid work item JSON: %w", err)
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
