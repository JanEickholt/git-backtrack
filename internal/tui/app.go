package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Jan/git-backtrack/internal/gitops"
)

type ViewState int

const (
	ViewList ViewState = iota
	ViewEdit
	ViewBatchEdit
	ViewConfirm
	ViewResult
)

type EditField int

const (
	FieldName EditField = iota
	Email
	Date
	Time
	Message
)

type Model struct {
	state           ViewState
	repo            *gitops.Repository
	commits         []gitops.CommitInfo
	graph           *gitops.Graph
	editQueue       []gitops.ForgeChange
	editMap         map[string]*gitops.ForgeChange
	selectedCommits map[string]bool
	scrollOffset    int
	visualLineCache []int // visualLineCache[i] = total visual lines for commits 0..i-1

	editingCommit *gitops.CommitInfo
	editFields    []textinput.Model
	focusField    EditField

	batchFields []textinput.Model
	batchFocus  int

	result *gitops.RewriteResult
	err    error
	list   list.Model
	help   help.Model
	width  int
	height int
	keys   keyMap
}

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Edit      key.Binding
	Reset     key.Binding
	Apply     key.Binding
	Quit      key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
	Tab       key.Binding
	ShiftTab  key.Binding
	Select    key.Binding
	BatchEdit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Edit, k.Select, k.BatchEdit, k.Apply, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Edit, k.Select},
		{k.BatchEdit, k.Reset},
		{k.Apply, k.Quit},
	}
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Reset:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reset")),
		Apply:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "apply")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Confirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		ShiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev")),
		Select:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		BatchEdit: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "batch edit")),
	}
}

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Background(lipgloss.Color("0")).Padding(0, 1)
	statusStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	editStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	selectedBgStyle = lipgloss.NewStyle().Background(lipgloss.Color("20"))
	errorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	successStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	labelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
)

func countVisualLines(graph *gitops.Graph, commitIndex int) int {
	if graph == nil || commitIndex >= len(graph.Lines) {
		return 1
	}
	line := graph.Lines[commitIndex]
	connectorCount := 0
	for _, row := range line.ConnectorRows {
		if gitops.HasDiagonal(row) {
			connectorCount++
		}
	}
	return 1 + connectorCount
}

func buildVisualLineCache(graph *gitops.Graph, commits []gitops.CommitInfo) []int {
	cache := make([]int, len(commits)+1)
	for i := range commits {
		cache[i+1] = cache[i] + countVisualLines(graph, i)
	}
	return cache
}

func findCommitForVisualLine(cache []int, visualLine int) int {
	if cache == nil || len(cache) == 0 {
		return 0
	}
	for i := 1; i < len(cache); i++ {
		if cache[i] > visualLine {
			return i - 1
		}
	}
	return len(cache) - 2
}

