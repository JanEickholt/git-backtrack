package gitops

import (
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestCalculateSmartTimeAdjustUsesWorkingHoursForMultiDayAdjustments(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	commits := historicalCommitsAtHours([]int{10, 11, 14, 15, 16})
	selected := []CommitInfo{
		commitAt("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", 2024, 1, 3, 17, "fix: final"),
		commitAt("dddddddddddddddddddddddddddddddddddddddd", 2024, 1, 3, 13, "feat: fourth"),
		commitAt("cccccccccccccccccccccccccccccccccccccccc", 2024, 1, 3, 10, "feat: third"),
		commitAt("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 2024, 1, 2, 15, "feat: second"),
		commitAt("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 2024, 1, 2, 10, "feat: first"),
	}
	commits = append(selected, commits...)
	selectedHashes := map[string]bool{}
	for _, commit := range selected {
		selectedHashes[commit.Hash.String()] = true
	}

	adjustments := CalculateSmartTimeAdjust(commits, selectedHashes, 12*time.Hour)

	if adjustments[selected[0].Hash.String()] != 12*time.Hour {
		t.Fatalf("newest adjustment = %s, want 12h", adjustments[selected[0].Hash.String()])
	}
	if adjustments[selected[len(selected)-1].Hash.String()] != 0 {
		t.Fatalf("oldest adjustment = %s, want 0", adjustments[selected[len(selected)-1].Hash.String()])
	}
	for _, commit := range selected[1 : len(selected)-1] {
		adjusted := commit.AuthorDate.Add(adjustments[commit.Hash.String()]).In(time.Local)
		minute := adjusted.Hour()*60 + adjusted.Minute()
		if minute < defaultSmartDayStart || minute > defaultSmartDayEnd {
			t.Fatalf("adjusted time = %s, want within normal working hours", adjusted)
		}
	}
}

func TestSmartTimeProfileTreatsHistoryAsNudgeNotSourceOfTruth(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	commits := historicalCommitsAtHours([]int{0, 1, 2, 3, 23, 23, 23})
	profile := smartTimeProfileFromHistory(commits)

	if profile.startMinute < defaultSmartDayStart-maxSmartHistoryShift/2 || profile.startMinute > defaultSmartDayStart+maxSmartHistoryShift/2 {
		t.Fatalf("startMinute = %d, want near normal start", profile.startMinute)
	}
	if profile.endMinute < defaultSmartDayEnd-maxSmartHistoryShift/2 || profile.endMinute > defaultSmartDayEnd+maxSmartHistoryShift/2 {
		t.Fatalf("endMinute = %d, want near normal end", profile.endMinute)
	}
}

func historicalCommitsAtHours(hours []int) []CommitInfo {
	commits := make([]CommitInfo, 0, len(hours))
	for i, hour := range hours {
		hash := plumbing.NewHash(string(rune('1'+i)) + "111111111111111111111111111111111111111")
		commits = append(commits, CommitInfo{Hash: hash, AuthorDate: time.Date(2023, 12, 1+i, hour, 0, 0, 0, time.UTC), Message: "feat: history"})
	}
	return commits
}

func commitAt(hash string, year int, month time.Month, day, hour int, message string) CommitInfo {
	return CommitInfo{
		Hash:       plumbing.NewHash(hash),
		AuthorDate: time.Date(year, month, day, hour, 0, 0, 0, time.UTC),
		Message:    message,
		Additions:  10,
	}
}
