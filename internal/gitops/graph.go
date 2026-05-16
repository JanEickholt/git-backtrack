package gitops

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/go-git/go-git/v5/plumbing"
)

type GraphNode struct {
	Commit   *CommitInfo
	Row      int
	Column   int
	IsMerge  bool
	IsBranch bool
}

type GraphLine struct {
	Commit        *CommitInfo
	Node          *GraphNode
	ConnectorRows []string
}

type Graph struct {
	Nodes    []*GraphNode
	Lines    []*GraphLine
	RowCount int
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

var zeroHash = plumbing.ZeroHash

func cloneLanes(lanes []plumbing.Hash) []plumbing.Hash {
	c := make([]plumbing.Hash, len(lanes))
	copy(c, lanes)
	return c
}

func findInLanes(lanes []plumbing.Hash, h plumbing.Hash) int {
	for i, lh := range lanes {
		if lh == h {
			return i
		}
	}
	return -1
}

func allocInLanes(lanes *[]plumbing.Hash, h plumbing.Hash) int {
	for i, lh := range *lanes {
		if lh == zeroHash {
			(*lanes)[i] = h
			return i
		}
	}
	*lanes = append(*lanes, h)
	return len(*lanes) - 1
}

func trimLanes(lanes *[]plumbing.Hash) {
	end := len(*lanes)
	for end > 0 && (*lanes)[end-1] == zeroHash {
		end--
	}
	*lanes = (*lanes)[:end]
}

func laneWidth(lanes []plumbing.Hash) int {
	w := len(lanes)
	for w > 0 && lanes[w-1] == zeroHash {
		w--
	}
	return w
}

func BuildGraph(commits []CommitInfo) *Graph {
	if len(commits) == 0 {
		return &Graph{}
	}

	lanes := make([]plumbing.Hash, 0, 8)
	nodes := make([]*GraphNode, len(commits))
	lines := make([]*GraphLine, len(commits))

	for i := range commits {
		c := &commits[i]

		col := findInLanes(lanes, c.Hash)
		if col == -1 {
			col = allocInLanes(&lanes, c.Hash)
		}

		lanesBefore := cloneLanes(lanes)

		nodes[i] = &GraphNode{
			Commit:   c,
			Row:      i,
			Column:   col,
			IsMerge:  len(c.Parents) > 1,
			IsBranch: len(c.Parents) == 0,
		}

		lanes[col] = zeroHash

		for pi, parentHash := range c.Parents {
			if pi == 0 {
				if findInLanes(lanes, parentHash) == -1 {
					lanes[col] = parentHash
				}
			} else {
				if findInLanes(lanes, parentHash) == -1 {
					allocInLanes(&lanes, parentHash)
				}
			}
		}

		trimLanes(&lanes)
		lanesAfter := cloneLanes(lanes)

		connRows := buildConnectorRows(lanesBefore, lanesAfter, col, len(c.Parents))

		lines[i] = &GraphLine{
			Commit:        c,
			Node:          nodes[i],
			ConnectorRows: connRows,
		}
	}

	return &Graph{
		Nodes:    nodes,
		Lines:    lines,
		RowCount: len(commits),
	}
}

// buildConnectorRows produces one or two connector rows between commits.
//
// Row layout (each cell is 2 chars wide: symbol + space):
//
//	lanesBefore: lane state when the commit dot was drawn
//	lanesAfter:  lane state after placing parents
//	commitCol:   column of the commit dot
//	parentCount: how many parents the commit has
//
// For a simple linear commit:
//
//	|   (straight down)
//
// For a branch-off (new lane opened to the right of commitCol):
//
//	| \
//	|  \   (diagonal going right/down)
//
// For a merge (lane coming in from the right collapsing into commitCol):
//
//	|\ |
//	| \|   (diagonal coming left/down then join)
func buildConnectorRows(before, after []plumbing.Hash, commitCol, parentCount int) []string {
	maxW := len(before)
	if len(after) > maxW {
		maxW = len(after)
	}
	if maxW == 0 {
		return nil
	}

	cellCount := maxW*2 - 1

	makeRow := func() []byte {
		row := make([]byte, cellCount)
		for i := range row {
			row[i] = ' '
		}
		return row
	}

	set := func(row []byte, col int, ch byte) {
		pos := col * 2
		if pos >= 0 && pos < len(row) {
			row[pos] = ch
		}
	}

	// Which lanes survive straight down (present in both before and after at the same col)?
	surviving := func(row []byte) {
		for col := 0; col < maxW; col++ {
			afterH := zeroHash
			if col < len(after) {
				afterH = after[col]
			}
			beforeH := zeroHash
			if col < len(before) {
				beforeH = before[col]
			}
			if afterH != zeroHash && beforeH != zeroHash && afterH == beforeH {
				set(row, col, '|')
			}
		}
	}

	var rows []string

	// ── Row 1: immediate below the commit dot ────────────────────────────────
	//
	// Rules (evaluated per "after" lane):
	//  - after[col] == before[col]  → straight '|'
	//  - after[col] != zeroHash and col > commitCol and before[col] == zeroHash
	//    → new lane opened (branch-off): draw '\' moving right
	//  - after[commitCol] != zeroHash and before[commitCol] != zeroHash
	//    → first parent continuing straight → '|'
	//  - a lane in before that has been consumed (merge): will need a '/'
	//    coming in from right toward commitCol

	r1 := makeRow()

	// Straight continuations
	surviving(r1)

	// First parent: continuing in commitCol
	if commitCol < len(after) && after[commitCol] != zeroHash {
		set(r1, commitCol, '|')
	}

	// New lanes opened to the right of commitCol (branch-offs, pi >= 1)
	for col := commitCol + 1; col < len(after); col++ {
		if after[col] == zeroHash {
			continue
		}
		// Was this lane already present before?
		beforeH := zeroHash
		if col < len(before) {
			beforeH = before[col]
		}
		if beforeH == zeroHash {
			// Newly opened: diagonal from commitCol going right
			set(r1, col, '\\')
			// Fill intermediate diagonals (only needed when col > commitCol+1)
			for mid := commitCol + 1; mid < col; mid++ {
				// only overwrite space
				pos := mid * 2
				if pos < len(r1) && r1[pos] == ' ' {
					r1[pos] = '\\'
				}
			}
		}
	}

	// Lanes in before that are now gone (merged into commitCol)
	// They were to the right of commitCol; draw '/' sweeping left
	for col := commitCol + 1; col < len(before); col++ {
		if before[col] == zeroHash {
			continue
		}
		afterH := zeroHash
		if col < len(after) {
			afterH = after[col]
		}
		// Lane disappeared: it was a merge parent consumed at this commit
		if afterH == zeroHash {
			set(r1, col, '/')
		}
	}

	rows = append(rows, string(r1))

	// ── Row 2: needed when diagonals must travel more than one row ───────────
	//
	// When a new lane opens far to the right, or a merge lane comes from far
	// right, we need extra rows so the diagonal actually connects.  For
	// simplicity we emit exactly one extra row when such a gap exists.

	needsRow2 := false
	for col := commitCol + 2; col < len(after); col++ {
		beforeH := zeroHash
		if col < len(before) {
			beforeH = before[col]
		}
		if after[col] != zeroHash && beforeH == zeroHash {
			needsRow2 = true
			break
		}
	}
	if !needsRow2 {
		for col := commitCol + 2; col < len(before); col++ {
			afterH := zeroHash
			if col < len(after) {
				afterH = after[col]
			}
			if before[col] != zeroHash && afterH == zeroHash {
				needsRow2 = true
				break
			}
		}
	}

	if needsRow2 {
		r2 := makeRow()
		surviving(r2)
		if commitCol < len(after) && after[commitCol] != zeroHash {
			set(r2, commitCol, '|')
		}
		for col := commitCol + 1; col < len(after); col++ {
			if after[col] == zeroHash {
				continue
			}
			beforeH := zeroHash
			if col < len(before) {
				beforeH = before[col]
			}
			if beforeH == zeroHash {
				set(r2, col, '\\')
				for mid := commitCol + 1; mid < col; mid++ {
					pos := mid * 2
					if pos < len(r2) && r2[pos] == ' ' {
						r2[pos] = '\\'
					}
				}
			}
		}
		for col := commitCol + 1; col < len(before); col++ {
			if before[col] == zeroHash {
				continue
			}
			afterH := zeroHash
			if col < len(after) {
				afterH = after[col]
			}
			if afterH == zeroHash {
				set(r2, col, '/')
			}
		}
		rows = append(rows, string(r2))
	}

	return rows
}

func RenderGraphLine(graph *Graph, lineIndex int, width int, style GraphStyle, highlight bool) string {
	return RenderGraphLineWithSuffix(graph, lineIndex, width, style, highlight, "", 0)
}

func RenderGraphLineWithSuffix(graph *Graph, lineIndex int, width int, style GraphStyle, highlight bool, suffix string, suffixWidth int) string {
	if graph == nil || lineIndex >= len(graph.Lines) || lineIndex < 0 {
		return ""
	}
	line := graph.Lines[lineIndex]
	if line == nil || line.Commit == nil {
		return ""
	}
	node := line.Node
	commit := line.Commit

	bg := lipgloss.Color("237")
	sepStyle := lipgloss.NewStyle()
	if highlight {
		sepStyle = sepStyle.Background(bg)
	}

	lanePrefix := renderLanePrefix(node.Column, highlight, style, bg)
	laneWidth := node.Column * 2

	var dot string
	if node.IsMerge {
		if highlight {
			dot = style.MergeDot.Background(bg).Render("◉")
		} else {
			dot = style.MergeDot.Render("◉")
		}
	} else {
		if highlight {
			dot = style.CommitDot.Background(bg).Render("●")
		} else {
			dot = style.CommitDot.Render("●")
		}
	}

	prefix := lanePrefix + dot + sepStyle.Render(" ")
	commitStr := renderCommitInfoWithSuffix(commit, width-laneWidth-2, highlight, suffix, suffixWidth)
	return prefix + commitStr
}

// RenderConnectorLines returns all connector rows for the given commit index.
// Call after RenderGraphLine; iterate and print each string on its own line.
func RenderConnectorLines(graph *Graph, lineIndex int, style GraphStyle) []string {
	if graph == nil || lineIndex >= len(graph.Lines) || lineIndex < 0 {
		return nil
	}
	line := graph.Lines[lineIndex]
	if line == nil {
		return nil
	}
	out := make([]string, 0, len(line.ConnectorRows))
	for _, row := range line.ConnectorRows {
		if !HasDiagonal(row) {
			continue
		}
		out = append(out, renderConnectorRow(row, style))
	}
	return out
}

func HasDiagonal(row string) bool {
	for _, ch := range row {
		if ch == '/' || ch == '\\' {
			return true
		}
	}
	return false
}

func renderConnectorRow(row string, style GraphStyle) string {
	var sb strings.Builder
	for _, ch := range row {
		switch ch {
		case '|':
			sb.WriteString(style.VerticalLine.Render("│"))
		case '/':
			sb.WriteString(style.BranchLine.Render("/"))
		case '\\':
			sb.WriteString(style.BranchLine.Render("\\"))
		default:
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

func renderLanePrefix(col int, highlight bool, style GraphStyle, bg lipgloss.Color) string {
	if col == 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i < col; i++ {
		if highlight {
			sb.WriteString(style.VerticalLine.Background(bg).Render("│"))
			sb.WriteString(lipgloss.NewStyle().Background(bg).Render(" "))
		} else {
			sb.WriteString(style.VerticalLine.Render("│"))
			sb.WriteRune(' ')
		}
	}
	return sb.String()
}

func renderCommitInfo(commit *CommitInfo, width int, highlight bool) string {
	return renderCommitInfoWithSuffix(commit, width, highlight, "", 0)
}

func renderCommitInfoWithSuffix(commit *CommitInfo, width int, highlight bool, suffix string, suffixWidth int) string {
	bg := lipgloss.Color("237")

	hashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	authorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sepStyle := lipgloss.NewStyle()

	if highlight {
		hashStyle = hashStyle.Background(bg)
		authorStyle = authorStyle.Background(bg)
		dateStyle = dateStyle.Background(bg)
		addStyle = addStyle.Background(bg)
		delStyle = delStyle.Background(bg)
		msgStyle = msgStyle.Background(bg)
		sepStyle = sepStyle.Background(bg)
	}

	dateStr := commit.AuthorDate.In(time.Local).Format("2006-01-02 15:04")
	message := commit.Message
	if idx := strings.Index(message, "\n"); idx != -1 {
		message = message[:idx]
	}

	statsStr := fmt.Sprintf("+%d -%d", commit.Additions, commit.Deletions)

	staticWidth := len(commit.ShortHash) + 2 + len(commit.AuthorName) + 2 + len(dateStr) + 2 + len(statsStr) + 2
	availableForMsg := width - staticWidth - suffixWidth
	if availableForMsg > 0 && len(message) > availableForMsg {
		message = message[:availableForMsg-3] + "..."
	}

	sep := sepStyle.Render("  ")
	addPart := addStyle.Render(fmt.Sprintf("+%d", commit.Additions))
	delPart := delStyle.Render(fmt.Sprintf("-%d", commit.Deletions))
	line := hashStyle.Render(commit.ShortHash) + sep +
		authorStyle.Render(commit.AuthorName) + sep +
		dateStyle.Render(dateStr) + sep +
		addPart + " " + delPart + sep +
		msgStyle.Render(message)

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