func NewModel(repo *gitops.Repository) Model {
	commits, err := repo.ListAllCommits()
	if err != nil {
		return Model{
			err:             err,
			commits:         []gitops.CommitInfo{},
			graph:           &gitops.Graph{},
			editQueue:       make([]gitops.ForgeChange, 0),
			editMap:         make(map[string]*gitops.ForgeChange),
			selectedCommits: make(map[string]bool),
			help:            help.New(),
			keys:            defaultKeyMap(),
		}
	}

	graph := gitops.BuildGraph(commits)
	visualLineCache := buildVisualLineCache(graph, commits)
	items := make([]list.Item, len(commits))
	for i, commit := range commits {
		items[i] = commitItem{commit: commit}
	}

	delegate := commitDelegate{}
	l := list.New(items, delegate, 80, 20)
	l.Title = "git-backtrack"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle

	return Model{
		state:           ViewList,
		repo:            repo,
		commits:         commits,
		graph:           graph,
		visualLineCache: visualLineCache,
		editQueue:       make([]gitops.ForgeChange, 0),
		editMap:         make(map[string]*gitops.ForgeChange),
		selectedCommits: make(map[string]bool),
		help:            help.New(),
		keys:            defaultKeyMap(),
		list:            l,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, m.height-2)
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case ViewList:
			return m.handleListKey(msg)
		case ViewEdit:
			return m.handleEditKey(msg)
		case ViewBatchEdit:
			return m.handleBatchEditKey(msg)
		case ViewConfirm:
			return m.handleConfirmKey(msg)
		case ViewResult:
			return m.handleResultKey(msg)
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Edit):
		if len(m.commits) == 0 {
			return m, nil
		}
		idx := m.list.Index()
		if idx >= 0 && idx < len(m.commits) {
			m.editingCommit = &m.commits[idx]
			m.initEditFields()
			m.state = ViewEdit
		}
		return m, nil

	case key.Matches(msg, m.keys.Select):
		if len(m.commits) == 0 {
			return m, nil
		}
		idx := m.list.Index()
		if idx >= 0 && idx < len(m.commits) {
			hash := m.commits[idx].Hash.String()
			m.selectedCommits[hash] = !m.selectedCommits[hash]
		}
		return m, nil

	case key.Matches(msg, m.keys.BatchEdit):
		selectedCount := 0
		for _, v := range m.selectedCommits {
			if v {
				selectedCount++
			}
		}
		if selectedCount == 0 {
			return m, nil
		}
		m.initBatchFields()
		m.state = ViewBatchEdit
		return m, nil

	case key.Matches(msg, m.keys.Reset):
		if len(m.commits) == 0 {
			return m, nil
		}
		idx := m.list.Index()
		if idx >= 0 && idx < len(m.commits) {
			hash := m.commits[idx].Hash.String()
			if m.editMap[hash] != nil {
				delete(m.editMap, hash)
				newQueue := make([]gitops.ForgeChange, 0, len(m.editQueue)-1)
				for _, c := range m.editQueue {
					if c.OriginalHash.String() != hash {
						newQueue = append(newQueue, c)
					}
				}
				m.editQueue = newQueue
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Apply):
		if len(m.editQueue) == 0 {
			return m, nil
		}
		m.state = ViewConfirm
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)

	maxRows := m.height - 2
	if maxRows <= 0 {
		maxRows = 20
	}
	selectedIndex := m.list.Index()

	if m.visualLineCache == nil || len(m.visualLineCache) <= selectedIndex {
		return m, cmd
	}

	selectedVisualLine := m.visualLineCache[selectedIndex]

	if selectedVisualLine < m.scrollOffset {
		distance := m.scrollOffset - selectedVisualLine
		if distance <= 3 {
			m.scrollOffset--
		} else {
			m.scrollOffset = selectedVisualLine
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	} else if selectedVisualLine >= m.scrollOffset+maxRows {
		distance := selectedVisualLine - (m.scrollOffset + maxRows - 1)
		if distance <= 3 {
			m.scrollOffset++
		} else {
			m.scrollOffset = selectedVisualLine
		}
		totalLines := m.visualLineCache[len(m.visualLineCache)-1]
		if m.scrollOffset+maxRows > totalLines {
			m.scrollOffset = totalLines - maxRows
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		}
	}

	return m, cmd
}

func (m *Model) initEditFields() {
	m.editFields = make([]textinput.Model, 5)

	commit := m.editingCommit
	existingChange := m.editMap[commit.Hash.String()]

	name := commit.AuthorName
	email := commit.AuthorEmail
	date := commit.AuthorDate.In(time.Local).Format("2006-01-02")
	timeStr := commit.AuthorDate.In(time.Local).Format("15:04:05")
	msg := commit.Message
	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}

	if existingChange != nil {
		if existingChange.NewAuthor != nil {
			name = existingChange.NewAuthor.Name
			email = existingChange.NewAuthor.Email
		}
		if existingChange.NewDate != nil {
			date = existingChange.NewDate.In(time.Local).Format("2006-01-02")
			timeStr = existingChange.NewDate.In(time.Local).Format("15:04:05")
		}
		if existingChange.NewMessage != "" {
			msg = existingChange.NewMessage
		}
	}

	m.editFields[FieldName] = textinput.New()
	m.editFields[FieldName].Placeholder = "Author name"
	m.editFields[FieldName].SetValue(name)
	m.editFields[FieldName].Width = 40
	m.editFields[FieldName].Focus()

	m.editFields[Email] = textinput.New()
	m.editFields[Email].Placeholder = "email@example.com"
	m.editFields[Email].SetValue(email)
	m.editFields[Email].Width = 40

	m.editFields[Date] = textinput.New()
	m.editFields[Date].Placeholder = "YYYY-MM-DD"
	m.editFields[Date].SetValue(date)
	m.editFields[Date].Width = 40

	m.editFields[Time] = textinput.New()
	m.editFields[Time].Placeholder = "HH:MM:SS"
	m.editFields[Time].SetValue(timeStr)
	m.editFields[Time].Width = 40

	m.editFields[Message] = textinput.New()
	m.editFields[Message].Placeholder = "Commit message"
	m.editFields[Message].SetValue(msg)
	m.editFields[Message].Width = 60

	m.focusField = FieldName
}

