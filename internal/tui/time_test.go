package tui

import (
	"testing"
	"time"

	"github.com/Jan/git-backtrack/internal/gitops"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestParseDuration(t *testing.T) {
	duration, ok := parseDuration("+2h")
	if !ok {
		t.Fatalf("parseDuration returned ok=false")
	}
	if duration != 2*time.Hour {
		t.Fatalf("duration = %s, want 2h", duration)
	}

	duration, ok = parseDuration("-30m")
	if !ok {
		t.Fatalf("parseDuration returned ok=false")
	}
	if duration != -30*time.Minute {
		t.Fatalf("duration = %s, want -30m", duration)
	}
}

func TestFormatPositiveTimeAdjustment(t *testing.T) {
	if got := formatPositiveTimeAdjustment(2 * time.Second); got != "+2s" {
		t.Fatalf("adjustment = %q, want +2s", got)
	}
	if got := formatPositiveTimeAdjustment(2 * time.Hour); got != "+2h" {
		t.Fatalf("adjustment = %q, want +2h", got)
	}
	if got := formatPositiveTimeAdjustment(1500 * time.Millisecond); got != "+2s" {
		t.Fatalf("adjustment = %q, want +2s", got)
	}
}

func TestParseDateTimeAcceptsMinutePrecision(t *testing.T) {
	got, err := parseDateTime("2024-01-02", "03:04", time.UTC)
	if err != nil {
		t.Fatalf("parseDateTime: %v", err)
	}
	want := time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("time = %s, want %s", got, want)
	}
}

func TestFormatCommitTimePreservesTimezoneOffset(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	commitTime := time.Date(2024, 1, 2, 3, 4, 5, 0, loc)

	got := formatCommitTime(commitTime, true)
	want := "2024-01-02 03:04 +0200"
	if got != want {
		t.Fatalf("formatCommitTime = %q, want %q", got, want)
	}
}

func TestFormatCommitTimeHidesTimezoneInLocalTime(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.FixedZone("local", -5*60*60)
	commitTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("test", 2*60*60))

	got := formatCommitTime(commitTime, false)
	want := "2024-01-01 20:04"
	if got != want {
		t.Fatalf("formatCommitTime = %q, want %q", got, want)
	}
}

func TestCalculateTimeSpreadDistributesFromOldest(t *testing.T) {
	oldest := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	middle := oldest.Add(1 * time.Hour)
	newest := oldest.Add(3 * time.Hour)
	commits := []gitops.CommitInfo{
		{Hash: plumbing.NewHash("3333333333333333333333333333333333333333"), AuthorDate: newest},
		{Hash: plumbing.NewHash("2222222222222222222222222222222222222222"), AuthorDate: middle},
		{Hash: plumbing.NewHash("1111111111111111111111111111111111111111"), AuthorDate: oldest},
	}
	selected := map[string]bool{
		commits[0].Hash.String(): true,
		commits[1].Hash.String(): true,
		commits[2].Hash.String(): true,
	}

	spread := calculateTimeSpread(commits, selected, 3*time.Hour, nil)

	if spread[commits[0].Hash.String()] != 3*time.Hour {
		t.Fatalf("newest spread = %s, want 3h", spread[commits[0].Hash.String()])
	}
	if spread[commits[1].Hash.String()] != 1*time.Hour {
		t.Fatalf("middle spread = %s, want 1h", spread[commits[1].Hash.String()])
	}
	if spread[commits[2].Hash.String()] != 0 {
		t.Fatalf("oldest spread = %s, want 0", spread[commits[2].Hash.String()])
	}
}

func TestDescendantSelectionIncludesRootAndChildren(t *testing.T) {
	root := plumbing.NewHash("2222222222222222222222222222222222222222")
	child := plumbing.NewHash("3333333333333333333333333333333333333333")
	mergeChild := plumbing.NewHash("4444444444444444444444444444444444444444")
	otherParent := plumbing.NewHash("5555555555555555555555555555555555555555")
	parent := plumbing.NewHash("1111111111111111111111111111111111111111")
	commits := []gitops.CommitInfo{
		{Hash: mergeChild, Parents: []plumbing.Hash{child, otherParent}},
		{Hash: child, Parents: []plumbing.Hash{root}},
		{Hash: otherParent},
		{Hash: root, Parents: []plumbing.Hash{parent}},
		{Hash: parent},
	}

	selected := descendantSelection(commits, root.String())

	for _, hash := range []plumbing.Hash{root, child, mergeChild} {
		if !selected[hash.String()] {
			t.Fatalf("selected[%s] = false, want true", hash.String()[:7])
		}
	}
	for _, hash := range []plumbing.Hash{parent, otherParent} {
		if selected[hash.String()] {
			t.Fatalf("selected[%s] = true, want false", hash.String()[:7])
		}
	}
}

func TestMinimumTimingFixAdjustmentUsesUnselectedParentBoundary(t *testing.T) {
	parentTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	parent := plumbing.NewHash("1111111111111111111111111111111111111111")
	root := plumbing.NewHash("2222222222222222222222222222222222222222")
	child := plumbing.NewHash("3333333333333333333333333333333333333333")
	commits := []gitops.CommitInfo{
		{Hash: child, AuthorDate: parentTime.Add(5 * time.Second), Parents: []plumbing.Hash{root}},
		{Hash: root, AuthorDate: parentTime.Add(-1 * time.Second), Parents: []plumbing.Hash{parent}},
		{Hash: parent, AuthorDate: parentTime},
	}
	selected := map[string]bool{root.String(): true, child.String(): true}

	adjustment := minimumTimingFixAdjustment(commits, selected, nil)

	if adjustment != 2*time.Second {
		t.Fatalf("adjustment = %s, want 2s", adjustment)
	}
}
