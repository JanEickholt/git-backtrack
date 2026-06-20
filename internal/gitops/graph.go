package gitops

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/go-git/go-git/v5/plumbing"
)

const graphCommitMarker = "\x1e"

type GraphRow struct {
	Prefix      string
	CommitHash  plumbing.Hash
	CommitIndex int
	IsCommit    bool
}

type Graph struct {
	Rows       []GraphRow
	CommitRows []int
}

type GraphStyle struct {
	CommitDot    lipgloss.Style
	MergeDot     lipgloss.Style
	VerticalLine lipgloss.Style
	BranchLine   lipgloss.Style
}

func DefaultGraphStyle() GraphStyle {
	return GraphStyle{
		CommitDot: lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")),
		MergeDot: lipgloss.NewStyle().
			Foreground(lipgloss.Color("13")),
		VerticalLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),
		BranchLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")),
	}
}

func ParseGraphRows(output string) *Graph {
	graph := &Graph{}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return graph
	}

	for _, line := range lines {
		markerIndex := strings.Index(line, graphCommitMarker)
		if markerIndex == -1 {
			graph.Rows = append(graph.Rows, GraphRow{
				Prefix:      line,
				CommitIndex: -1,
			})
			continue
		}

		hashText := strings.TrimSpace(line[markerIndex+len(graphCommitMarker):])
		row := GraphRow{
			Prefix:      line[:markerIndex],
			CommitHash:  plumbing.NewHash(hashText),
			CommitIndex: -1,
			IsCommit:    true,
		}
		graph.CommitRows = append(graph.CommitRows, len(graph.Rows))
		graph.Rows = append(graph.Rows, row)
	}

	return graph
}

func (g *Graph) AttachCommitIndexes(commits []CommitInfo) {
	if g == nil {
		return
	}
	hashToIndex := make(map[plumbing.Hash]int, len(commits))
	for i, commit := range commits {
		hashToIndex[commit.Hash] = i
	}

	g.CommitRows = g.CommitRows[:0]
	for i := range g.Rows {
		if !g.Rows[i].IsCommit {
			g.Rows[i].CommitIndex = -1
			continue
		}
		commitIndex, ok := hashToIndex[g.Rows[i].CommitHash]
		if !ok {
			g.Rows[i].CommitIndex = -1
			continue
		}
		g.Rows[i].CommitIndex = commitIndex
		g.CommitRows = append(g.CommitRows, i)
	}
}

func BuildGraph(commits []CommitInfo) *Graph {
	graph := &Graph{
		Rows:       make([]GraphRow, 0, len(commits)),
		CommitRows: make([]int, 0, len(commits)),
	}
	for i, commit := range commits {
		graph.Rows = append(graph.Rows, GraphRow{
			Prefix:      "* ",
			CommitHash:  commit.Hash,
			CommitIndex: i,
			IsCommit:    true,
		})
		graph.CommitRows = append(graph.CommitRows, i)
	}
	return graph
}

func RenderGraphLine(graph *Graph, commits []CommitInfo, rowIndex int, width int, style GraphStyle, highlight bool) string {
	return RenderGraphLineWithSuffix(graph, commits, rowIndex, width, style, highlight, "", 0)
}

func RenderGraphLineWithSuffix(graph *Graph, commits []CommitInfo, rowIndex int, width int, style GraphStyle, highlight bool, suffix string, suffixWidth int) string {
	return RenderGraphLineWithColumnWidths(graph, commits, rowIndex, width, style, highlight, suffix, suffixWidth, AuthorColumnWidth(commits), StatColumnWidth(commits))
}

func RenderGraphLineWithAuthorWidth(graph *Graph, commits []CommitInfo, rowIndex int, width int, style GraphStyle, highlight bool, suffix string, suffixWidth int, authorWidth int) string {
	return RenderGraphLineWithColumnWidths(graph, commits, rowIndex, width, style, highlight, suffix, suffixWidth, authorWidth, StatColumnWidth(commits))
}

func RenderGraphLineWithColumnWidths(graph *Graph, commits []CommitInfo, rowIndex int, width int, style GraphStyle, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int) string {
	return RenderGraphLineWithColumnWidthsAndTimezone(graph, commits, rowIndex, width, style, highlight, suffix, suffixWidth, authorWidth, statWidth, true)
}

func RenderGraphLineWithColumnWidthsAndTimezone(graph *Graph, commits []CommitInfo, rowIndex int, width int, style GraphStyle, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool) string {
	return RenderGraphLineWithColumnWidthsAndOptions(graph, commits, rowIndex, width, style, highlight, suffix, suffixWidth, authorWidth, statWidth, showTimezone, false, true)
}

func RenderGraphLineWithColumnWidthsAndOptions(graph *Graph, commits []CommitInfo, rowIndex int, width int, style GraphStyle, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	if graph == nil || rowIndex >= len(graph.Rows) || rowIndex < 0 {
		return ""
	}
	row := graph.Rows[rowIndex]
	prefixText := truncateForWidth(row.Prefix, width)
	prefix := RenderGraphPrefix(prefixText, style, highlight)
	if !row.IsCommit || row.CommitIndex < 0 || row.CommitIndex >= len(commits) {
		return prefix
	}

	commitWidth := width - len(prefixText)
	if commitWidth < 0 {
		commitWidth = 0
	}
	commitStr := renderCommitInfoWithSuffix(&commits[row.CommitIndex], commitWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, showTimezone, showEmail, showLineDiffs)
	return prefix + commitStr
}

