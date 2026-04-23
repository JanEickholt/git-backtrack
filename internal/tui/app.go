package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
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
	ViewBranch
	ViewSettings
)

type SettingItem int

const (
	SettingOverviewMode SettingItem = iota
	SettingTimezone
	SettingEmail
	SettingLineDiffs
	settingCount
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
	undoStack       [][]gitops.ForgeChange
	selectedCommits map[string]bool
	scrollOffset    int
	settingsIndex   int

	editingCommit *gitops.CommitInfo
	editFields    []textinput.Model
	messageField  textarea.Model
	focusField    EditField

	batchFields []textinput.Model
	batchFocus  int

	result  *gitops.RewriteResult
	err     error
	list    list.Model
	help    help.Model
	width   int
	height  int
	keys    keyMap
	options Options

	currentBranch string
	branchList    list.Model
}

type Options struct {
	CleanView     bool
	PlainView     bool
	ShowTimezone  bool
	ShowEmail     bool
	HideLineDiffs bool
}

func (o Options) disablesGraph() bool {
	return o.CleanView || o.PlainView
}

func (o Options) showsLineDiffs() bool {
	return !o.HideLineDiffs
}

func NewModel(repo *gitops.Repository) Model {
	return NewModelWithOptions(repo, Options{})
}

func NewModelWithOptions(repo *gitops.Repository, options Options) Model {
	headRef, _ := repo.GetHead()
	currentBranch := ""
	if headRef != nil && headRef.Name().IsBranch() {
		currentBranch = headRef.Name().Short()
	}

	var commits []gitops.CommitInfo
	var graph *gitops.Graph
	var err error
	if currentBranch != "" {
		if options.disablesGraph() {
			commits, err = repo.ListCommitsFromRef("refs/heads/" + currentBranch)
		} else {
			commits, graph, err = repo.ListCommitsFromRefWithGraph("refs/heads/" + currentBranch)
		}
	} else if options.disablesGraph() {
		commits, err = repo.ListAllCommits()
	} else {
		commits, graph, err = repo.ListAllCommitsWithGraph()
	}
	if graph == nil {
		graph = &gitops.Graph{}
	}
	if err != nil {
		return Model{
			err:             err,
			commits:         []gitops.CommitInfo{},
			graph:           &gitops.Graph{},
			options:         options,
			editQueue:       make([]gitops.ForgeChange, 0),
			editMap:         make(map[string]*gitops.ForgeChange),
			selectedCommits: make(map[string]bool),
			help:            help.New(),
			keys:            defaultKeyMap(),
		}
	}

	delegate := commitDelegate{}
	l := list.New(commitListItems(commits), delegate, 80, 20)
	l.Title = "git-backtrack"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle

	branches, _ := repo.ListBranches()
	branchItems := make([]list.Item, len(branches))
	for i, b := range branches {
		branchItems[i] = branchItem{name: b}
	}
	bd := branchDelegate{}
	bl := list.New(branchItems, bd, 80, 20)
	bl.Title = "Switch Branch"
	bl.SetShowStatusBar(false)
	bl.SetFilteringEnabled(false)
	bl.Styles.Title = titleStyle

	return Model{
		state:           ViewList,
		repo:            repo,
		commits:         commits,
		graph:           graph,
		editQueue:       make([]gitops.ForgeChange, 0),
		editMap:         make(map[string]*gitops.ForgeChange),
		selectedCommits: make(map[string]bool),
		help:            help.New(),
		keys:            defaultKeyMap(),
		options:         options,
		list:            l,
		currentBranch:   currentBranch,
		branchList:      bl,
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
		m.branchList.SetSize(msg.Width, m.height-2)
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
		case ViewBranch:
			return m.handleBranchKey(msg)
		case ViewSettings:
			return m.handleSettingsKey(msg)
		}
	}

	var cmd tea.Cmd
	switch m.state {
	case ViewBranch:
		m.branchList, cmd = m.branchList.Update(msg)
	default:
		m.list, cmd = m.list.Update(msg)
	}
	return m, cmd
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.PageUp):
		m.moveListPage(-1)
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		m.moveListPage(1)
		return m, nil

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

	case key.Matches(msg, m.keys.Undo):
		m.undoLastChange()
		return m, nil

	case key.Matches(msg, m.keys.Drop):
		if len(m.commits) == 0 {
			return m, nil
		}
		selectedCommits := m.selectedCommitInfos()
		if len(selectedCommits) > 0 {
			m.applyWithUndo(func() {
				m.toggleDropForCommits(selectedCommits)
			})
			return m, nil
		}
		idx := m.list.Index()
		if idx >= 0 && idx < len(m.commits) {
			m.applyWithUndo(func() {
				m.toggleDropForCommits([]gitops.CommitInfo{m.commits[idx]})
			})
		}
		return m, nil

	case key.Matches(msg, m.keys.Combine):
		selectedCommits := m.selectedCommitInfos()
		if len(selectedCommits) < 2 {
			return m, nil
		}
		idx := m.list.Index()
		if idx < 0 || idx >= len(m.commits) || !m.selectedCommits[m.commits[idx].Hash.String()] {
			return m, nil
		}
		m.applyWithUndo(func() {
			m.toggleCombineForCommits(selectedCommits, m.commits[idx].Hash)
		})
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
			m.applyWithUndo(func() {
				m.removeChange(hash)
			})
		}
		return m, nil

	case key.Matches(msg, m.keys.Apply):
		if len(m.editQueue) == 0 {
			return m, nil
		}
		m.state = ViewConfirm
		return m, nil

	case key.Matches(msg, m.keys.SwitchBranch):
		m.state = ViewBranch
		return m, nil

	case key.Matches(msg, m.keys.Settings):
		m.state = ViewSettings
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.updateListScrollOffset(m.visibleListRows())
	return m, cmd
}