func (m *Model) initBatchFields() {
	m.batchFields = make([]textinput.Model, 5)

	m.batchFields[0] = textinput.New()
	m.batchFields[0].Placeholder = "Author name (empty = keep original)"
	m.batchFields[0].SetValue("")
	m.batchFields[0].Width = 40
	m.batchFields[0].Focus()

	m.batchFields[1] = textinput.New()
	m.batchFields[1].Placeholder = "Author email (empty = keep original)"
	m.batchFields[1].SetValue("")
	m.batchFields[1].Width = 40

	m.batchFields[2] = textinput.New()
	m.batchFields[2].Placeholder = "Time adjust: -2h, +1d, -30m (empty = keep)"
	m.batchFields[2].SetValue("")
	m.batchFields[2].Width = 40

	m.batchFields[3] = textinput.New()
	m.batchFields[3].Placeholder = "Message (empty = keep original)"
	m.batchFields[3].SetValue("")
	m.batchFields[3].Width = 60

	m.batchFields[4] = textinput.New()
	m.batchFields[4].Placeholder = "Time spread: +1h, -30m (weighted distribution)"
	m.batchFields[4].SetValue("")
	m.batchFields[4].Width = 40

	m.batchFocus = 0
}

func (m Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.state = ViewList
		m.editingCommit = nil
		return m, nil

	case key.Matches(msg, m.keys.Confirm):
		change := m.buildForgeChange()
		hashStr := change.OriginalHash.String()

		if change.HasChanges() {
			if m.editMap == nil {
				m.editMap = make(map[string]*gitops.ForgeChange)
			}
			if m.editMap[hashStr] != nil {
				for i, c := range m.editQueue {
					if c.OriginalHash.String() == hashStr {
						changeCopy := change
						m.editQueue[i] = changeCopy
						m.editMap[hashStr] = &m.editQueue[i]
						break
					}
				}
			} else {
				changeCopy := change
				m.editQueue = append(m.editQueue, changeCopy)
				m.editMap[hashStr] = &m.editQueue[len(m.editQueue)-1]
			}
		} else if m.editMap != nil && m.editMap[hashStr] != nil {
			delete(m.editMap, hashStr)
			newQueue := make([]gitops.ForgeChange, 0, len(m.editQueue))
			for _, c := range m.editQueue {
				if c.OriginalHash.String() != hashStr {
					newQueue = append(newQueue, c)
				}
			}
			m.editQueue = newQueue
			// Rebuild editMap pointers since slice backing array may have changed
			for i := range m.editQueue {
				m.editMap[m.editQueue[i].OriginalHash.String()] = &m.editQueue[i]
			}
		}
		m.state = ViewList
		m.editingCommit = nil
		return m, nil

	case key.Matches(msg, m.keys.Tab):
		m.editFields[m.focusField].Blur()
		m.focusField = (m.focusField + 1) % 5
		m.editFields[m.focusField].Focus()
		return m, nil

	case key.Matches(msg, m.keys.ShiftTab):
		m.editFields[m.focusField].Blur()
		m.focusField = (m.focusField + 4) % 5
		m.editFields[m.focusField].Focus()
		return m, nil
	}

	m.editFields[m.focusField], cmd = m.editFields[m.focusField].Update(msg)
	return m, cmd
}

func (m Model) handleBatchEditKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Cancel):
			m.state = ViewList
			return m, nil

		case key.Matches(msg, m.keys.Confirm):
			return m.applyBatchChanges()

		case key.Matches(msg, m.keys.Tab):
			m.batchFields[m.batchFocus].Blur()
			m.batchFocus = (m.batchFocus + 1) % 5
			m.batchFields[m.batchFocus].Focus()
			return m, nil

		case key.Matches(msg, m.keys.ShiftTab):
			m.batchFields[m.batchFocus].Blur()
			m.batchFocus = (m.batchFocus + 4) % 5
			m.batchFields[m.batchFocus].Focus()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.batchFields[m.batchFocus], cmd = m.batchFields[m.batchFocus].Update(msg)
	return m, cmd
}

