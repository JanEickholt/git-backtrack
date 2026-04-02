package tui

import "github.com/Jan/git-backtrack/internal/gitops"

func graphRowForCommit(graph *gitops.Graph, commitIndex int) int {
	if graph == nil || commitIndex < 0 {
		return 0
	}
	for rowIndex, row := range graph.Rows {
		if row.IsCommit && row.CommitIndex == commitIndex {
			return rowIndex
		}
	}
	return 0
}

func scrollOffsetForSelectedCommit(graph *gitops.Graph, selectedCommit, currentOffset, maxRows int) int {
	if graph == nil || len(graph.Rows) == 0 {
		return 0
	}
	if maxRows <= 0 {
		maxRows = 1
	}

	selectedRow := graphRowForCommit(graph, selectedCommit)
	newOffset := currentOffset
	if selectedRow < newOffset {
		newOffset = selectedRow
	} else if selectedRow >= newOffset+maxRows {
		newOffset = selectedRow - maxRows + 1
	}

	maxOffset := len(graph.Rows) - maxRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if newOffset > maxOffset {
		newOffset = maxOffset
	}
	if newOffset < 0 {
		newOffset = 0
	}
	return newOffset
}

func scrollOffsetForSelectedIndex(totalItems, selectedIndex, currentOffset, maxRows int) int {
	if totalItems <= 0 {
		return 0
	}
	if maxRows <= 0 {
		maxRows = 1
	}
	if selectedIndex < 0 {
		selectedIndex = 0
	}
	if selectedIndex >= totalItems {
		selectedIndex = totalItems - 1
	}

	newOffset := currentOffset
	if selectedIndex < newOffset {
		newOffset = selectedIndex
	} else if selectedIndex >= newOffset+maxRows {
		newOffset = selectedIndex - maxRows + 1
	}

	maxOffset := totalItems - maxRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if newOffset > maxOffset {
		newOffset = maxOffset
	}
	if newOffset < 0 {
		newOffset = 0
	}
	return newOffset
}
