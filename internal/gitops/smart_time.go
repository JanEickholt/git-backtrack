package gitops

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	defaultSmartDayStart = 9 * 60
	defaultSmartDayEnd   = 18 * 60
	minSmartDayMinutes   = 4 * 60
	maxSmartHistoryShift = 2 * 60
)

// CalculateSmartTimeAdjust returns per-commit date deltas for smart_adjust.
// For single-day ranges it weights gaps by commit effort/type. For multi-day
// positive adjustments it projects intermediate commits onto the repository's
// usual commit-time window so expanded history avoids implausible night gaps.
func CalculateSmartTimeAdjust(
	commits []CommitInfo,
	selectedHashes map[string]bool,
	timeToAdd time.Duration,
) map[string]time.Duration {
	result := make(map[string]time.Duration)

	selectedCommits := make([]CommitInfo, 0)
	for _, commit := range commits {
		if selectedHashes[commit.Hash.String()] {
			selectedCommits = append(selectedCommits, commit)
		}
	}
	if len(selectedCommits) < 2 || timeToAdd == 0 {
		return result
	}

	if shouldUseHistoryAwareSmartAdjust(selectedCommits, timeToAdd) {
		if adjusted := calculateHistoryAwareSmartAdjust(commits, selectedCommits, timeToAdd); len(adjusted) > 0 {
			return adjusted
		}
	}

	return calculateWeightedSmartAdjust(selectedCommits, timeToAdd)
}

