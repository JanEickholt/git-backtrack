package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Jan/git-backtrack/internal/gitops"
)

func formatCommitTime(t time.Time) string {
	return t.Format("2006-01-02 15:04 -0700")
}

func adjustTime(original time.Time, adjustment string) (time.Time, error) {
	adj := strings.TrimSpace(adjustment)
	if len(adj) < 2 {
		return original, fmt.Errorf("invalid time adjustment")
	}

	sign := 1
	if adj[0] == '-' {
		sign = -1
		adj = adj[1:]
	} else if adj[0] == '+' {
		adj = adj[1:]
	}

	var amount int
	var unit string
	if _, err := fmt.Sscanf(adj, "%d%s", &amount, &unit); err != nil {
		return original, fmt.Errorf("invalid time adjustment format")
	}

	duration := time.Duration(amount)
	switch unit {
	case "s", "sec", "second", "seconds":
		duration *= time.Second
	case "m", "min", "minute", "minutes":
		duration *= time.Minute
	case "h", "hour", "hours":
		duration *= time.Hour
	case "d", "day", "days":
		duration *= 24 * time.Hour
	case "w", "week", "weeks":
		duration *= 7 * 24 * time.Hour
	default:
		return original, fmt.Errorf("unknown time unit: %s", unit)
	}

	if sign < 0 {
		return original.Add(-duration), nil
	}
	return original.Add(duration), nil
}

func parseDuration(adjustment string) (time.Duration, bool) {
	adj := strings.TrimSpace(adjustment)
	if len(adj) < 2 {
		return 0, false
	}

	sign := 1
	if adj[0] == '-' {
		sign = -1
		adj = adj[1:]
	} else if adj[0] == '+' {
		adj = adj[1:]
	}

	var amount int
	var unit string
	if _, err := fmt.Sscanf(adj, "%d%s", &amount, &unit); err != nil {
		return 0, false
	}

	duration := time.Duration(amount)
	switch unit {
	case "s", "sec", "second", "seconds":
		duration *= time.Second
	case "m", "min", "minute", "minutes":
		duration *= time.Minute
	case "h", "hour", "hours":
		duration *= time.Hour
	case "d", "day", "days":
		duration *= 24 * time.Hour
	case "w", "week", "weeks":
		duration *= 7 * 24 * time.Hour
	default:
		return 0, false
	}

	if sign < 0 {
		return -duration, true
	}
	return duration, true
}

func calculateTimeSpread(
	commits []gitops.CommitInfo,
	selectedHashes map[string]bool,
	timeToAdd time.Duration,
	editMap map[string]*gitops.ForgeChange,
) map[string]time.Duration {
	result := make(map[string]time.Duration)

	var selectedCommits []gitops.CommitInfo
	for _, commit := range commits {
		if selectedHashes[commit.Hash.String()] {
			selectedCommits = append(selectedCommits, commit)
		}
	}
	if len(selectedCommits) < 2 {
		return result
	}

	getEffectiveDate := func(commit gitops.CommitInfo) time.Time {
		hashStr := commit.Hash.String()
		if editMap != nil && editMap[hashStr] != nil && editMap[hashStr].NewDate != nil {
			return *editMap[hashStr].NewDate
		}
		return commit.AuthorDate
	}

	gaps := make([]time.Duration, len(selectedCommits)-1)
	var totalSpan time.Duration
	for i := 0; i < len(gaps); i++ {
		currentDate := getEffectiveDate(selectedCommits[i])
		nextDate := getEffectiveDate(selectedCommits[i+1])
		gap := currentDate.Sub(nextDate)
		if gap < 0 {
			gap = -gap
		}
		gaps[i] = gap
		totalSpan += gap
	}

	if totalSpan == 0 {
		equalShare := timeToAdd / time.Duration(len(selectedCommits)-1)
		for i := 0; i < len(selectedCommits)-1; i++ {
			result[selectedCommits[i].Hash.String()] = equalShare
		}
		result[selectedCommits[len(selectedCommits)-1].Hash.String()] = 0
		return result
	}

	for i := 0; i < len(selectedCommits); i++ {
		distanceFromOldest := time.Duration(0)
		for j := i; j < len(gaps); j++ {
			distanceFromOldest += gaps[j]
		}
		proportion := float64(distanceFromOldest) / float64(totalSpan)
		result[selectedCommits[i].Hash.String()] = time.Duration(float64(timeToAdd) * proportion)
	}

	return result
}

func parseDateTime(dateStr, timeStr string, loc *time.Location) (time.Time, error) {
	datetimeStr := dateStr + " " + timeStr

	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		t, err := time.ParseInLocation(layout, datetimeStr, loc)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse datetime: %s", datetimeStr)
}

func hasTimeAnomaly(commit gitops.CommitInfo, allCommits []gitops.CommitInfo, editMap map[string]*gitops.ForgeChange) bool {
	hashToCommit := make(map[string]gitops.CommitInfo)
	for _, c := range allCommits {
		hashToCommit[c.Hash.String()] = c
	}

	localLoc := time.Local
	commitDate := commit.AuthorDate.In(localLoc)
	if change, ok := editMap[commit.Hash.String()]; ok && change.NewDate != nil {
		commitDate = change.NewDate.In(localLoc)
	}

	for _, parentHash := range commit.Parents {
		if parent, ok := hashToCommit[parentHash.String()]; ok {
			parentDate := parent.AuthorDate.In(localLoc)
			if change, ok := editMap[parentHash.String()]; ok && change.NewDate != nil {
				parentDate = change.NewDate.In(localLoc)
			}
			if commitDate.Before(parentDate) {
				return true
			}
		}
	}
	return false
}
