package gitops

import (
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestRenderGraphLineShowsCommitTimezone(t *testing.T) {
	commitTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("test", 2*60*60))
	commits := []CommitInfo{
		{
			Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
			ShortHash:  "1111111",
			AuthorName: "Test User",
			AuthorDate: commitTime,
			Message:    "message",
		},
	}

	line := RenderGraphLine(BuildGraph(commits), commits, 0, 120, DefaultGraphStyle(), false)
	if !strings.Contains(line, "2024-01-02 03:04 +0200") {
		t.Fatalf("rendered line %q does not include timezone offset", line)
	}
}

func TestParseGraphRowsKeepsConnectorRows(t *testing.T) {
	output := "* " + graphCommitMarker + "1111111111111111111111111111111111111111\n" +
		"|\\  \n" +
		"| * " + graphCommitMarker + "2222222222222222222222222222222222222222\n"

	graph := ParseGraphRows(output)
	if len(graph.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(graph.Rows))
	}
	if !graph.Rows[0].IsCommit || graph.Rows[0].Prefix != "* " {
		t.Fatalf("first row = %+v, want commit row", graph.Rows[0])
	}
	if graph.Rows[1].IsCommit || graph.Rows[1].Prefix != "|\\  " {
		t.Fatalf("second row = %+v, want connector row", graph.Rows[1])
	}
	if !graph.Rows[2].IsCommit || graph.Rows[2].Prefix != "| * " {
		t.Fatalf("third row = %+v, want nested commit row", graph.Rows[2])
	}
}

func TestRenderGraphLineTruncatesLongPrefix(t *testing.T) {
	graph := &Graph{
		Rows: []GraphRow{{Prefix: strings.Repeat("| ", 20), CommitIndex: -1}},
	}

	line := RenderGraphLine(graph, nil, 0, 10, DefaultGraphStyle(), false)
	if len(line) > 10 {
		t.Fatalf("line len = %d, want <= 10: %q", len(line), line)
	}
}

func TestCommitColumnFormatting(t *testing.T) {
	author := FormatCommitAuthor("Jan", 8)
	if len(author) != 8 {
		t.Fatalf("author len = %d, want %d", len(author), 8)
	}
	if author[:3] != "Jan" {
		t.Fatalf("author = %q, want prefix Jan", author)
	}

	stat := FormatCommitStat("+", 8, 4)
	if stat != "  +8" {
		t.Fatalf("stat = %q, want %q", stat, "  +8")
	}
}

func TestStatColumnWidthUsesLargestDisplayedCount(t *testing.T) {
	commits := []CommitInfo{
		{Additions: 8, Deletions: 12},
		{Additions: 1234, Deletions: 0},
	}

	width := StatColumnWidth(commits)
	if width != 5 {
		t.Fatalf("width = %d, want 5", width)
	}

	stat := FormatCommitStat("+", 8, width)
	if stat != "   +8" {
		t.Fatalf("stat = %q, want %q", stat, "   +8")
	}
}

func TestRenderCommitLineWithColumnWidthsKeepsAlignedSpacing(t *testing.T) {
	commit := CommitInfo{
		Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:  "1111111",
		AuthorName: "Jan",
		AuthorDate: time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Additions:  8,
		Deletions:  12,
		Message:    "message",
	}

	line := RenderCommitLineWithColumnWidths(commit, 120, false, "", 0, 8, 4)
	if !strings.Contains(line, "Jan       2024-01-02") {
		t.Fatalf("line does not keep padded author spacing: %q", line)
	}
	if !strings.Contains(line, "  +8  -12") {
		t.Fatalf("line does not keep padded stat spacing: %q", line)
	}
}

func TestRenderCommitLineWithColumnWidthsCanHideTimezone(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.FixedZone("local", -5*60*60)
	commit := CommitInfo{
		Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:  "1111111",
		AuthorName: "Jan",
		AuthorDate: time.Date(2024, 1, 2, 3, 4, 0, 0, time.FixedZone("test", 2*60*60)),
		Message:    "message",
	}

	line := RenderCommitLineWithColumnWidthsAndTimezone(commit, 120, false, "", 0, 3, 2, false)
	if !strings.Contains(line, "2024-01-01 20:04") {
		t.Fatalf("line does not show local time without timezone: %q", line)
	}
	if strings.Contains(line, "+0200") || strings.Contains(line, "-0500") {
		t.Fatalf("line should not include timezone offset: %q", line)
	}
}

func TestRenderCommitLineWithColumnWidthsCanShowEmail(t *testing.T) {
	commit := CommitInfo{
		Hash:        plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:   "1111111",
		AuthorName:  "Jan",
		AuthorEmail: "jan@example.com",
		AuthorDate:  time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Message:     "message",
	}
	width := AuthorColumnWidthWithEmail([]CommitInfo{commit}, true)

	line := RenderCommitLineWithColumnWidthsAndOptions(commit, 120, false, "", 0, width, 2, false, true, true)
	if !strings.Contains(line, "Jan <jan@example.com>") {
		t.Fatalf("line does not include email: %q", line)
	}

	line = RenderCommitLineWithColumnWidthsAndOptions(commit, 120, false, "", 0, width, 2, false, false, true)
	if strings.Contains(line, "jan@example.com") {
		t.Fatalf("line should not include email: %q", line)
	}
}

func TestRenderCommitLineWithColumnWidthsCanHideLineDiffs(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	commit := CommitInfo{
		Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:  "1111111",
		AuthorName: "Jan",
		AuthorDate: time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Additions:  8,
		Deletions:  12,
		Message:    "message",
	}

	line := RenderCommitLineWithColumnWidthsAndOptions(commit, 120, false, "", 0, 3, 4, false, false, false)
	if strings.Contains(line, "+8") || strings.Contains(line, "-12") {
		t.Fatalf("line should not include line diffs: %q", line)
	}
	if !strings.Contains(line, "2024-01-02 03:04  message") {
		t.Fatalf("line should keep spacing between date and message: %q", line)
	}
}
