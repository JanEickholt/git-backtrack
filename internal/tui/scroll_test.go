package tui

import (
	"testing"

	"github.com/Jan/git-backtrack/internal/gitops"
)

func TestScrollOffsetForSelectedCommitKeepsSelectionVisible(t *testing.T) {
	graph := &gitops.Graph{
		Rows: []gitops.GraphRow{
			{IsCommit: true, CommitIndex: 0},
			{Prefix: "|\\", CommitIndex: -1},
			{IsCommit: true, CommitIndex: 1},
			{Prefix: "|/", CommitIndex: -1},
			{IsCommit: true, CommitIndex: 2},
		},
	}

	offset := scrollOffsetForSelectedCommit(graph, 2, 0, 3)
	if offset != 2 {
		t.Fatalf("offset = %d, want 2", offset)
	}

	offset = scrollOffsetForSelectedCommit(graph, 0, 2, 3)
	if offset != 0 {
		t.Fatalf("offset = %d, want 0", offset)
	}
}

func TestScrollOffsetForSelectedCommitHandlesLargeJumpOverConnectors(t *testing.T) {
	graph := &gitops.Graph{}
	for i := 0; i < 30; i++ {
		graph.Rows = append(graph.Rows,
			gitops.GraphRow{IsCommit: true, CommitIndex: i},
			gitops.GraphRow{Prefix: "|\\", CommitIndex: -1},
			gitops.GraphRow{Prefix: "| |", CommitIndex: -1},
		)
	}

	offset := scrollOffsetForSelectedCommit(graph, 20, 0, 10)
	selectedRow := graphRowForCommit(graph, 20)
	if selectedRow < offset || selectedRow >= offset+10 {
		t.Fatalf("selected row %d is outside viewport [%d, %d)", selectedRow, offset, offset+10)
	}
}

func TestGraphRowForCommitScansRowsByCommitIndex(t *testing.T) {
	graph := &gitops.Graph{
		Rows: []gitops.GraphRow{
			{IsCommit: true, CommitIndex: 2},
			{Prefix: "|", CommitIndex: -1},
			{IsCommit: true, CommitIndex: 0},
		},
	}

	row := graphRowForCommit(graph, 0)
	if row != 2 {
		t.Fatalf("row = %d, want 2", row)
	}
}

func TestScrollOffsetForSelectedIndexKeepsSelectionVisible(t *testing.T) {
	offset := scrollOffsetForSelectedIndex(20, 12, 0, 5)
	if offset != 8 {
		t.Fatalf("offset = %d, want 8", offset)
	}

	offset = scrollOffsetForSelectedIndex(20, 2, 8, 5)
	if offset != 2 {
		t.Fatalf("offset = %d, want 2", offset)
	}

	offset = scrollOffsetForSelectedIndex(3, 2, 8, 5)
	if offset != 0 {
		t.Fatalf("offset = %d, want 0", offset)
	}
}
