package tui

import (
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

func singleGlyphIcons(set iconSet) map[string]string {
	return map[string]string{
		"WTMain": set.WTMain, "WTClean": set.WTClean, "WTDirty": set.WTDirty,
		"WTConflict": set.WTConflict, "WTMissing": set.WTMissing, "WTPrunable": set.WTPrunable,
		"WTDetached": set.WTDetached,
		"Ahead":      set.Ahead, "Behind": set.Behind, "Synced": set.Synced,
		"PROpen": set.PROpen, "PRMerged": set.PRMerged, "PRClosed": set.PRClosed, "PRDraft": set.PRDraft,
		"CheckPass": set.CheckPass, "CheckFail": set.CheckFail, "CheckPending": set.CheckPending,
		"CheckNone":      set.CheckNone,
		"ReviewApproved": set.ReviewApproved, "ReviewChanges": set.ReviewChanges,
		"ReviewRequired": set.ReviewRequired,
		"OK":             set.OK, "Err": set.Err, "Warn": set.Warn,
		"Section": set.Section, "Pointer": set.Pointer, "Dot": set.Dot,
		"SkeletonBlock": set.SkeletonBlock,
	}
}

func TestIconsAreSingleWidth(t *testing.T) {
	for setName, set := range map[string]iconSet{"unicode": unicodeIcons, "ascii": asciiIcons} {
		for name, icon := range singleGlyphIcons(set) {
			if icon == "" {
				t.Fatalf("%s icon %s is empty", setName, name)
			}
			if width := runewidth.StringWidth(icon); width != 1 {
				t.Fatalf("%s icon %s = %q has width %d, want 1", setName, name, icon, width)
			}
		}
	}
}

func TestAsciiIconFallback(t *testing.T) {
	ascii := pickIconSet(func(key string) string {
		if key == "WT_MANAGER_ASCII" {
			return "1"
		}
		return ""
	})
	if ascii != asciiIcons {
		t.Fatal("WT_MANAGER_ASCII did not select the ascii icon set")
	}
	unicode := pickIconSet(func(string) string { return "" })
	if unicode != unicodeIcons {
		t.Fatal("default icon set is not unicode")
	}
}

func TestChecksDisplay(t *testing.T) {
	cases := []struct {
		name   string
		checks []StatusCheck
		want   string
	}{
		{"none", nil, icons.CheckNone},
		{"pass", []StatusCheck{{Status: "completed", Conclusion: "success"}}, icons.CheckPass + " 1"},
		{"fail wins", []StatusCheck{{Status: "completed", Conclusion: "failure"}, {Status: "completed", Conclusion: "success"}}, icons.CheckFail + " 1"},
		{"pending", []StatusCheck{{Status: "queued"}}, icons.CheckPending + " 1"},
	}
	for _, tc := range cases {
		if got := checksDisplay(tc.checks); got != tc.want {
			t.Fatalf("%s: checksDisplay = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestStateDisplayUsesIconAndLabel(t *testing.T) {
	wt := Worktree{Status: RepoStatus{Dirty: true}}
	if got := stateDisplay(wt); got != icons.WTDirty+" dirty" {
		t.Fatalf("stateDisplay = %q", got)
	}
	main := Worktree{IsMain: true}
	if got := stateDisplay(main); got != icons.WTMain+" main" {
		t.Fatalf("stateDisplay(main) = %q", got)
	}
}

func TestShortDuration(t *testing.T) {
	cases := []struct {
		duration time.Duration
		want     string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{72 * time.Hour, "3d"},
	}
	for _, tc := range cases {
		if got := shortDuration(tc.duration); got != tc.want {
			t.Fatalf("shortDuration(%v) = %q, want %q", tc.duration, got, tc.want)
		}
	}
}

func TestTrackingDisplay(t *testing.T) {
	if got := trackingDisplay(RepoStatus{}); got != icons.Synced+" up to date" {
		t.Fatalf("trackingDisplay synced = %q", got)
	}
	if got := trackingDisplay(RepoStatus{Ahead: 2, Behind: 1}); got != icons.Ahead+"2 "+icons.Behind+"1" {
		t.Fatalf("trackingDisplay ahead/behind = %q", got)
	}
}