func (m *Model) moveListPage(direction int) {
	if len(m.commits) == 0 || direction == 0 {
		return
	}
	maxRows := m.visibleListRows()
	selectedIndex := m.list.Index()
	targetIndex := pageMoveTargetIndex(len(m.commits), selectedIndex, maxRows, direction)
	if !m.options.disablesGraph() && m.graph != nil {
		targetIndex = pageMoveTargetCommit(m.graph, len(m.commits), selectedIndex, maxRows, direction)
	}
	m.list.Select(targetIndex)
	m.updateListScrollOffset(maxRows)
}

func (m *Model) updateListScrollOffset(maxRows int) {
	selectedIndex := m.list.Index()
	if m.options.disablesGraph() {
		m.scrollOffset = scrollOffsetForSelectedIndex(len(m.commits), selectedIndex, m.scrollOffset, maxRows)
		return
	}
	if m.graph == nil {
		return
	}
	m.scrollOffset = scrollOffsetForSelectedCommit(m.graph, selectedIndex, m.scrollOffset, maxRows)
}

func (m Model) visibleListRows() int {
	maxRows := m.height - 2
	if maxRows <= 0 {
		maxRows = 20
	}
	return maxRows
}

func (m *Model) initEditFields() {
	m.editFields = make([]textinput.Model, 4)

	commit := m.editingCommit
	existingChange := m.editMap[commit.Hash.String()]

	name := commit.AuthorName
	email := commit.AuthorEmail
	date := commit.AuthorDate.In(time.Local).Format("2006-01-02")
	timeStr := commit.AuthorDate.In(time.Local).Format("15:04:05")
	msg := commit.Message

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
	msg = displayCommitMessage(msg)

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

	m.messageField = textarea.New()
	m.messageField.Placeholder = "Commit message"
	m.messageField.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline"))
	m.messageField.SetValue(msg)
	m.messageField.SetWidth(60)
	m.messageField.SetHeight(messageHeight(msg))

	m.focusField = FieldName
}

func messageHeight(msg string) int {
	if msg == "" {
		return 3
	}
	lines := strings.Count(msg, "\n") + 1
	height := lines + 2
	if height < 3 {
		height = 3
	}
	if height > 10 {
		height = 10
	}
	return height
}

func displayCommitMessage(message string) string {
	message = stripTerminalControls(message)
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line), " ", ""))
		if strings.HasPrefix(normalized, "#conflicts") || strings.HasPrefix(normalized, "#conflics") {
			return strings.TrimRight(strings.Join(lines[:i], "\n"), "\n")
		}
	}
	return message
}

func stripTerminalControls(value string) string {
	var b strings.Builder
	skippingEscape := false
	skippingCSI := false
	for _, r := range value {
		if skippingCSI {
			if r >= 0x40 && r <= 0x7e {
				skippingCSI = false
			}
			continue
		}
		if skippingEscape {
			if r == '[' {
				skippingCSI = true
			}
			skippingEscape = false
			continue
		}
		if r == 0x1b {
			skippingEscape = true
			continue
		}
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
		before := cloneForgeChanges(m.editQueue)
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
		m.recordUndoIfChanged(before)
		m.state = ViewList
		m.editingCommit = nil
		return m, nil

	case key.Matches(msg, m.keys.Tab):
		if m.focusField == Message {
			m.messageField.Blur()
		} else {
			m.editFields[m.focusField].Blur()
		}
		m.focusField = (m.focusField + 1) % 5
		if m.focusField == Message {
			m.messageField.Focus()
		} else {
			m.editFields[m.focusField].Focus()
		}
		return m, nil

	case key.Matches(msg, m.keys.ShiftTab):
		if m.focusField == Message {
			m.messageField.Blur()
		} else {
			m.editFields[m.focusField].Blur()
		}
		m.focusField = (m.focusField + 4) % 5
		if m.focusField == Message {
			m.messageField.Focus()
		} else {
			m.editFields[m.focusField].Focus()
		}
		return m, nil
	}

	if m.focusField == Message {
		m.messageField, cmd = m.messageField.Update(msg)
	} else {
		m.editFields[m.focusField], cmd = m.editFields[m.focusField].Update(msg)
	}
	return m, cmd
}

