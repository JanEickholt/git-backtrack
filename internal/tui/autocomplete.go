package tui

import (
	"strings"

	"github.com/Jan/git-backtrack/internal/gitops"
)

const maxAuthorCompletions = 5
const noCompletionIndex = -1

type authorCompletionKind int

const (
	authorCompletionName authorCompletionKind = iota
	authorCompletionEmail
)

func authorCompletionCandidates(commits []gitops.CommitInfo, kind authorCompletionKind, query string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	query = strings.TrimSpace(query)
	lowerQuery := strings.ToLower(query)
	seen := make(map[string]bool)
	values := make([]string, 0, len(commits))
	for _, commit := range commits {
		value := commit.AuthorName
		if kind == authorCompletionEmail {
			value = commit.AuthorEmail
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, value)
	}

	if query == "" {
		if len(values) > limit {
			return values[:limit]
		}
		return values
	}

	var candidates []string
	addMatching := func(match func(string) bool) {
		for _, value := range values {
			if len(candidates) >= limit {
				return
			}
			lowerValue := strings.ToLower(value)
			if strings.EqualFold(value, query) || !match(lowerValue) {
				continue
			}
			alreadyAdded := false
			for _, candidate := range candidates {
				if strings.EqualFold(candidate, value) {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				candidates = append(candidates, value)
			}
		}
	}

	addMatching(func(value string) bool { return strings.HasPrefix(value, lowerQuery) })
	addMatching(func(value string) bool { return strings.Contains(value, lowerQuery) })
	return candidates
}

func editCompletionKind(field EditField) (authorCompletionKind, bool) {
	switch field {
	case FieldName:
		return authorCompletionName, true
	case Email:
		return authorCompletionEmail, true
	default:
		return authorCompletionName, false
	}
}

func batchCompletionKind(index int) (authorCompletionKind, bool) {
	switch index {
	case 0:
		return authorCompletionName, true
	case 1:
		return authorCompletionEmail, true
	default:
		return authorCompletionName, false
	}
}

func normalizedCompletionIndex(index int, candidates []string) int {
	if len(candidates) == 0 {
		return 0
	}
	index %= len(candidates)
	if index < 0 {
		index += len(candidates)
	}
	return index
}