func (m Model) applyBatchChanges() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.batchFields[0].Value())
	email := strings.TrimSpace(m.batchFields[1].Value())
	timeAdjust := strings.TrimSpace(m.batchFields[2].Value())
	newMessage := strings.TrimSpace(m.batchFields[3].Value())
	timeSpread := strings.TrimSpace(m.batchFields[4].Value())

	if m.editMap == nil {
		m.editMap = make(map[string]*gitops.ForgeChange)
	}

	// Parse time spread duration if provided
	var timeSpreadDuration time.Duration
	var hasTimeSpread bool
	if timeSpread != "" {
		if d, ok := parseDuration(timeSpread); ok {
			timeSpreadDuration = d
			hasTimeSpread = true
		}
	}

	for hashStr, selected := range m.selectedCommits {
		if !selected {
			continue
		}

		var commit *gitops.CommitInfo
		for i := range m.commits {
			if m.commits[i].Hash.String() == hashStr {
				commit = &m.commits[i]
				break
			}
		}
		if commit == nil {
			continue
		}

		// Start with existing change if any, otherwise create new
		var change gitops.ForgeChange
		existingChange := m.editMap[hashStr]
		if existingChange != nil {
			change = *existingChange // Copy existing changes
		} else {
			change = gitops.ForgeChange{
				OriginalHash: commit.Hash,
			}
		}

		// Merge author changes: preserve existing values if new ones are empty
		if name != "" || email != "" {
			newAuthor := &gitops.AuthorInfo{
				Name:  name,
				Email: email,
			}
			// Preserve existing modified values if batch input is empty
			if existingChange != nil && existingChange.NewAuthor != nil {
				if name == "" {
					newAuthor.Name = existingChange.NewAuthor.Name
				}
				if email == "" {
					newAuthor.Email = existingChange.NewAuthor.Email
				}
			} else {
				// Use original commit values if batch input is empty
				if name == "" {
					newAuthor.Name = commit.AuthorName
				}
				if email == "" {
					newAuthor.Email = commit.AuthorEmail
				}
			}
			change.NewAuthor = newAuthor
		}

		// Merge time adjustment: accumulate on top of existing modified date
		if timeAdjust != "" {
			// Start from existing modified date if present, otherwise from original
			baseDate := commit.AuthorDate
			if existingChange != nil && existingChange.NewDate != nil {
				baseDate = *existingChange.NewDate
			}
			adjustedDate, err := adjustTime(baseDate, timeAdjust)
			if err == nil {
				change.NewDate = &adjustedDate
			}
		}

		// Merge message: use provided message, otherwise preserve existing
		if newMessage != "" {
			change.NewMessage = newMessage
		} else if existingChange != nil && existingChange.NewMessage != "" {
			// Preserve existing message change
			change.NewMessage = existingChange.NewMessage
		}

		if change.HasChanges() {
			if m.editMap[hashStr] != nil {
				for i, c := range m.editQueue {
					if c.OriginalHash.String() == hashStr {
						changeCopy := change
						m.editQueue[i] = changeCopy
						break
					}
				}
			} else {
				changeCopy := change
				m.editQueue = append(m.editQueue, changeCopy)
			}
		} else if m.editMap != nil && m.editMap[hashStr] != nil {
			newQueue := make([]gitops.ForgeChange, 0, len(m.editQueue))
			for _, c := range m.editQueue {
				if c.OriginalHash.String() != hashStr {
					newQueue = append(newQueue, c)
				}
			}
			m.editQueue = newQueue
		}
	}

	// Rebuild editMap pointers since slice modifications may have invalidated them
	m.editMap = make(map[string]*gitops.ForgeChange)
	for i := range m.editQueue {
		m.editMap[m.editQueue[i].OriginalHash.String()] = &m.editQueue[i]
	}

	if hasTimeSpread {
		timeSpreadMap := calculateTimeSpread(m.commits, m.selectedCommits, timeSpreadDuration, m.editMap)
		for hashStr, spreadDuration := range timeSpreadMap {
			if spreadDuration == 0 {
				continue
			}
			existingChange := m.editMap[hashStr]
			var change gitops.ForgeChange
			if existingChange != nil {
				change = *existingChange
			} else {
				for i := range m.commits {
					if m.commits[i].Hash.String() == hashStr {
						change = gitops.ForgeChange{
							OriginalHash: m.commits[i].Hash,
						}
						break
					}
				}
			}

			baseDate := change.NewDate
			if baseDate == nil {
				for i := range m.commits {
					if m.commits[i].Hash.String() == hashStr {
						baseDate = &m.commits[i].AuthorDate
						break
					}
				}
			}
			if baseDate != nil {
				newDate := baseDate.Add(spreadDuration)
				change.NewDate = &newDate
			}

			if existingChange != nil {
				for i, c := range m.editQueue {
					if c.OriginalHash.String() == hashStr {
						m.editQueue[i] = change
						break
					}
				}
			} else {
				m.editQueue = append(m.editQueue, change)
			}
		}

		m.editMap = make(map[string]*gitops.ForgeChange)
		for i := range m.editQueue {
			m.editMap[m.editQueue[i].OriginalHash.String()] = &m.editQueue[i]
		}
	}

	m.selectedCommits = make(map[string]bool)
	m.state = ViewList
	return m, nil
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

