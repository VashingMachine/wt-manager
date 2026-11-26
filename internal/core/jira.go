package core

import (
	"fmt"
	"strings"
)

// JiraBoard is a ticket source resolved from a pasted JIRA link. A board link
// gives a board (ID set), which can also be scoped to its active sprints. Any
// other link inside a space (a work item, a project page, a bare key) gives the
// whole space: no ID and the space's key as its only project. ID/Name/Projects
// are filled once by ResolveJiraBoard and then persisted, so the link only has
// to be parsed and looked up during setup.
type JiraBoard struct {
	URL      string   `json:"url"`
	ID       int      `json:"id,omitempty"`
	Name     string   `json:"name"`
	Projects []string `json:"projects"`
	// Sources holds the boards and spaces of the all-sources selection built
	// by JiraConfig.AllSources. It is never persisted.
	Sources []JiraBoard `json:"-"`
}

// JiraDefaultLimit is how many tickets a board or space lists unless the
// config says otherwise.
const JiraDefaultLimit = 100

// JiraAllSources names the selection that lists every configured board and
// space together. DefaultBoard stores it like any board name.
const JiraAllSources = "All boards and spaces"

// Configured reports whether the source can list tickets at all.
func (b JiraBoard) Configured() bool {
	return b.ID != 0 || len(b.Projects) > 0 || len(b.Sources) > 0
}

// IsCombined reports whether the source is the all-sources selection.
func (b JiraBoard) IsCombined() bool {
	return len(b.Sources) > 0
}

// Members lists the boards and spaces the source loads: its sources when
// combined, otherwise itself.
func (b JiraBoard) Members() []JiraBoard {
	if b.IsCombined() {
		return b.Sources
	}
	return []JiraBoard{b}
}

// IsSpace reports whether the source is a whole JIRA space rather than a board.
func (b JiraBoard) IsSpace() bool {
	return b.ID == 0 && len(b.Projects) > 0
}

// SourceID identifies the source in cache and selection keys.
func (b JiraBoard) SourceID() string {
	if b.IsCombined() {
		ids := make([]string, 0, len(b.Sources))
		for _, source := range b.Sources {
			ids = append(ids, source.SourceID())
		}
		return "all:" + strings.Join(ids, "|")
	}
	if b.ID != 0 {
		return fmt.Sprint(b.ID)
	}
	return "space:" + strings.Join(b.Projects, ",")
}

// SprintScoped reports whether the sprint filter narrows this source. Sprints
// belong to boards, so a space always lists every ticket in it, and the
// all-sources selection is narrowed only through its boards.
func (b JiraBoard) SprintScoped(sprintOnly bool) bool {
	if !sprintOnly {
		return false
	}
	for _, member := range b.Members() {
		if member.ID != 0 {
			return true
		}
	}
	return false
}

// JiraConfig is the per-repository JIRA setup written under a profile.
type JiraConfig struct {
	Site              string      `json:"site"`
	Boards            []JiraBoard `json:"boards"`
	DefaultBoard      string      `json:"defaultBoard"`
	MineOnly          bool        `json:"mineOnly"`
	SprintOnly        bool        `json:"sprintOnly"`
	OpenOnly          bool        `json:"openOnly"`
	FiltersConfigured bool        `json:"filtersConfigured,omitempty"`
	Limit             int         `json:"limit"`
}

// Board returns the configured board with the given name, or nil.
func (c *JiraConfig) Board(name string) *JiraBoard {
	if c == nil {
		return nil
	}
	for i := range c.Boards {
		if c.Boards[i].Name == name {
			return &c.Boards[i]
		}
	}
	return nil
}

// AllSources is the selection that lists every configured board and space
// together.
func (c *JiraConfig) AllSources() JiraBoard {
	return JiraBoard{Name: JiraAllSources, Sources: append([]JiraBoard(nil), c.Boards...)}
}

// ActiveBoard returns the board or space named by DefaultBoard. Otherwise it
// returns every board and space together, or the only one when there is just
// one.
func (c *JiraConfig) ActiveBoard() *JiraBoard {
	if c == nil || len(c.Boards) == 0 {
		return nil
	}
	if board := c.Board(c.DefaultBoard); board != nil {
		return board
	}
	if len(c.Boards) == 1 {
		return &c.Boards[0]
	}
	all := c.AllSources()
	return &all
}

// normalizeJiraConfig fills in defaults for an opted-in JIRA block. It never
// creates one: profiles without JIRA configured stay byte-identical on rewrite.
func normalizeJiraConfig(profile *RepositoryProfile) bool {
	jira := profile.Jira
	if jira == nil {
		return false
	}

	changed := false
	if trimmed := strings.TrimSpace(jira.Site); trimmed != jira.Site {
		jira.Site = trimmed
		changed = true
	}

	boards := make([]JiraBoard, 0, len(jira.Boards))
	seen := map[string]struct{}{}
	for _, board := range jira.Boards {
		board.Name = strings.TrimSpace(board.Name)
		board.URL = strings.TrimSpace(board.URL)
		if !board.Configured() {
			changed = true
			continue
		}
		if board.Name == "" {
			board.Name = fmt.Sprintf("board %d", board.ID)
			if board.IsSpace() {
				board.Name = strings.Join(board.Projects, ", ")
			}
			changed = true
		}
		if _, ok := seen[board.Name]; ok {
			changed = true
			continue
		}
		seen[board.Name] = struct{}{}
		boards = append(boards, board)
	}
	if len(boards) != len(jira.Boards) {
		changed = true
	}
	jira.Boards = boards

	jira.DefaultBoard = strings.TrimSpace(jira.DefaultBoard)
	if jira.DefaultBoard != "" && jira.DefaultBoard != JiraAllSources && jira.Board(jira.DefaultBoard) == nil {
		jira.DefaultBoard = ""
		changed = true
	}
	// Tickets open on every board and space together unless one was picked.
	if jira.DefaultBoard == "" && len(jira.Boards) > 0 {
		jira.DefaultBoard = JiraAllSources
		changed = true
	}
	if jira.Limit <= 0 {
		jira.Limit = JiraDefaultLimit
		changed = true
	}
	if !jira.FiltersConfigured {
		jira.MineOnly = true
		jira.SprintOnly = true
		jira.OpenOnly = true
		jira.FiltersConfigured = true
		changed = true
	}

	// Tickets default to "assigned to me"; without boards there is nothing to show.
	if len(jira.Boards) == 0 {
		profile.Jira = nil
		return true
	}
	return changed
}
