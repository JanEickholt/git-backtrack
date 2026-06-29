package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Jan/git-backtrack/internal/gitops"
)

func formatCommitTime(t time.Time, showTimezone bool) string {
	if showTimezone {
		return t.Format("2006-01-02 15:04 -0700")
	}
	return t.In(time.Local).Format("2006-01-02 15:04")
}

func adjustTime(original time.Time, adjustment string) (time.Time, error) {
	duration, ok := parseDuration(adjustment)
	if !ok {
		return original, fmt.Errorf("invalid time adjustment")
	}
	return original.Add(duration), nil
}

func parseDuration(adjustment string) (time.Duration, bool) {
	adj := strings.TrimSpace(adjustment)
	if adj == "" {
		return 0, false
	}

	var total time.Duration
	i := 0
	for i < len(adj) {
		for i < len(adj) && adj[i] == ' ' {
			i++
		}
		if i >= len(adj) {
			break
		}

		sign := int64(1)
		if adj[i] == '-' {
			sign = -1
			i++
		} else if adj[i] == '+' {
			i++
		}
		for i < len(adj) && adj[i] == ' ' {
			i++
		}

		amountStart := i
		for i < len(adj) && adj[i] >= '0' && adj[i] <= '9' {
			i++
		}
		if amountStart == i {
			return 0, false
		}
		amount, err := strconv.ParseInt(adj[amountStart:i], 10, 64)
		if err != nil {
			return 0, false
		}

		unitStart := i
		for i < len(adj) && ((adj[i] >= 'a' && adj[i] <= 'z') || (adj[i] >= 'A' && adj[i] <= 'Z')) {
			i++
		}
		if unitStart == i {
			return 0, false
		}
		unit := strings.ToLower(adj[unitStart:i])
		unitDuration, ok := durationUnit(unit)
		if !ok {
			return 0, false
		}
		total += time.Duration(sign*amount) * unitDuration

		for i < len(adj) && adj[i] == ' ' {
			i++
		}
		if i < len(adj) && adj[i] != '+' && adj[i] != '-' && (adj[i] < '0' || adj[i] > '9') {
			return 0, false
		}
	}
	return total, true
}