// parseDuration parses a duration string like "+1h", "-30m", "+1d" into a time.Duration.
// Returns the duration and a bool indicating if parsing was successful.
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

// calculateTimeSpread distributes timeToAdd proportionally across commits
// based on time gaps between consecutive commits.
// Returns a map of commit hash -> time to add for each commit (first commit gets 0).
func calculateTimeSpread(
	commits []gitops.CommitInfo,
	selectedHashes map[string]bool,
	timeToAdd time.Duration,
	editMap map[string]*gitops.ForgeChange,
) map[string]time.Duration {
	result := make(map[string]time.Duration)

	// Get selected commits and sort by AuthorDate (newest first)
	var selectedCommits []gitops.CommitInfo
	for _, commit := range commits {
		if selectedHashes[commit.Hash.String()] {
			selectedCommits = append(selectedCommits, commit)
		}
	}

	// Need at least 2 commits to spread time
	if len(selectedCommits) < 2 {
		return result
	}

	// Get effective date for a commit (modified date from editMap or original)
	getEffectiveDate := func(commit gitops.CommitInfo) time.Time {
		hashStr := commit.Hash.String()
		if editMap != nil && editMap[hashStr] != nil && editMap[hashStr].NewDate != nil {
			return *editMap[hashStr].NewDate
		}
		return commit.AuthorDate
	}

	// Calculate gaps between consecutive commits (newest to oldest)
	// gaps[i] = time gap between selectedCommits[i] and selectedCommits[i+1]
	gaps := make([]time.Duration, len(selectedCommits)-1)
	var totalSpan time.Duration

	for i := 0; i < len(gaps); i++ {
		currentDate := getEffectiveDate(selectedCommits[i])
		nextDate := getEffectiveDate(selectedCommits[i+1])
		gap := currentDate.Sub(nextDate)
		if gap < 0 {
			gap = -gap // Ensure positive gap
		}
		gaps[i] = gap
		totalSpan += gap
	}

	// If total span is 0, distribute equally (excluding oldest)
	if totalSpan == 0 {
		equalShare := timeToAdd / time.Duration(len(selectedCommits)-1)
		for i := 0; i < len(selectedCommits)-1; i++ {
			result[selectedCommits[i].Hash.String()] = equalShare
		}
		result[selectedCommits[len(selectedCommits)-1].Hash.String()] = 0
		return result
	}

	// Distribute time proportionally based on distance from oldest
	// Distance from oldest for commit i = sum of gaps[i] + gaps[i+1] + ... + gaps[len-1]
	// Oldest commit (len-1) has distance 0, newest commit (0) has distance = totalSpan
	for i := 0; i < len(selectedCommits); i++ {
		distanceFromOldest := time.Duration(0)
		for j := i; j < len(gaps); j++ {
			distanceFromOldest += gaps[j]
		}
		proportion := float64(distanceFromOldest) / float64(totalSpan)
		addedTime := time.Duration(float64(timeToAdd) * proportion)
		result[selectedCommits[i].Hash.String()] = addedTime
	}

	return result
}