func (m Model) handleBranchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Cancel):
		m.state = ViewList
		return m, nil

	case key.Matches(msg, m.keys.Confirm):
		idx := m.branchList.Index()
		if idx >= 0 && idx < len(m.branchList.Items()) {
			item := m.branchList.Items()[idx]
			if bi, ok := item.(branchItem); ok {
				err := m.repo.SwitchBranch(bi.name)
				if err != nil {
					m.err = err
					m.state = ViewList
					return m, nil
				}
				m.currentBranch = bi.name
				m.editQueue = make([]gitops.ForgeChange, 0)
				m.editMap = make(map[string]*gitops.ForgeChange)
				m.undoStack = nil
				m.selectedCommits = make(map[string]bool)
				m.refresh()
			}
		}
		m.state = ViewList
		return m, nil
	}

	var cmd tea.Cmd
	m.branchList, cmd = m.branchList.Update(msg)
	return m, cmd
}

func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel), key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Settings):
		m.state = ViewList
		return m, nil

	case key.Matches(msg, m.keys.Up):
		m.settingsIndex = (m.settingsIndex + int(settingCount) - 1) % int(settingCount)
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.settingsIndex = (m.settingsIndex + 1) % int(settingCount)
		return m, nil

	case key.Matches(msg, m.keys.Confirm), key.Matches(msg, m.keys.Select):
		m.toggleSetting(SettingItem(m.settingsIndex))
		m.updateListScrollOffset(m.visibleListRows())
		return m, nil
	}
	return m, nil
}

func (m *Model) toggleSetting(item SettingItem) {
	switch item {
	case SettingOverviewMode:
		m.cycleOverviewMode()
	case SettingTimezone:
		m.options.ShowTimezone = !m.options.ShowTimezone
	case SettingEmail:
		m.options.ShowEmail = !m.options.ShowEmail
	case SettingLineDiffs:
		m.options.HideLineDiffs = !m.options.HideLineDiffs
	}
}

func (m *Model) cycleOverviewMode() {
	switch {
	case !m.options.disablesGraph():
		m.options.PlainView = true
	case m.options.PlainView:
		m.options.PlainView = false
		m.options.CleanView = true
	default:
		m.options.CleanView = false
		if err := m.ensureGraphLoaded(); err != nil {
			m.err = err
		}
	}
}

func (m *Model) ensureGraphLoaded() error {
	if m.graph != nil && len(m.graph.Rows) > 0 {
		return nil
	}

	var commits []gitops.CommitInfo
	var graph *gitops.Graph
	var err error
	if m.currentBranch != "" {
		commits, graph, err = m.repo.ListCommitsFromRefWithGraph("refs/heads/" + m.currentBranch)
	} else {
		commits, graph, err = m.repo.ListAllCommitsWithGraph()
	}
	if err != nil {
		return err
	}
	if graph == nil {
		graph = &gitops.Graph{}
	}
	m.commits = commits
	m.graph = graph
	m.list.SetItems(commitListItems(commits))
	return nil
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
	before := cloneForgeChanges(m.editQueue)

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
		if existingChange != nil && (existingChange.Operation == gitops.ForgeDrop || existingChange.Operation == gitops.ForgeCombine) {
			continue
		}
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

	m.recordUndoIfChanged(before)
	m.selectedCommits = make(map[string]bool)
	m.state = ViewList
	return m, nil
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

	if m.messageField.Value() != displayCommitMessage(m.editingCommit.Message) {
		change.NewMessage = m.messageField.Value()
	}

	return change
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
				m.refresh()
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
	var commits []gitops.CommitInfo
	var graph *gitops.Graph
	var err error
	if m.currentBranch != "" {
		if m.options.disablesGraph() {
			commits, err = m.repo.ListCommitsFromRef("refs/heads/" + m.currentBranch)
		} else {
			commits, graph, err = m.repo.ListCommitsFromRefWithGraph("refs/heads/" + m.currentBranch)
		}
	} else if m.options.disablesGraph() {
		commits, err = m.repo.ListAllCommits()
	} else {
		commits, graph, err = m.repo.ListAllCommitsWithGraph()
	}
	if err != nil {
		return
	}
	if graph == nil {
		graph = &gitops.Graph{}
	}
	m.commits = commits
	m.graph = graph
	m.editQueue = make([]gitops.ForgeChange, 0)
	m.editMap = make(map[string]*gitops.ForgeChange)
	m.undoStack = nil
	m.list.SetItems(commitListItems(commits))
}

