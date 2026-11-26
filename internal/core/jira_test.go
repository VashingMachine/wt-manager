package core

import "testing"

// Assures that a space is a configured source keyed by its space and never
// narrowed to a sprint, while a board keeps its id key and sprint scope.
func TestJiraBoardSpaceHasNoSprintScope(t *testing.T) {
	space := JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	board := JiraBoard{ID: 123, Name: "Ops Board", Projects: []string{"OPS"}}

	if !space.Configured() || !space.IsSpace() || space.SourceID() != "space:OPS" || space.SprintScoped(true) {
		t.Fatalf("space = configured:%v space:%v id:%q sprint:%v", space.Configured(), space.IsSpace(), space.SourceID(), space.SprintScoped(true))
	}
	if !board.Configured() || board.IsSpace() || board.SourceID() != "123" || !board.SprintScoped(true) || board.SprintScoped(false) {
		t.Fatalf("board = configured:%v space:%v id:%q sprint:%v", board.Configured(), board.IsSpace(), board.SourceID(), board.SprintScoped(true))
	}
	if (JiraBoard{Name: "empty"}).Configured() {
		t.Fatal("a source without a board or space counts as configured")
	}
}

// Assures that tickets default to every configured board and space together,
// that a picked board still wins, and that a lone board needs no combining.
func TestJiraConfigActiveBoardDefaultsToAllSources(t *testing.T) {
	board := JiraBoard{ID: 123, Name: "App Board", Projects: []string{"APP"}}
	space := JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	jira := &JiraConfig{Boards: []JiraBoard{board, space}}

	for _, defaultBoard := range []string{"", JiraAllSources, "removed board"} {
		jira.DefaultBoard = defaultBoard
		all := jira.ActiveBoard()
		if all == nil || !all.IsCombined() || all.Name != JiraAllSources || len(all.Members()) != 2 {
			t.Fatalf("ActiveBoard() with default %q = %+v, want every board and space", defaultBoard, all)
		}
	}
	jira.DefaultBoard = "Operations (OPS)"
	if got := jira.ActiveBoard(); got == nil || got.SourceID() != space.SourceID() {
		t.Fatalf("ActiveBoard() = %+v, want the picked space", got)
	}
	single := &JiraConfig{Boards: []JiraBoard{board}, DefaultBoard: JiraAllSources}
	if got := single.ActiveBoard(); got == nil || got.IsCombined() || got.ID != 123 {
		t.Fatalf("ActiveBoard() with one board = %+v, want that board", got)
	}
}

// Assures that all sources together are configured, keyed apart from each
// member, and narrowed by the sprint filter only when they include a board.
func TestJiraAllSourcesSprintScopeFollowsBoards(t *testing.T) {
	board := JiraBoard{ID: 123, Name: "App Board", Projects: []string{"APP"}}
	space := JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	all := (&JiraConfig{Boards: []JiraBoard{board, space}}).AllSources()
	spaces := (&JiraConfig{Boards: []JiraBoard{space, {Name: "Website (WEB)", Projects: []string{"WEB"}}}}).AllSources()

	if !all.Configured() || all.IsSpace() || all.SourceID() == board.SourceID() || all.SourceID() == space.SourceID() {
		t.Fatalf("all sources = configured:%v space:%v id:%q", all.Configured(), all.IsSpace(), all.SourceID())
	}
	if !all.SprintScoped(true) || all.SprintScoped(false) || spaces.SprintScoped(true) {
		t.Fatalf("sprint scope = with board:%v off:%v spaces only:%v", all.SprintScoped(true), all.SprintScoped(false), spaces.SprintScoped(true))
	}
}