func (m Model) buildForgeChange() gitops.ForgeChange {
	change := gitops.ForgeChange{
		OriginalHash: m.editingCommit.Hash,
	}

	if m.editFields[FieldName].Value() != m.editingCommit.AuthorName ||
		m.editFields[Email].Value() != m.editingCommit.AuthorEmail {
		change.NewAuthor = &gitops.AuthorInfo{
			Name:  m.editFields[FieldName].Value(),
			Email: m.editFields[Email].Value(),
		}
	}

	dateStr := m.editFields[Date].Value()
	timeStr := m.editFields[Time].Value()
	newDateTime, err := parseDateTime(dateStr, timeStr, time.Local)
	if err == nil && !newDateTime.Equal(m.editingCommit.AuthorDate) {
		change.NewDate = &newDateTime
	}

	if m.editFields[Message].Value() != strings.Split(m.editingCommit.Message, "\n")[0] {
		change.NewMessage = m.editFields[Message].Value()
	}

	return change
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

func (m Model) handleConfirmKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Cancel):
			m.state = ViewList
			return m, nil

		case key.Matches(msg, m.keys.Confirm):
			rewriter := gitops.NewHistoryRewriter(m.repo)

			backupRef, err := rewriter.CreateFullBackup()
			if err != nil {
				m.err = fmt.Errorf("failed to create backup: %w", err)
				m.state = ViewResult
				return m, nil
			}

			result, err := rewriter.ApplyChanges(m.editQueue)
			if err != nil {
				m.err = err
			} else {
				m.result = result
				m.result.BackupRef = backupRef
				m.repo.Reload()
				commits, _ := m.repo.ListAllCommits()
				m.commits = commits
				m.graph = gitops.BuildGraph(commits)
				m.editQueue = make([]gitops.ForgeChange, 0)
				m.editMap = make(map[string]*gitops.ForgeChange)
				items := make([]list.Item, len(commits))
				for i, commit := range commits {
					items[i] = commitItem{commit: commit}
				}
				m.list.SetItems(items)
			}
			m.state = ViewResult
			return m, nil
		}
	}
	return m, nil
}

func (m Model) handleResultKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Confirm):
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) refresh() {
	commits, err := m.repo.ListAllCommits()
	if err != nil {
		return
	}
	m.commits = commits
	m.graph = gitops.BuildGraph(commits)
	m.visualLineCache = buildVisualLineCache(m.graph, commits)
	m.editQueue = make([]gitops.ForgeChange, 0)
	m.editMap = make(map[string]*gitops.ForgeChange)
	items := make([]list.Item, len(commits))
	for i, commit := range commits {
		items[i] = commitItem{commit: commit}
	}
	m.list.SetItems(items)
}

func (m Model) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	switch m.state {
	case ViewList:
		return m.renderListView()
	case ViewEdit:
		return m.renderEditView()
	case ViewBatchEdit:
		return m.renderBatchEditView()
	case ViewConfirm:
		return m.renderConfirmView()
	case ViewResult:
		return m.renderResultView()
	default:
		return ""
	}
}

func (m Model) renderListView() string {
	var b strings.Builder
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}
	if len(m.commits) == 0 {
		return statusStyle.Render("No commits found in repository")
	}
	if m.visualLineCache == nil || len(m.visualLineCache) == 0 {
		return statusStyle.Render("Loading...")
	}
	b.WriteString(titleStyle.Render("git-backtrack"))
	b.WriteString("\n")
	maxRows := m.height - 2
	if maxRows <= 0 {
		maxRows = 20
	}

	bg := lipgloss.Color("237")
	bgStyle := lipgloss.NewStyle().Background(bg)
	style := gitops.DefaultGraphStyle()

	startCommit := findCommitForVisualLine(m.visualLineCache, m.scrollOffset)
	if startCommit >= len(m.commits) {
		startCommit = len(m.commits) - 1
	}
	if startCommit < 0 {
		startCommit = 0
	}

	visualLinesRendered := 0
	commitIndex := startCommit
	startVisualLine := m.visualLineCache[startCommit]
	linesToSkip := m.scrollOffset - startVisualLine
	skipCommitLine := linesToSkip > 0
	if skipCommitLine {
		linesToSkip--
	}

	for commitIndex < len(m.commits) && visualLinesRendered < maxRows {
		commit := m.commits[commitIndex]
		highlight := commitIndex == m.list.Index()
		connectorLines := gitops.RenderConnectorLines(m.graph, commitIndex, style)

		if !skipCommitLine {
			hasWarning := hasTimeAnomaly(commit, m.commits, m.editMap)
			warning := ""
			warningWidth := 0
			if hasWarning {
				if highlight {
					warning = errorStyle.Background(bg).Render(" ⚠️time")
				} else {
					warning = errorStyle.Render(" ⚠️time")
				}
				warningWidth = 7
			}

			lineWidth := m.width - 6
			line := gitops.RenderGraphLineWithSuffix(m.graph, commitIndex, lineWidth, style, highlight, warning, warningWidth)
			change := m.editMap[commit.Hash.String()]
			if change != nil {
				line = renderModifiedCommit(commit, change, lineWidth, highlight, warning, warningWidth)
			}

			selected := m.selectedCommits[commit.Hash.String()]
			var selMarker string
			if selected {
				if highlight {
					selMarker = editStyle.Background(bg).Render("[x] ")
				} else {
					selMarker = editStyle.Render("[x] ")
				}
			} else {
				if highlight {
					selMarker = bgStyle.Render("    ")
				} else {
					selMarker = "    "
				}
			}

			var fullLine string
			if highlight {
				fullLine = bgStyle.Render("> ") + selMarker + line
			} else {
				fullLine = "  " + selMarker + line
			}

			b.WriteString(fullLine)
			b.WriteString("\n")
			visualLinesRendered++
		}
		skipCommitLine = false

		connectorStartIdx := 0
		if linesToSkip > 0 {
			connectorStartIdx = linesToSkip
			linesToSkip = 0
		}

		for i := connectorStartIdx; i < len(connectorLines) && visualLinesRendered < maxRows; i++ {
			connRow := connectorLines[i]
			if connRow == "" {
				continue
			}
			b.WriteString("      ")
			b.WriteString(connRow)
			b.WriteString("\n")
			visualLinesRendered++
		}

		commitIndex++
	}

	selectedCount := 0
	for _, v := range m.selectedCommits {
		if v {
			selectedCount++
		}
	}
	var statusText string
	if selectedCount > 0 {
		statusText = fmt.Sprintf("%d commits | %d selected | %d pending | space:select b:batch a:apply q:quit", len(m.commits), selectedCount, len(m.editQueue))
	} else {
		statusText = fmt.Sprintf("%d commits | %d pending | e:edit space:select b:batch a:apply q:quit", len(m.commits), len(m.editQueue))
	}
	b.WriteString(statusStyle.Render(statusText))
	return b.String()
}