func commitListItems(commits []gitops.CommitInfo) []list.Item {
	items := make([]list.Item, len(commits))
	for i, commit := range commits {
		items[i] = commitItem{commit: commit}
	}
	return items
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
	case ViewBranch:
		return m.renderBranchView()
	case ViewSettings:
		return m.renderSettingsView()
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
	if !m.options.disablesGraph() && (m.graph == nil || len(m.graph.Rows) == 0) {
		return statusStyle.Render("Loading...")
	}
	selectedCount := 0
	for _, v := range m.selectedCommits {
		if v {
			selectedCount++
		}
	}

	var headerParts []string
	headerParts = append(headerParts, "git-backtrack")
	if m.currentBranch != "" {
		headerParts = append(headerParts, m.currentBranch)
	}
	headerParts = append(headerParts, fmt.Sprintf("%d commits", len(m.commits)))
	if selectedCount > 0 {
		headerParts = append(headerParts, fmt.Sprintf("%d selected", selectedCount))
	}
	if len(m.editQueue) > 0 {
		headerParts = append(headerParts, fmt.Sprintf("%d pending", len(m.editQueue)))
	}

	b.WriteString(titleStyle.Render(strings.Join(headerParts, " | ")))
	b.WriteString("\n")
	maxRows := m.height - 2
	if maxRows <= 0 {
		maxRows = 20
	}
	if m.options.CleanView {
		m.renderCleanRows(&b, maxRows, selectedCount)
		return b.String()
	}
	if m.options.PlainView {
		m.renderPlainRows(&b, maxRows, selectedCount)
		return b.String()
	}

	bg := lipgloss.Color("237")
	bgStyle := lipgloss.NewStyle().Background(bg)
	style := gitops.DefaultGraphStyle()
	folds := m.foldDisplayIndex()
	authorWidth := m.authorColumnWidth(folds)
	statWidth := gitops.StatColumnWidth(m.commits)

	visualLinesRendered := 0
	rowIndex := m.scrollOffset
	for rowIndex < len(m.graph.Rows) && visualLinesRendered < maxRows {
		row := m.graph.Rows[rowIndex]
		lineWidth := m.width - 6

		if !row.IsCommit || row.CommitIndex < 0 || row.CommitIndex >= len(m.commits) {
			line := gitops.RenderGraphLineWithColumnWidths(m.graph, m.commits, rowIndex, lineWidth, style, false, "", 0, authorWidth, statWidth)
			b.WriteString("      ")
			b.WriteString(line)
			b.WriteString("\n")
			visualLinesRendered++
			rowIndex++
			continue
		}

		commitIndex := row.CommitIndex
		commit := m.commits[commitIndex]
		highlight := commitIndex == m.list.Index()
		hasWarning := hasTimeAnomaly(commit, m.commits, m.editMap)
		change := m.editMap[commit.Hash.String()]
		suffix, suffixWidth := renderCommitSuffix(change, hasWarning, highlight, bg)
		showLineDiffs := m.options.showsLineDiffs()

		line := gitops.RenderGraphLineWithColumnWidthsAndOptions(m.graph, m.commits, rowIndex, lineWidth, style, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
		if change != nil {
			prefixText := truncateForWidth(row.Prefix, lineWidth)
			prefix := gitops.RenderGraphPrefix(prefixText, style, highlight)
			commitWidth := lineWidth - len(prefixText)
			if commitWidth < 0 {
				commitWidth = 0
			}
			if change.Operation == gitops.ForgeDrop {
				line = prefix + renderDroppedCommit(commit, commitWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
			} else if change.Operation == gitops.ForgeCombine {
				fold := folds.ByHash[commit.Hash.String()]
				line = prefix + renderCombinedCommit(commit, fold, commitWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
			} else {
				line = prefix + renderModifiedCommit(commit, change, commitWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
			}
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
		rowIndex++
	}

	b.WriteString(statusStyle.Render(m.listStatusText(selectedCount)))
	return b.String()
}

func (m Model) renderCleanRows(b *strings.Builder, maxRows int, selectedCount int) {
	bg := lipgloss.Color("237")
	bgStyle := lipgloss.NewStyle().Background(bg)
	folds := m.foldDisplayIndex()

	start := scrollOffsetForSelectedIndex(len(m.commits), m.list.Index(), m.scrollOffset, maxRows)
	if start < 0 {
		start = 0
	}
	lineWidth := m.width - 6
	if lineWidth <= 0 {
		lineWidth = 80
	}

	visualLinesRendered := 0
	for commitIndex := start; commitIndex < len(m.commits) && visualLinesRendered < maxRows; commitIndex++ {
		commit := m.commits[commitIndex]
		highlight := commitIndex == m.list.Index()
		hasWarning := hasTimeAnomaly(commit, m.commits, m.editMap)
		change := m.editMap[commit.Hash.String()]
		suffix, suffixWidth := renderCommitSuffix(change, hasWarning, highlight, bg)
		line := renderCleanCommit(commit, change, folds.ByHash[commit.Hash.String()], lineWidth, highlight, suffix, suffixWidth, m.options.ShowTimezone, m.options.ShowEmail, m.options.showsLineDiffs())
		selected := m.selectedCommits[commit.Hash.String()]
		var selMarker string
		if selected {
			if highlight {
				selMarker = editStyle.Background(bg).Render("[x] ")
			} else {
				selMarker = editStyle.Render("[x] ")
			}
		} else if highlight {
			selMarker = bgStyle.Render("    ")
		} else {
			selMarker = "    "
		}

		if highlight {
			b.WriteString(bgStyle.Render("> "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(selMarker)
		b.WriteString(line)
		b.WriteString("\n")
		visualLinesRendered++
	}

	b.WriteString(statusStyle.Render(m.listStatusText(selectedCount)))
}

func (m Model) renderPlainRows(b *strings.Builder, maxRows int, selectedCount int) {
	bg := lipgloss.Color("237")
	bgStyle := lipgloss.NewStyle().Background(bg)
	folds := m.foldDisplayIndex()
	authorWidth := m.authorColumnWidth(folds)
	statWidth := gitops.StatColumnWidth(m.commits)

	start := scrollOffsetForSelectedIndex(len(m.commits), m.list.Index(), m.scrollOffset, maxRows)
	if start < 0 {
		start = 0
	}
	lineWidth := m.width - 6
	if lineWidth <= 0 {
		lineWidth = 80
	}

	visualLinesRendered := 0
	for commitIndex := start; commitIndex < len(m.commits) && visualLinesRendered < maxRows; commitIndex++ {
		commit := m.commits[commitIndex]
		highlight := commitIndex == m.list.Index()
		hasWarning := hasTimeAnomaly(commit, m.commits, m.editMap)
		change := m.editMap[commit.Hash.String()]
		suffix, suffixWidth := renderCommitSuffix(change, hasWarning, highlight, bg)
		showLineDiffs := m.options.showsLineDiffs()
		line := gitops.RenderCommitLineWithColumnWidthsAndOptions(commit, lineWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
		if change != nil {
			if change.Operation == gitops.ForgeDrop {
				line = renderDroppedCommit(commit, lineWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
			} else if change.Operation == gitops.ForgeCombine {
				line = renderCombinedCommit(commit, folds.ByHash[commit.Hash.String()], lineWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
			} else {
				line = renderModifiedCommit(commit, change, lineWidth, highlight, suffix, suffixWidth, authorWidth, statWidth, m.options.ShowTimezone, m.options.ShowEmail, showLineDiffs)
			}
		}

		selected := m.selectedCommits[commit.Hash.String()]
		var selMarker string
		if selected {
			if highlight {
				selMarker = editStyle.Background(bg).Render("[x] ")
			} else {
				selMarker = editStyle.Render("[x] ")
			}
		} else if highlight {
			selMarker = bgStyle.Render("    ")
		} else {
			selMarker = "    "
		}

		if highlight {
			b.WriteString(bgStyle.Render("> "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(selMarker)
		b.WriteString(line)
		b.WriteString("\n")
		visualLinesRendered++
	}

	b.WriteString(statusStyle.Render(m.listStatusText(selectedCount)))
}

func (m Model) listStatusText(selectedCount int) string {
	parts := []string{}
	if selectedCount > 0 {
		parts = append(parts, "d:drop", "f:fold", "space:select", "b:batch")
	} else {
		parts = append(parts, "e:edit", "d:drop", "space:select", "b:batch")
	}
	if len(m.undoStack) > 0 {
		parts = append(parts, "u:undo")
	}
	parts = append(parts, "c:switch", "s:settings", "a:apply", "q:quit")
	return strings.Join(parts, " ")
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

	labels := []string{"Author Name", "Author Email", "Date (YYYY-MM-DD)", "Time (HH:MM:SS)"}
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

	prefix := "  "
	if m.focusField == Message {
		prefix = "> "
	}
	b.WriteString(prefix + labelStyle.Render("Message:"))
	b.WriteString("\n")
	if m.focusField == Message {
		b.WriteString(editStyle.Render(m.messageField.View()))
	} else {
		b.WriteString(statusStyle.Render(m.messageField.View()))
	}
	b.WriteString("\n\n")

	b.WriteString(statusStyle.Render("Original: "))
	b.WriteString(fmt.Sprintf("%s <%s> %s",
		m.editingCommit.AuthorName,
		m.editingCommit.AuthorEmail,
		formatCommitTime(m.editingCommit.AuthorDate, m.options.ShowTimezone)))
	b.WriteString("\n")
	b.WriteString(statusStyle.Render("Original Message:"))
	b.WriteString("\n")
	b.WriteString(displayCommitMessage(m.editingCommit.Message))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("[Tab]") + " next  ")
	b.WriteString(labelStyle.Render("[Enter]") + " save  ")
	b.WriteString(labelStyle.Render("[Shift+Enter]") + " newline  ")
	b.WriteString(labelStyle.Render("[Esc]") + " cancel")

	return b.String()
}

func (m Model) renderConfirmView() string {
	var b strings.Builder
	folds := m.foldDisplayIndex()
	renderedFolds := make(map[int]bool)
	itemIndex := 1

	b.WriteString(titleStyle.Render("Confirm Changes"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("About to rewrite %d commits:\n\n", len(m.editQueue)))

	for _, change := range m.editQueue {
		if change.Operation == gitops.ForgeCombine {
			fold := folds.ByHash[change.OriginalHash.String()]
			if renderedFolds[fold.ID] {
				continue
			}
			renderedFolds[fold.ID] = true
			b.WriteString(fmt.Sprintf("  %d. Fold %d: %s\n", itemIndex, fold.ID, shortHashList(fold.Hashes)))
			b.WriteString(fmt.Sprintf("     Operation: fold %d commits into one\n", len(fold.Hashes)))
			b.WriteString("\n")
			itemIndex++
			continue
		}

		b.WriteString(fmt.Sprintf("  %d. %s\n", itemIndex, change.OriginalHash.String()[:7]))
		if change.Operation == gitops.ForgeDrop {
			b.WriteString("     Operation: drop commit\n")
			b.WriteString("\n")
			itemIndex++
			continue
		}
		if change.NewAuthor != nil {
			b.WriteString(fmt.Sprintf("     Author: %s <%s>\n", change.NewAuthor.Name, change.NewAuthor.Email))
		}
		if change.NewDate != nil {
			b.WriteString(fmt.Sprintf("     Date: %s\n", formatCommitTime(*change.NewDate, m.options.ShowTimezone)))
		}
		if change.NewMessage != "" {
			fullMsg := strings.ReplaceAll(change.NewMessage, "\n", " ")
			b.WriteString(fmt.Sprintf("     Message: %s\n", fullMsg))
		}
		b.WriteString("\n")
		itemIndex++
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

func (m Model) renderBranchView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Switch Branch"))
	b.WriteString("\n\n")
	b.WriteString(m.branchList.View())
	b.WriteString("\n")
	b.WriteString(statusStyle.Render("enter:select esc:cancel"))
	return b.String()
}

func (m Model) renderSettingsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Settings"))
	b.WriteString("\n\n")

	rows := []struct {
		label string
		value string
	}{
		{label: "Overview", value: m.overviewModeLabel()},
		{label: "Timezone offsets", value: settingEnabledLabel(m.options.ShowTimezone)},
		{label: "Author emails", value: settingEnabledLabel(m.options.ShowEmail)},
		{label: "Line diffs", value: settingEnabledLabel(m.options.showsLineDiffs())},
	}

	for i, row := range rows {
		prefix := "  "
		labelStyleToUse := labelStyle
		valueStyle := statusStyle
		if i == m.settingsIndex {
			prefix = "> "
			labelStyleToUse = labelStyle.Background(lipgloss.Color("237"))
			valueStyle = statusStyle.Background(lipgloss.Color("237"))
		}
		b.WriteString(prefix)
		b.WriteString(labelStyleToUse.Render(row.label))
		b.WriteString(": ")
		b.WriteString(valueStyle.Render(row.value))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(statusStyle.Render("enter/space:toggle up/down:move s/esc/q:close"))
	return b.String()
}

func (m Model) overviewModeLabel() string {
	switch {
	case m.options.CleanView:
		return "clean"
	case m.options.PlainView:
		return "plain"
	default:
		return "graph"
	}
}

func settingEnabledLabel(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (m Model) authorColumnWidth(folds foldDisplayIndex) int {
	width := gitops.AuthorColumnWidthWithEmail(m.commits, m.options.ShowEmail)
	for _, commit := range m.commits {
		if change := m.editMap[commit.Hash.String()]; change != nil {
			name := gitops.FormatCommitAuthorIdentity(commit.AuthorName, commit.AuthorEmail, m.options.ShowEmail)
			if change.Operation == gitops.ForgeDrop {
				name = "[drop] " + name
			} else if change.Operation == gitops.ForgeCombine {
				name = folds.ByHash[commit.Hash.String()].Label + name
			} else if change.NewAuthor != nil {
				name = gitops.FormatCommitAuthorIdentity(change.NewAuthor.Name, change.NewAuthor.Email, m.options.ShowEmail || change.NewAuthor.Email != "")
			}
			if len(name) > width {
				width = len(name)
			}
		}
	}
	return width
}

func renderDroppedCommit(original gitops.CommitInfo, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	return renderTaggedCommit(original, "[drop] ", lipgloss.Color("9"), width, highlight, suffix, suffixWidth, authorWidth, statWidth, showTimezone, showEmail, showLineDiffs)
}

func renderCombinedCommit(original gitops.CommitInfo, fold foldDisplay, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	label := fold.Label
	color := fold.Color
	if label == "" {
		label = "[fold] "
	}
	if color == "" {
		color = "11"
	}
	return renderTaggedCommit(original, label, lipgloss.Color(color), width, highlight, suffix, suffixWidth, authorWidth, statWidth, showTimezone, showEmail, showLineDiffs)
}

func renderCommitSuffix(change *gitops.ForgeChange, hasTimeWarning bool, highlight bool, bg lipgloss.Color) (string, int) {
	parts := make([]string, 0, 3)
	width := 0
	if hasTimeWarning {
		text := " ⚠️time"
		style := errorStyle
		if highlight {
			style = style.Background(bg)
		}
		parts = append(parts, style.Render(text))
		width += lipgloss.Width(text)
	}
	if change != nil && change.NewDate != nil {
		text := " [time]"
		style := editStyle
		if highlight {
			style = style.Background(bg)
		}
		parts = append(parts, style.Render(text))
		width += lipgloss.Width(text)
	}
	if change != nil && change.NewMessage != "" {
		text := " [msg]"
		style := editStyle
		if highlight {
			style = style.Background(bg)
		}
		parts = append(parts, style.Render(text))
		width += lipgloss.Width(text)
	}
	return strings.Join(parts, ""), width
}

func renderCleanCommit(original gitops.CommitInfo, change *gitops.ForgeChange, fold foldDisplay, width int, highlight bool, suffix string, suffixWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	name := gitops.FormatCommitAuthorIdentity(original.AuthorName, original.AuthorEmail, showEmail)
	date := formatCommitTime(original.AuthorDate, showTimezone)
	msg := strings.Split(original.Message, "\n")[0]
	hashColor := lipgloss.Color("14")
	nameColor := lipgloss.Color("12")

	if change != nil {
		switch change.Operation {
		case gitops.ForgeDrop:
			name = "[drop] " + name
			hashColor = lipgloss.Color("9")
			nameColor = lipgloss.Color("9")
		case gitops.ForgeCombine:
			label := fold.Label
			color := fold.Color
			if label == "" {
				label = "[fold] "
			}
			if color == "" {
				color = "11"
			}
			name = label + name
			hashColor = lipgloss.Color(color)
			nameColor = lipgloss.Color(color)
		default:
			if change.NewAuthor != nil {
				name = gitops.FormatCommitAuthorIdentity(change.NewAuthor.Name, change.NewAuthor.Email, showEmail || change.NewAuthor.Email != "")
			}
			if change.NewDate != nil {
				date = formatCommitTime(*change.NewDate, showTimezone)
			}
			if change.NewMessage != "" {
				msg = strings.Split(change.NewMessage, "\n")[0]
			}
		}
	}

	bg := lipgloss.Color("237")
	hashStyle := lipgloss.NewStyle().Foreground(hashColor).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(nameColor)
	dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sepStyle := lipgloss.NewStyle()
	if highlight {
		hashStyle = hashStyle.Background(bg)
		nameStyle = nameStyle.Background(bg)
		dateStyle = dateStyle.Background(bg)
		addStyle = addStyle.Background(bg)
		delStyle = delStyle.Background(bg)
		msgStyle = msgStyle.Background(bg)
		sepStyle = sepStyle.Background(bg)
	}

	addStr := fmt.Sprintf("+%d", original.Additions)
	delStr := fmt.Sprintf("-%d", original.Deletions)
	staticWidth := len(original.ShortHash) + 1 + len(name) + 1 + len(date) + 1
	if showLineDiffs {
		statsWidth := len(addStr) + 1 + len(delStr)
		staticWidth += statsWidth + 1
	}
	msg = truncateForWidth(msg, width-staticWidth-suffixWidth)

	sep := sepStyle.Render(" ")
	line := hashStyle.Render(original.ShortHash) + sep +
		nameStyle.Render(name) + sep +
		dateStyle.Render(date) + sep
	if showLineDiffs {
		line += addStyle.Render(addStr) + sep + delStyle.Render(delStr) + sep
	}
	line += msgStyle.Render(msg)

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

func renderTaggedCommit(original gitops.CommitInfo, tag string, color lipgloss.Color, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	msg := strings.Split(original.Message, "\n")[0]
	date := formatCommitTime(original.AuthorDate, showTimezone)
	name := gitops.FormatCommitAuthor(tag+gitops.FormatCommitAuthorIdentity(original.AuthorName, original.AuthorEmail, showEmail), authorWidth)

	bg := lipgloss.Color("237")
	hashStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sepStyle := lipgloss.NewStyle()

	if highlight {
		hashStyle = hashStyle.Background(bg)
		nameStyle = nameStyle.Background(bg)
		addStyle = addStyle.Background(bg)
		delStyle = delStyle.Background(bg)
		textStyle = textStyle.Background(bg)
		sepStyle = sepStyle.Background(bg)
	}

	addStr := gitops.FormatCommitStat("+", original.Additions, statWidth)
	delStr := gitops.FormatCommitStat("-", original.Deletions, statWidth)
	staticWidth := len(original.ShortHash) + 2 + authorWidth + 2 + len(date) + 2
	if showLineDiffs {
		staticWidth += statWidth + 1 + statWidth + 2
	}
	availableForMsg := width - staticWidth - suffixWidth
	msg = truncateForWidth(msg, availableForMsg)

	sep := sepStyle.Render("  ")
	statSep := sepStyle.Render(" ")
	line := hashStyle.Render(original.ShortHash) + sep + nameStyle.Render(name) + sep + textStyle.Render(date) + sep
	if showLineDiffs {
		line += addStyle.Render(addStr) + statSep + delStyle.Render(delStr) + sep
	}
	line += textStyle.Render(msg)

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

func renderModifiedCommit(original gitops.CommitInfo, change *gitops.ForgeChange, width int, highlight bool, suffix string, suffixWidth int, authorWidth int, statWidth int, showTimezone bool, showEmail bool, showLineDiffs bool) string {
	name := gitops.FormatCommitAuthorIdentity(original.AuthorName, original.AuthorEmail, showEmail)
	date := formatCommitTime(original.AuthorDate, showTimezone)
	msg := strings.Split(original.Message, "\n")[0]

	if change.NewAuthor != nil {
		name = gitops.FormatCommitAuthorIdentity(change.NewAuthor.Name, change.NewAuthor.Email, showEmail || change.NewAuthor.Email != "")
	}
	if change.NewDate != nil {
		date = formatCommitTime(*change.NewDate, showTimezone)
	}
	if change.NewMessage != "" {
		msg = strings.Split(change.NewMessage, "\n")[0]
	}

	bg := lipgloss.Color("237")
	hashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sepStyle := lipgloss.NewStyle()

	if highlight {
		hashStyle = hashStyle.Background(bg)
		nameStyle = nameStyle.Background(bg)
		dateStyle = dateStyle.Background(bg)
		addStyle = addStyle.Background(bg)
		delStyle = delStyle.Background(bg)
		msgStyle = msgStyle.Background(bg)
		sepStyle = sepStyle.Background(bg)
	}

	hashPart := hashStyle.Render(original.ShortHash)
	namePart := nameStyle.Render(gitops.FormatCommitAuthor(name, authorWidth))
	datePart := dateStyle.Render(date)
	addPart := addStyle.Render(gitops.FormatCommitStat("+", original.Additions, statWidth))
	delPart := delStyle.Render(gitops.FormatCommitStat("-", original.Deletions, statWidth))

	staticWidth := len(original.ShortHash) + 2 + authorWidth + 2 + len(date) + 2
	if showLineDiffs {
		staticWidth += statWidth + 1 + statWidth + 2
	}
	availableForMsg := width - staticWidth - suffixWidth
	msg = truncateForWidth(msg, availableForMsg)

	sep := sepStyle.Render("  ")
	statSep := sepStyle.Render(" ")
	line := hashPart + sep + namePart + sep + datePart + sep
	if showLineDiffs {
		line += addPart + statSep + delPart + sep
	}
	line += msgStyle.Render(msg)

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

	line := fmt.Sprintf("%s  %s  %s  +%d -%d  %s",
		i.commit.ShortHash,
		i.commit.AuthorName,
		formatCommitTime(i.commit.AuthorDate, false),
		i.commit.Additions,
		i.commit.Deletions,
		strings.Split(i.commit.Message, "\n")[0],
	)

	if index%2 == 0 {
		line = lipgloss.NewStyle().Background(lipgloss.Color("235")).Render(line)
	}

	fmt.Fprintln(w, line)
}

type branchItem struct {
	name string
}

func (i branchItem) FilterValue() string { return i.name }

type branchDelegate struct{}

func (d branchDelegate) Height() int                             { return 1 }
func (d branchDelegate) Spacing() int                            { return 0 }
func (d branchDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d branchDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(branchItem)
	if !ok {
		return
	}

	prefix := "  "
	if index == m.Index() {
		prefix = "> "
	}

	line := prefix + i.name
	if index == m.Index() {
		line = selectedStyle.Render(line)
	} else {
		line = statusStyle.Render(line)
	}

	fmt.Fprintln(w, line)
}
