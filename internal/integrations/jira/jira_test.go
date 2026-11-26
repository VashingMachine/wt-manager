package jira

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/testkit"
)

// Assures that board links and ids resolve to a board, while any other link or
// key inside a space (a ticket, a project page, a bare key) resolves to that
// whole space.
func TestParseJiraLink(t *testing.T) {
	for _, test := range []struct {
		input string
		want  jiraLinkRef
	}{
		{input: "456", want: jiraLinkRef{BoardID: 456}},
		{input: "https://site.atlassian.net/jira/software/c/projects/APP/boards/456", want: jiraLinkRef{BoardID: 456}},
		{input: "https://site.atlassian.net/secure/RapidBoard.jspa?rapidView=456", want: jiraLinkRef{BoardID: 456}},
		{input: "https://site.atlassian.net/jira/software/c/projects/OPS/boards/123?selectedIssue=OPS-42", want: jiraLinkRef{BoardID: 123}},
		{input: "https://example.atlassian.net/browse/OPS-42", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/browse/OPS-42?focusedCommentId=1", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/browse/OPS", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/issues/?jql=assignee%3DcurrentUser()&selectedIssue=OPS-42", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/jira/software/c/projects/OPS/list", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/jira/software/projects/AB2/issues/AB2-7", want: jiraLinkRef{Space: "AB2"}},
		{input: "https://site.atlassian.net/jira/servicedesk/projects/HELP/queues/custom/1", want: jiraLinkRef{Space: "HELP"}},
		{input: "ops-42", want: jiraLinkRef{Space: "OPS"}},
		{input: " OPS ", want: jiraLinkRef{Space: "OPS"}},
	} {
		got, err := parseJiraLink(test.input)
		if err != nil {
			t.Fatalf("parseJiraLink(%q) error = %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parseJiraLink(%q) = %+v, want %+v", test.input, got, test.want)
		}
	}
	for _, input := range []string{"", "https://site.atlassian.net/jira/your-work", "not a link"} {
		if got, err := parseJiraLink(input); err == nil {
			t.Fatalf("parseJiraLink(%q) = %+v, want an error", input, got)
		}
	}
}

func TestRenderADF(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Summary"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}]}]}`)
	got := renderADF(raw)
	for _, want := range []string{"## Summary", "- first"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderADF() = %q, missing %q", got, want)
		}
	}
}

func TestDecodeJiraIssuesAcceptsJSONStream(t *testing.T) {
	output := `[{"key":"APP-1","fields":{"summary":"first"}}]{"issues":[{"key":"APP-2","fields":{"summary":"second"}}]}`
	issues, err := decodeJiraIssues(output)
	if err != nil {
		t.Fatalf("decodeJiraIssues() error = %v", err)
	}
	if len(issues) != 2 || issues[0].Key != "APP-1" || issues[1].Key != "APP-2" {
		t.Fatalf("decodeJiraIssues() = %#v", issues)
	}
}

// Assures that pasting a ticket link resolves to the whole space the ticket
// belongs to, named after the space, rather than requiring a board link.
func TestResolveJiraBoardFromTicketLinkResolvesWholeSpace(t *testing.T) {
	testkit.InstallFakeACLI(t)
	link := "https://example.atlassian.net/browse/OPS-42"

	got, err := resolveJiraBoard(context.Background(), link)
	if err != nil {
		t.Fatalf("resolveJiraBoard() error = %v", err)
	}
	if !got.IsSpace() || got.ID != 0 || got.Name != "Operations (OPS)" || got.URL != link || len(got.Projects) != 1 || got.Projects[0] != "OPS" {
		t.Fatalf("resolveJiraBoard() = %+v, want the OPS space", got)
	}
}
