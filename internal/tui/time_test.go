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

	got := formatCommitTime(commitTime)
	want := "2024-01-02 03:04 +0200"
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