func durationUnit(unit string) (time.Duration, bool) {
	switch unit {
	case "s", "sec", "second", "seconds":
		return time.Second, true
	case "m", "min", "minute", "minutes":
		return time.Minute, true
	case "h", "hour", "hours":
		return time.Hour, true
	case "d", "day", "days":
		return 24 * time.Hour, true
	case "w", "week", "weeks":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func formatPositiveTimeAdjustment(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	seconds := int64((duration + time.Second - 1) / time.Second)
	units := []struct {
		suffix  string
		seconds int64
	}{
		{suffix: "w", seconds: int64((7 * 24 * time.Hour) / time.Second)},
		{suffix: "d", seconds: int64((24 * time.Hour) / time.Second)},
		{suffix: "h", seconds: int64(time.Hour / time.Second)},
		{suffix: "m", seconds: int64(time.Minute / time.Second)},
		{suffix: "s", seconds: 1},
	}
	for _, unit := range units {
		if seconds%unit.seconds == 0 {
			return fmt.Sprintf("+%d%s", seconds/unit.seconds, unit.suffix)
		}
	}
	return fmt.Sprintf("+%ds", seconds)
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

func calculateSmartTimeAdjust(
	commits []gitops.CommitInfo,
	selectedHashes map[string]bool,
	timeToAdd time.Duration,
) map[string]time.Duration {
	result := make(map[string]time.Duration)

	selectedCommits := make([]gitops.CommitInfo, 0)
	for _, commit := range commits {
		if selectedHashes[commit.Hash.String()] {
			selectedCommits = append(selectedCommits, commit)
		}
	}
	if len(selectedCommits) < 2 || timeToAdd == 0 {
		return result
	}

	weights := make([]float64, len(selectedCommits)-1)
	var totalWeight float64
	for i := 0; i < len(weights); i++ {
		weight := smartCommitWeight(selectedCommits[i])
		weights[i] = weight
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return result
	}

	cumulative := time.Duration(0)
	oldestIndex := len(selectedCommits) - 1
	result[selectedCommits[oldestIndex].Hash.String()] = 0
	for i := oldestIndex - 1; i >= 0; i-- {
		gap := time.Duration(float64(timeToAdd) * weights[i] / totalWeight)
		cumulative += gap
		result[selectedCommits[i].Hash.String()] = cumulative
	}
	result[selectedCommits[0].Hash.String()] = timeToAdd
	return result
}

func smartCommitWeight(commit gitops.CommitInfo) float64 {
	changedLines := commit.Additions + commit.Deletions
	if changedLines < 0 {
		changedLines = 0
	}

	weight := 1.0 + math.Sqrt(float64(changedLines))
	message := strings.ToLower(strings.TrimSpace(commit.Message))
	subject := strings.SplitN(message, "\n", 2)[0]

	switch {
	case strings.HasPrefix(subject, "chore") || strings.HasPrefix(subject, "docs"):
		weight *= 0.45
	case strings.HasPrefix(subject, "test"):
		weight *= 0.65
	case strings.HasPrefix(subject, "fix"):
		weight *= 1.15
	case strings.HasPrefix(subject, "feat"):
		weight *= 1.2
	case strings.HasPrefix(subject, "refactor"):
		weight *= 1.05
	}

	if strings.Contains(subject, "lint") || strings.Contains(subject, "format") {
		weight *= 0.5
	}
	if strings.Contains(subject, "error") || strings.Contains(subject, "bug") || strings.Contains(subject, "crash") {
		weight *= 1.15
	}

	if weight < 0.1 {
		return 0.1
	}
	return weight
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

func effectiveCommitDate(commit gitops.CommitInfo, editMap map[string]*gitops.ForgeChange) time.Time {
	if change, ok := editMap[commit.Hash.String()]; ok && change.NewDate != nil {
		return *change.NewDate
	}
	return commit.AuthorDate
}

func hasTimeAnomaly(commit gitops.CommitInfo, allCommits []gitops.CommitInfo, editMap map[string]*gitops.ForgeChange) bool {
	hashToCommit := make(map[string]gitops.CommitInfo)
	for _, c := range allCommits {
		hashToCommit[c.Hash.String()] = c
	}

	localLoc := time.Local
	commitDate := effectiveCommitDate(commit, editMap).In(localLoc)

	for _, parentHash := range commit.Parents {
		if parent, ok := hashToCommit[parentHash.String()]; ok {
			parentDate := effectiveCommitDate(parent, editMap).In(localLoc)
			if commitDate.Before(parentDate) {
				return true
			}
		}
	}
	return false
}

func descendantSelection(commits []gitops.CommitInfo, rootHash string) map[string]bool {
	childrenByParent := make(map[string][]string)
	commitExists := make(map[string]bool, len(commits))
	for _, commit := range commits {
		hash := commit.Hash.String()
		commitExists[hash] = true
		for _, parent := range commit.Parents {
			parentHash := parent.String()
			childrenByParent[parentHash] = append(childrenByParent[parentHash], hash)
		}
	}

	selected := make(map[string]bool)
	if !commitExists[rootHash] {
		return selected
	}
	stack := []string{rootHash}
	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if selected[hash] {
			continue
		}
		selected[hash] = true
		stack = append(stack, childrenByParent[hash]...)
	}
	return selected
}

func minimumTimingFixAdjustment(commits []gitops.CommitInfo, selected map[string]bool, editMap map[string]*gitops.ForgeChange) time.Duration {
	hashToCommit := make(map[string]gitops.CommitInfo, len(commits))
	for _, commit := range commits {
		hashToCommit[commit.Hash.String()] = commit
	}

	var adjustment time.Duration
	for _, commit := range commits {
		hash := commit.Hash.String()
		if !selected[hash] {
			continue
		}
		commitDate := effectiveCommitDate(commit, editMap)
		for _, parentHash := range commit.Parents {
			parentHashStr := parentHash.String()
			if selected[parentHashStr] {
				continue
			}
			parent, ok := hashToCommit[parentHashStr]
			if !ok {
				continue
			}
			needed := effectiveCommitDate(parent, editMap).Add(time.Second).Sub(commitDate)
			if needed > adjustment {
				adjustment = needed
			}
		}
	}
	if adjustment <= 0 {
		return 0
	}
	return ((adjustment + time.Second - 1) / time.Second) * time.Second
}