func calculateWeightedSmartAdjust(selectedCommits []CommitInfo, timeToAdd time.Duration) map[string]time.Duration {
	result := make(map[string]time.Duration)
	weights := make([]float64, len(selectedCommits)-1)
	var totalWeight float64
	for i := 0; i < len(weights); i++ {
		weight := SmartCommitWeight(selectedCommits[i])
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

func shouldUseHistoryAwareSmartAdjust(selectedCommits []CommitInfo, timeToAdd time.Duration) bool {
	if timeToAdd < 8*time.Hour {
		return false
	}
	oldest := selectedCommits[len(selectedCommits)-1].AuthorDate.In(time.Local)
	newestTarget := selectedCommits[0].AuthorDate.Add(timeToAdd).In(time.Local)
	if !newestTarget.After(oldest) {
		return false
	}
	return !sameLocalDate(oldest, newestTarget)
}

func calculateHistoryAwareSmartAdjust(commits, selectedCommits []CommitInfo, timeToAdd time.Duration) map[string]time.Duration {
	oldToNew := make([]CommitInfo, len(selectedCommits))
	for i := range selectedCommits {
		oldToNew[len(selectedCommits)-1-i] = selectedCommits[i]
	}

	start := oldToNew[0].AuthorDate
	end := oldToNew[len(oldToNew)-1].AuthorDate.Add(timeToAdd)
	if !end.After(start) {
		return nil
	}

	profile := smartTimeProfileFromHistory(commits)
	available := profile.workDurationBetween(start, end)
	if available <= 0 {
		return nil
	}

	gapWeights := make([]float64, len(oldToNew)-1)
	var totalWeight float64
	for i := 0; i < len(gapWeights); i++ {
		weight := SmartCommitWeight(oldToNew[i+1])
		gapWeights[i] = weight
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return nil
	}

	result := make(map[string]time.Duration, len(oldToNew))
	result[oldToNew[0].Hash.String()] = 0
	result[oldToNew[len(oldToNew)-1].Hash.String()] = timeToAdd

	cumulativeWeight := 0.0
	for i := 1; i < len(oldToNew)-1; i++ {
		cumulativeWeight += gapWeights[i-1]
		workOffset := time.Duration(float64(available) * cumulativeWeight / totalWeight)
		target := profile.addWorkDuration(start, workOffset)
		minTarget := oldToNew[i].AuthorDate
		maxTarget := oldToNew[i].AuthorDate.Add(timeToAdd)
		if target.Before(minTarget) {
			target = minTarget
		}
		if target.After(maxTarget) {
			target = maxTarget
		}
		result[oldToNew[i].Hash.String()] = target.Sub(oldToNew[i].AuthorDate)
	}
	return result
}

func SmartCommitWeight(commit CommitInfo) float64 {
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

type smartTimeProfile struct {
	startMinute int
	endMinute   int
}

func smartTimeProfileFromHistory(commits []CommitInfo) smartTimeProfile {
	minutes := make([]int, 0, len(commits))
	for _, commit := range commits {
		local := commit.AuthorDate.In(time.Local)
		minutes = append(minutes, local.Hour()*60+local.Minute())
	}
	if len(minutes) < 5 {
		return smartTimeProfile{startMinute: defaultSmartDayStart, endMinute: defaultSmartDayEnd}
	}
	sort.Ints(minutes)
	historyStart := percentileMinute(minutes, 0.10)
	historyEnd := percentileMinute(minutes, 0.90)
	start := blendNormalAndHistoryMinute(defaultSmartDayStart, historyStart)
	end := blendNormalAndHistoryMinute(defaultSmartDayEnd, historyEnd)
	if end-start < minSmartDayMinutes {
		median := blendNormalAndHistoryMinute((defaultSmartDayStart+defaultSmartDayEnd)/2, percentileMinute(minutes, 0.50))
		start = median - minSmartDayMinutes/2
		end = median + minSmartDayMinutes/2
	}
	if start < 0 {
		end -= start
		start = 0
	}
	if end > 24*60-1 {
		start -= end - (24*60 - 1)
		end = 24*60 - 1
		if start < 0 {
			start = 0
		}
	}
	if end <= start {
		return smartTimeProfile{startMinute: defaultSmartDayStart, endMinute: defaultSmartDayEnd}
	}
	return smartTimeProfile{startMinute: start, endMinute: end}
}

func blendNormalAndHistoryMinute(normal, history int) int {
	delta := history - normal
	if delta > maxSmartHistoryShift {
		delta = maxSmartHistoryShift
	}
	if delta < -maxSmartHistoryShift {
		delta = -maxSmartHistoryShift
	}
	return normal + delta/2
}

func percentileMinute(sortedMinutes []int, percentile float64) int {
	if len(sortedMinutes) == 0 {
		return defaultSmartDayStart
	}
	index := int(math.Round(percentile * float64(len(sortedMinutes)-1)))
	if index < 0 {
		index = 0
	}
	if index >= len(sortedMinutes) {
		index = len(sortedMinutes) - 1
	}
	return sortedMinutes[index]
}

func (p smartTimeProfile) workDurationBetween(start, end time.Time) time.Duration {
	if !end.After(start) {
		return 0
	}
	cur := p.normalizeForward(start)
	var total time.Duration
	for cur.Before(end) {
		_, windowEnd := p.windowFor(cur)
		segmentEnd := end
		if windowEnd.Before(segmentEnd) {
			segmentEnd = windowEnd
		}
		if segmentEnd.After(cur) {
			total += segmentEnd.Sub(cur)
		}
		cur = p.nextWindowStart(cur)
	}
	return total
}

func (p smartTimeProfile) addWorkDuration(start time.Time, duration time.Duration) time.Time {
	cur := p.normalizeForward(start)
	remaining := duration
	for remaining > 0 {
		_, windowEnd := p.windowFor(cur)
		available := windowEnd.Sub(cur)
		if remaining <= available {
			return cur.Add(remaining)
		}
		remaining -= available
		cur = p.nextWindowStart(cur)
	}
	return cur
}

func (p smartTimeProfile) normalizeForward(t time.Time) time.Time {
	windowStart, windowEnd := p.windowFor(t)
	if t.Before(windowStart) {
		return windowStart
	}
	if !t.Before(windowEnd) {
		return p.nextWindowStart(t)
	}
	return t
}

func (p smartTimeProfile) windowFor(t time.Time) (time.Time, time.Time) {
	local := t.In(time.Local)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	start := dayStart.Add(time.Duration(p.startMinute) * time.Minute)
	end := dayStart.Add(time.Duration(p.endMinute) * time.Minute)
	return start, end
}

func (p smartTimeProfile) nextWindowStart(t time.Time) time.Time {
	local := t.In(time.Local)
	nextDay := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, time.Local)
	return nextDay.Add(time.Duration(p.startMinute) * time.Minute)
}

func sameLocalDate(a, b time.Time) bool {
	a = a.In(time.Local)
	b = b.In(time.Local)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