func (m Model) renderBatchEditView() string {
	var b strings.Builder
	selectedCount := 0
	for _, v := range m.selectedCommits {
		if v {
			selectedCount++
		}
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("Batch Edit: %d commits", selectedCount)))
	b.WriteString("\n\n")

	labels := []string{
		"Author Name",
		"Author Email",
		"Time Adjust",
		"Message",
		"Time Spread",
	}
	placeholders := []string{
		"(empty = keep original)",
		"(empty = keep original)",
		"e.g., -2h, +1d, -30m",
		"(empty = keep original)",
		"e.g., +1h, -30m (weighted)",
	}

	for i, input := range m.batchFields {
		prefix := "  "
		if i == m.batchFocus {
			prefix = "> "
		}
		b.WriteString(prefix + labelStyle.Render(labels[i]+": "))
		b.WriteString(input.View())
		b.WriteString(" " + statusStyle.Render(placeholders[i]))
		b.WriteString("\n\n")
	}

	b.WriteString("\n")
	b.WriteString(labelStyle.Render("[Tab]") + " next  ")
	b.WriteString(labelStyle.Render("[Enter]") + " apply  ")
	b.WriteString(labelStyle.Render("[Esc]") + " cancel")

	return b.String()
}

func (m Model) renderEditView() string {
	if m.editingCommit == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render(fmt.Sprintf("Edit Commit: %s", m.editingCommit.ShortHash)))
	b.WriteString("\n\n")

	labels := []string{"Author Name", "Author Email", "Date (YYYY-MM-DD)", "Time (HH:MM:SS)", "Message"}
	for i, input := range m.editFields {
		prefix := "  "
		if i == int(m.focusField) {
			prefix = "> "
		}
		b.WriteString(prefix + labelStyle.Render(labels[i]+": "))
		if i == int(m.focusField) {
			b.WriteString(editStyle.Render(input.View()))
		} else {
			b.WriteString(statusStyle.Render(input.View()))
		}
		b.WriteString("\n\n")
	}

	b.WriteString(statusStyle.Render("Original: "))
	b.WriteString(fmt.Sprintf("%s <%s> %s",
		m.editingCommit.AuthorName,
		m.editingCommit.AuthorEmail,
		m.editingCommit.AuthorDate.Format("2006-01-02 15:04:05")))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("[Tab]") + " next  ")
	b.WriteString(labelStyle.Render("[Enter]") + " save  ")
	b.WriteString(labelStyle.Render("[Esc]") + " cancel")

	return b.String()
}