func RenderGraphPrefix(prefix string, style GraphStyle, highlight bool) string {
	bg := lipgloss.Color("237")
	plainStyle := lipgloss.NewStyle()
	if highlight {
		plainStyle = plainStyle.Background(bg)
		style.CommitDot = style.CommitDot.Background(bg)
		style.MergeDot = style.MergeDot.Background(bg)
		style.VerticalLine = style.VerticalLine.Background(bg)
		style.BranchLine = style.BranchLine.Background(bg)
	}

	var b strings.Builder
	for _, ch := range prefix {
		switch ch {
		case '*':
			b.WriteString(style.CommitDot.Render("*"))
		case '|':
			b.WriteString(style.VerticalLine.Render("|"))
		case '/', '\\', '_':
			b.WriteString(style.BranchLine.Render(string(ch)))
		default:
			b.WriteString(plainStyle.Render(string(ch)))
		}
	}
	return b.String()
}

func renderCommitInfo(commit *CommitInfo, width int, highlight bool) string {
	return renderCommitInfoWithSuffix(commit, width, highlight, "", 0, len(commit.AuthorName), StatColumnWidth([]CommitInfo{*commit}), true, false, true)
}

func RenderCommitLineWithColumnWidths(commit CommitInfo, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int) string {
	return RenderCommitLineWithColumnWidthsAndTimezone(commit, width, highlight, suffix, suffixWidth, authorWidth, statWidth, true)
}

func RenderCommitLineWithColumnWidthsAndTimezone(commit CommitInfo, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool) string {
	return RenderCommitLineWithColumnWidthsAndOptions(commit, width, highlight, suffix, suffixWidth, authorWidth, statWidth, showTimezone, false, true)
}

func RenderCommitLineWithColumnWidthsAndOptions(commit CommitInfo, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	return renderCommitInfoWithSuffix(&commit, width, highlight, suffix, suffixWidth, authorWidth, statWidth, showTimezone, showEmail, showLineDiffs)
}

func renderCommitInfoWithSuffix(commit *CommitInfo, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	bg := lipgloss.Color("237")

	hashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	authorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sepStyle := lipgloss.NewStyle()

	if commit.IsUnpushed {
		authorStyle = authorStyle.Foreground(lipgloss.Color("13"))
		dateStyle = dateStyle.Foreground(lipgloss.Color("7"))
		msgStyle = msgStyle.Bold(true)
	}

	if highlight {
		hashStyle = hashStyle.Background(bg)
		authorStyle = authorStyle.Background(bg)
		dateStyle = dateStyle.Background(bg)
		addStyle = addStyle.Background(bg)
		delStyle = delStyle.Background(bg)
		msgStyle = msgStyle.Background(bg)
		sepStyle = sepStyle.Background(bg)
	}

	dateStr := formatCommitTime(commit.AuthorDate, showTimezone)
	message := commit.Message
	if idx := strings.Index(message, "\n"); idx != -1 {
		message = message[:idx]
	}

	authorStr := FormatCommitAuthor(FormatCommitAuthorIdentity(commit.AuthorName, commit.AuthorEmail, showEmail), authorWidth)
	addStr := FormatCommitStat("+", commit.Additions, statWidth)
	delStr := FormatCommitStat("-", commit.Deletions, statWidth)

	staticWidth := len(commit.ShortHash) + 2 + authorWidth + 2 + len(dateStr) + 2
	if showLineDiffs {
		staticWidth += statWidth + 1 + statWidth + 2
	}
	availableForMsg := width - staticWidth - suffixWidth
	message = truncateForWidth(message, availableForMsg)

	sep := sepStyle.Render("  ")
	statSep := sepStyle.Render(" ")
	addPart := addStyle.Render(addStr)
	delPart := delStyle.Render(delStr)
	line := hashStyle.Render(commit.ShortHash) + sep +
		authorStyle.Render(authorStr) + sep +
		dateStyle.Render(dateStr) + sep
	if showLineDiffs {
		line += addPart + statSep + delPart + sep
	}
	line += msgStyle.Render(message)

	if highlight {
		line += suffix
		lineLen := staticWidth + len(message) + suffixWidth
		if width > lineLen {
			line += sepStyle.Render(strings.Repeat(" ", width-lineLen))
		}
	} else if suffix != "" {
		line += suffix
	}

	return line
}

func formatCommitTime(t time.Time, showTimezone bool) string {
	if showTimezone {
		return t.Format("2006-01-02 15:04 -0700")
	}
	return t.In(time.Local).Format("2006-01-02 15:04")
}

func AuthorColumnWidth(commits []CommitInfo) int {
	return AuthorColumnWidthWithEmail(commits, false)
}

func AuthorColumnWidthWithEmail(commits []CommitInfo, showEmail bool) int {
	width := 0
	for _, commit := range commits {
		author := FormatCommitAuthorIdentity(commit.AuthorName, commit.AuthorEmail, showEmail)
		if len(author) > width {
			width = len(author)
		}
	}
	return width
}

func StatColumnWidth(commits []CommitInfo) int {
	width := 0
	for _, commit := range commits {
		width = max(width, len(formatCommitStat("+", commit.Additions)))
		width = max(width, len(formatCommitStat("-", commit.Deletions)))
	}
	return width
}

func FormatCommitAuthor(author string, width int) string {
	return padRight(truncateForWidth(author, width), width)
}

func FormatCommitAuthorIdentity(name string, email string, showEmail bool) string {
	if showEmail && email != "" {
		return name + " <" + email + ">"
	}
	return name
}

func FormatCommitStat(prefix string, value int, width int) string {
	return fmt.Sprintf("%*s", width, formatCommitStat(prefix, value))
}

func formatCommitStat(prefix string, value int) string {
	return fmt.Sprintf("%s%d", prefix, value)
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

func truncateForWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}