func (m Model) renderConfirmView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Confirm Changes"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("About to rewrite %d commits:\n\n", len(m.editQueue)))

	for i, change := range m.editQueue {
		b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, change.OriginalHash.String()[:7]))
		if change.NewAuthor != nil {
			b.WriteString(fmt.Sprintf("     Author: %s <%s>\n", change.NewAuthor.Name, change.NewAuthor.Email))
		}
		if change.NewDate != nil {
			b.WriteString(fmt.Sprintf("     Date: %s\n", change.NewDate.Format("2006-01-02 15:04:05")))
		}
		if change.NewMessage != "" {
			b.WriteString(fmt.Sprintf("     Message: %s\n", strings.Split(change.NewMessage, "\n")[0]))
		}
		b.WriteString("\n")
	}

	b.WriteString(errorStyle.Render("This will rewrite git history!"))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("[Enter]") + " confirm  ")
	b.WriteString(labelStyle.Render("[Esc]") + " cancel")

	return b.String()
}

func (m Model) renderResultView() string {
	var b strings.Builder

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
		b.WriteString(statusStyle.Render("Press any key to exit"))
		return b.String()
	}

	b.WriteString(successStyle.Render("Changes applied successfully!"))
	b.WriteString("\n\n")

	if m.result != nil {
		b.WriteString(fmt.Sprintf("Rewrote %d commits\n", len(m.result.ChangedRefs)))
		if m.result.BackupRef != "" {
			b.WriteString(fmt.Sprintf("Backup: %s\n", m.result.BackupRef))
		}
	}

	b.WriteString("\n")
	b.WriteString(statusStyle.Render("Press any key to exit"))

	return b.String()
}

func renderModifiedCommit(original gitops.CommitInfo, change *gitops.ForgeChange, width int, highlight bool, suffix string, suffixWidth int) string {
	name := original.AuthorName
	date := original.AuthorDate.In(time.Local).Format("2006-01-02 15:04")
	msg := strings.Split(original.Message, "\n")[0]

	if change.NewAuthor != nil {
		name = change.NewAuthor.Name
		if change.NewAuthor.Email != "" {
			name = name + " <" + change.NewAuthor.Email + ">"
		}
	}
	if change.NewDate != nil {
		date = change.NewDate.In(time.Local).Format("2006-01-02 15:04")
	}
	if change.NewMessage != "" {
		msg = strings.Split(change.NewMessage, "\n")[0]
	}

	bg := lipgloss.Color("237")
	hashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sepStyle := lipgloss.NewStyle()

	if highlight {
		hashStyle = hashStyle.Background(bg)
		nameStyle = nameStyle.Background(bg)
		dateStyle = dateStyle.Background(bg)
		msgStyle = msgStyle.Background(bg)
		sepStyle = sepStyle.Background(bg)
	}

	hashPart := hashStyle.Render(original.ShortHash)
	namePart := nameStyle.Render(name)
	datePart := dateStyle.Render(date)

	staticWidth := len(original.ShortHash) + 2 + len(name) + 2 + len(date) + 2
	availableForMsg := width - staticWidth - suffixWidth
	if availableForMsg > 0 && len(msg) > availableForMsg {
		msg = msg[:availableForMsg-3] + "..."
	}

	sep := sepStyle.Render("  ")
	line := hashPart + sep + namePart + sep + datePart + sep + msgStyle.Render(msg)

	if highlight {
		line += suffix
		lineLen := staticWidth + len(msg) + suffixWidth
		if width > lineLen {
			line += sepStyle.Render(strings.Repeat(" ", width-lineLen))
		}
	} else if suffix != "" {
		line += suffix
	}

	return line
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

type commitItem struct {
	commit gitops.CommitInfo
}

func (i commitItem) FilterValue() string {
	return i.commit.Message
}

type commitDelegate struct{}

func (d commitDelegate) Height() int                             { return 1 }
func (d commitDelegate) Spacing() int                            { return 0 }
func (d commitDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d commitDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(commitItem)
	if !ok {
		return
	}

	line := fmt.Sprintf("%s  %s  %s  %s",
		i.commit.ShortHash,
		i.commit.AuthorName,
		i.commit.AuthorDate.In(time.Local).Format("2006-01-02 15:04"),
		strings.Split(i.commit.Message, "\n")[0],
	)

	if index%2 == 0 {
		line = lipgloss.NewStyle().Background(lipgloss.Color("235")).Render(line)
	}

	fmt.Fprintln(w, line)
}
