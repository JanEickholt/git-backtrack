package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jan/git-backtrack/internal/gitops"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestSetChangeReplacesExistingChange(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	model := Model{}

	model.setChange(gitops.ForgeChange{OriginalHash: hash})
	model.setChange(gitops.ForgeChange{OriginalHash: hash, Operation: gitops.ForgeDrop})

	if len(model.editQueue) != 1 {
		t.Fatalf("editQueue len = %d, want 1", len(model.editQueue))
	}
	if model.editQueue[0].Operation != gitops.ForgeDrop {
		t.Fatalf("operation = %v, want ForgeDrop", model.editQueue[0].Operation)
	}
	if model.editMap[hash.String()] != &model.editQueue[0] {
		t.Fatalf("editMap pointer was not rebuilt")
	}
}

func TestHandleListKeyCtrlArrowsMoveByPage(t *testing.T) {
	items := make([]list.Item, 20)
	commits := make([]gitops.CommitInfo, 20)
	for i := range commits {
		hash := plumbing.NewHash(strings.Repeat("1", 39) + string(rune('0'+i%10)))
		commits[i] = gitops.CommitInfo{Hash: hash}
		items[i] = commitItem{commit: commits[i]}
	}
	m := Model{
		commits: commits,
		list:    list.New(items, commitDelegate{}, 80, 20),
		height:  7,
		keys:    defaultKeyMap(),
		options: Options{PlainView: true},
	}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyCtrlDown})
	m = updated.(Model)
	if m.list.Index() != 5 {
		t.Fatalf("index after ctrl+down = %d, want 5", m.list.Index())
	}
	if m.scrollOffset != 1 {
		t.Fatalf("scrollOffset after ctrl+down = %d, want 1", m.scrollOffset)
	}

	updated, _ = m.handleListKey(tea.KeyMsg{Type: tea.KeyCtrlUp})
	m = updated.(Model)
	if m.list.Index() != 0 {
		t.Fatalf("index after ctrl+up = %d, want 0", m.list.Index())
	}
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset after ctrl+up = %d, want 0", m.scrollOffset)
	}
}

func TestHandleListKeyParentJumpsToFirstVisibleParent(t *testing.T) {
	child := plumbing.NewHash("1111111111111111111111111111111111111111")
	missingParent := plumbing.NewHash("2222222222222222222222222222222222222222")
	visibleParent := plumbing.NewHash("3333333333333333333333333333333333333333")
	commits := []gitops.CommitInfo{
		{Hash: child, Parents: []plumbing.Hash{missingParent, visibleParent}},
		{Hash: plumbing.NewHash("4444444444444444444444444444444444444444")},
		{Hash: visibleParent},
	}
	items := make([]list.Item, len(commits))
	for i := range commits {
		items[i] = commitItem{commit: commits[i]}
	}
	m := Model{
		commits: commits,
		list:    list.New(items, commitDelegate{}, 80, 20),
		height:  3,
		keys:    defaultKeyMap(),
		options: Options{PlainView: true},
	}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if m.list.Index() != 2 {
		t.Fatalf("index after p = %d, want 2", m.list.Index())
	}
	if m.scrollOffset != 2 {
		t.Fatalf("scrollOffset after p = %d, want 2", m.scrollOffset)
	}
}

func TestHandleListKeyParentJumpsWithGraphScroll(t *testing.T) {
	child := plumbing.NewHash("1111111111111111111111111111111111111111")
	parent := plumbing.NewHash("2222222222222222222222222222222222222222")
	commits := []gitops.CommitInfo{{Hash: child, Parents: []plumbing.Hash{parent}}, {Hash: parent}}
	items := make([]list.Item, len(commits))
	for i := range commits {
		items[i] = commitItem{commit: commits[i]}
	}
	m := Model{
		commits: commits,
		graph: &gitops.Graph{Rows: []gitops.GraphRow{
			{IsCommit: true, CommitIndex: 0},
			{},
			{},
			{IsCommit: true, CommitIndex: 1},
		}},
		list:   list.New(items, commitDelegate{}, 80, 20),
		height: 3,
		keys:   defaultKeyMap(),
	}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if m.list.Index() != 1 {
		t.Fatalf("index after p = %d, want 1", m.list.Index())
	}
	if m.scrollOffset != 3 {
		t.Fatalf("graph scrollOffset after p = %d, want 3", m.scrollOffset)
	}
}

func TestHandleListKeySettingsOpensSettings(t *testing.T) {
	m := Model{keys: defaultKeyMap()}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if m.state != ViewSettings {
		t.Fatalf("state = %v, want ViewSettings", m.state)
	}
}

func TestHandleListKeySwitchBranchUsesUppercaseB(t *testing.T) {
	m := Model{keys: defaultKeyMap()}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	m = updated.(Model)
	if m.state != ViewBranch {
		t.Fatalf("state = %v, want ViewBranch", m.state)
	}

	m = Model{keys: defaultKeyMap(), list: list.New(nil, commitDelegate{}, 80, 20)}
	updated, _ = m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(Model)
	if m.state == ViewBranch {
		t.Fatalf("lowercase c should not open branch switch")
	}
}

func TestHandleListKeyTimingFixSelectsDescendantsAndPrefillsBatch(t *testing.T) {
	parentTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	parent := plumbing.NewHash("1111111111111111111111111111111111111111")
	root := plumbing.NewHash("2222222222222222222222222222222222222222")
	child := plumbing.NewHash("3333333333333333333333333333333333333333")
	commits := []gitops.CommitInfo{
		{Hash: child, AuthorDate: parentTime.Add(5 * time.Second), Parents: []plumbing.Hash{root}},
		{Hash: root, AuthorDate: parentTime.Add(-1 * time.Second), Parents: []plumbing.Hash{parent}},
		{Hash: parent, AuthorDate: parentTime},
	}
	items := make([]list.Item, len(commits))
	for i := range commits {
		items[i] = commitItem{commit: commits[i]}
	}
	m := Model{
		commits:         commits,
		list:            list.New(items, commitDelegate{}, 80, 20),
		keys:            defaultKeyMap(),
		editMap:         make(map[string]*gitops.ForgeChange),
		selectedCommits: make(map[string]bool),
	}
	m.list.Select(1)

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m = updated.(Model)

	if m.state != ViewBatchEdit {
		t.Fatalf("state = %v, want ViewBatchEdit", m.state)
	}
	if m.batchFocus != 2 {
		t.Fatalf("batchFocus = %d, want 2", m.batchFocus)
	}
	if got := m.batchFields[2].Value(); got != "+2s" {
		t.Fatalf("time adjustment = %q, want +2s", got)
	}
	if !m.selectedCommits[root.String()] || !m.selectedCommits[child.String()] {
		t.Fatalf("root and child should be selected: %#v", m.selectedCommits)
	}
	if m.selectedCommits[parent.String()] {
		t.Fatalf("parent should not be selected")
	}
}

func TestHandleSettingsKeyTogglesSettings(t *testing.T) {
	m := Model{state: ViewSettings, keys: defaultKeyMap(), graph: &gitops.Graph{Rows: []gitops.GraphRow{{IsCommit: true}}}}

	updated, _ := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.options.PlainView || m.options.CleanView {
		t.Fatalf("options after first overview toggle = %+v, want plain", m.options)
	}

	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.options.CleanView || m.options.PlainView {
		t.Fatalf("options after second overview toggle = %+v, want clean", m.options)
	}

	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.options.CleanView || m.options.PlainView {
		t.Fatalf("options after third overview toggle = %+v, want graph", m.options)
	}

	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.options.ShowTimezone {
		t.Fatalf("ShowTimezone = false, want true")
	}

	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.options.ShowEmail {
		t.Fatalf("ShowEmail = false, want true")
	}

	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.options.HideLineDiffs {
		t.Fatalf("HideLineDiffs = false, want true")
	}
}

func TestHandleSettingsKeyCyclesGraphOrder(t *testing.T) {
	m := Model{state: ViewSettings, keys: defaultKeyMap(), options: Options{CleanView: true}, settingsIndex: int(SettingGraphOrder)}

	for _, want := range []gitops.GraphOrder{gitops.GraphOrderDate, gitops.GraphOrderAuthorDate, gitops.GraphOrderFirstParent, gitops.GraphOrderTopo} {
		updated, _ := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if m.options.GraphOrder != want {
			t.Fatalf("GraphOrder = %q, want %q", m.options.GraphOrder, want)
		}
	}
}

func TestHandleSettingsKeyClosesSettings(t *testing.T) {
	m := Model{state: ViewSettings, keys: defaultKeyMap()}

	updated, _ := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.state != ViewList {
		t.Fatalf("state = %v, want ViewList", m.state)
	}
}

func TestRenderListFooterShowsContextActions(t *testing.T) {
	m := Model{width: 160}

	footer := m.renderListFooter(0)
	for _, want := range []string{"e", "edit", "s", "settings"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer %q missing %q", footer, want)
		}
	}
	if strings.Contains(footer, "up/down") || strings.Contains(footer, "ctrl+up/down") || strings.Contains(footer, "move") || strings.Contains(footer, "page") {
		t.Fatalf("footer should not explain movement keys: %q", footer)
	}

	footer = m.renderListFooter(2)
	for _, want := range []string{"d", "drop", "f", "fold", "b", "batch"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("selected footer %q missing %q", footer, want)
		}
	}
}

func TestRenderFooterKeepsLabelsWhenNarrow(t *testing.T) {
	footer := renderFooter([]footerAction{
		{key: "up/down", label: "move"},
		{key: "enter", label: "save"},
	}, 8)

	for _, want := range []string{"up/down", "move", "enter", "save"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer %q missing %q", footer, want)
		}
	}
}

func TestHandleEditKeyEnterSavesMessageField(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	m := Model{
		state:           ViewEdit,
		keys:            defaultKeyMap(),
		editingCommit:   &gitops.CommitInfo{Hash: hash, AuthorName: "Jan", AuthorEmail: "jan@example.com", AuthorDate: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Message: "old message"},
		editMap:         make(map[string]*gitops.ForgeChange),
		selectedCommits: make(map[string]bool),
	}
	m.initEditFields()
	m.focusField = Message
	m.messageField.Focus()
	m.messageField.SetValue("new message")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.state != ViewList {
		t.Fatalf("state = %v, want ViewList", m.state)
	}
	if len(m.editQueue) != 1 || m.editQueue[0].NewMessage != "new message" {
		t.Fatalf("editQueue = %+v, want message change", m.editQueue)
	}
}

func TestHandleEditKeyAltEnterInsertsMessageNewline(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	m := Model{
		state:           ViewEdit,
		keys:            defaultKeyMap(),
		editingCommit:   &gitops.CommitInfo{Hash: hash, AuthorName: "Jan", AuthorEmail: "jan@example.com", AuthorDate: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Message: "subject"},
		editMap:         make(map[string]*gitops.ForgeChange),
		selectedCommits: make(map[string]bool),
	}
	m.initEditFields()
	m.focusField = Message
	m.messageField.Focus()
	m.messageField.SetValue("subject")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = updated.(Model)
	if m.state != ViewEdit {
		t.Fatalf("state = %v, want ViewEdit", m.state)
	}
	if !strings.Contains(m.messageField.Value(), "\n") {
		t.Fatalf("message value = %q, want newline", m.messageField.Value())
	}
	if len(m.editQueue) != 0 {
		t.Fatalf("editQueue len = %d, want 0", len(m.editQueue))
	}
}

func TestDisplayCommitMessageStripsConflictSection(t *testing.T) {
	message := "subject\n\nbody\n# Conflicts:\n#\tfile.go\n"

	got := displayCommitMessage(message)
	if got != "subject\n\nbody" {
		t.Fatalf("displayCommitMessage = %q, want subject and body only", got)
	}
}

func TestDisplayCommitMessageStripsTerminalControls(t *testing.T) {
	message := "subject\x1b[2J\r\nbody"

	got := displayCommitMessage(message)
	if got != "subject\nbody" {
		t.Fatalf("displayCommitMessage = %q, want controls stripped", got)
	}
}

func TestRenderEditViewShowsCleanOriginalMessage(t *testing.T) {
	m := Model{
		keys:          defaultKeyMap(),
		editingCommit: &gitops.CommitInfo{ShortHash: "1111111", AuthorName: "Jan", AuthorEmail: "jan@example.com", AuthorDate: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Message: "subject\n# Conflics:\n#\tfile.go"},
	}
	m.initEditFields()

	view := m.renderEditView()
	if !strings.Contains(view, "subject") {
		t.Fatalf("view missing original subject: %q", view)
	}
	if strings.Contains(view, "Conflics") || strings.Contains(view, "file.go") {
		t.Fatalf("view includes conflict section: %q", view)
	}
}

func TestRemoveChangeRebuildsMap(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	model := Model{}

	model.setChange(gitops.ForgeChange{OriginalHash: first})
	model.setChange(gitops.ForgeChange{OriginalHash: second})
	model.removeChange(first.String())

	if len(model.editQueue) != 1 {
		t.Fatalf("editQueue len = %d, want 1", len(model.editQueue))
	}
	if model.editQueue[0].OriginalHash != second {
		t.Fatalf("remaining hash = %s, want %s", model.editQueue[0].OriginalHash, second)
	}
	if _, ok := model.editMap[first.String()]; ok {
		t.Fatalf("removed hash still exists in editMap")
	}
	if model.editMap[second.String()] != &model.editQueue[0] {
		t.Fatalf("remaining editMap pointer was not rebuilt")
	}
}

func TestUndoLastChangeRestoresPendingChangesStepByStep(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	model := Model{}

	model.applyWithUndo(func() {
		model.setChange(gitops.ForgeChange{OriginalHash: first})
	})
	model.applyWithUndo(func() {
		model.setChange(gitops.ForgeChange{OriginalHash: second, Operation: gitops.ForgeDrop})
	})

	if len(model.undoStack) != 2 {
		t.Fatalf("undoStack len = %d, want 2", len(model.undoStack))
	}
	if !model.undoLastChange() {
		t.Fatalf("undoLastChange returned false")
	}
	if len(model.editQueue) != 1 || model.editQueue[0].OriginalHash != first {
		t.Fatalf("editQueue after first undo = %+v, want only first", model.editQueue)
	}
	if model.editMap[first.String()] != &model.editQueue[0] {
		t.Fatalf("editMap pointer was not rebuilt after undo")
	}

	if !model.undoLastChange() {
		t.Fatalf("second undoLastChange returned false")
	}
	if len(model.editQueue) != 0 {
		t.Fatalf("editQueue len after second undo = %d, want 0", len(model.editQueue))
	}
	if model.undoLastChange() {
		t.Fatalf("undoLastChange returned true with empty stack")
	}
}

func TestApplyWithUndoSkipsNoOp(t *testing.T) {
	model := Model{}

	model.applyWithUndo(func() {})
	if len(model.undoStack) != 0 {
		t.Fatalf("undoStack len = %d, want 0", len(model.undoStack))
	}
}

func TestUndoSnapshotDeepCopiesChangeData(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	other := plumbing.NewHash("2222222222222222222222222222222222222222")
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	model := Model{}
	model.setChange(gitops.ForgeChange{
		OriginalHash: hash,
		NewAuthor:    &gitops.AuthorInfo{Name: "Original", Email: "original@example.com"},
		NewDate:      &date,
		CombineGroup: []plumbing.Hash{hash, other},
	})

	model.applyWithUndo(func() {
		model.setChange(gitops.ForgeChange{OriginalHash: hash, Operation: gitops.ForgeDrop})
	})
	model.editQueue[0].NewAuthor = &gitops.AuthorInfo{Name: "Mutated", Email: "mutated@example.com"}
	model.editQueue[0].NewDate = nil
	model.editQueue[0].CombineGroup = []plumbing.Hash{other}

	if !model.undoLastChange() {
		t.Fatalf("undoLastChange returned false")
	}
	change := model.editQueue[0]
	if change.NewAuthor == nil || change.NewAuthor.Name != "Original" {
		t.Fatalf("NewAuthor after undo = %+v, want Original", change.NewAuthor)
	}
	if change.NewDate == nil || !change.NewDate.Equal(date) {
		t.Fatalf("NewDate after undo = %v, want %v", change.NewDate, date)
	}
	if len(change.CombineGroup) != 2 || change.CombineGroup[0] != hash || change.CombineGroup[1] != other {
		t.Fatalf("CombineGroup after undo = %v, want original group", change.CombineGroup)
	}
}

func TestSelectedCommitInfosPreservesCommitOrder(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	third := plumbing.NewHash("3333333333333333333333333333333333333333")
	model := Model{
		commits: []gitops.CommitInfo{
			{Hash: first},
			{Hash: second},
			{Hash: third},
		},
		selectedCommits: map[string]bool{
			third.String(): true,
			first.String(): true,
		},
	}

	selected := model.selectedCommitInfos()
	if len(selected) != 2 {
		t.Fatalf("selected len = %d, want 2", len(selected))
	}
	if selected[0].Hash != first || selected[1].Hash != third {
		t.Fatalf("selected order = [%s %s], want [%s %s]", selected[0].Hash, selected[1].Hash, first, third)
	}
}

func TestToggleDropForCommitsMarksAndUnmarksBatch(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	commits := []gitops.CommitInfo{{Hash: first}, {Hash: second}}
	model := Model{}

	model.toggleDropForCommits(commits)
	if len(model.editQueue) != 2 {
		t.Fatalf("editQueue len after drop = %d, want 2", len(model.editQueue))
	}
	for _, commit := range commits {
		change := model.editMap[commit.Hash.String()]
		if change == nil || change.Operation != gitops.ForgeDrop {
			t.Fatalf("commit %s was not marked drop", commit.Hash)
		}
	}

	model.toggleDropForCommits(commits)
	if len(model.editQueue) != 0 {
		t.Fatalf("editQueue len after unmark = %d, want 0", len(model.editQueue))
	}
}

func TestToggleDropForCommitsReplacesMixedChanges(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	commits := []gitops.CommitInfo{{Hash: first}, {Hash: second}}
	model := Model{}
	model.setChange(gitops.ForgeChange{OriginalHash: first})

	model.toggleDropForCommits(commits)
	for _, commit := range commits {
		change := model.editMap[commit.Hash.String()]
		if change == nil || change.Operation != gitops.ForgeDrop {
			t.Fatalf("commit %s was not marked drop", commit.Hash)
		}
	}
}

func TestToggleCombineForCommitsMarksAndUnmarksBatch(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	commits := []gitops.CommitInfo{{Hash: first}, {Hash: second}}
	model := Model{}

	model.toggleCombineForCommits(commits, second)
	if len(model.editQueue) != 2 {
		t.Fatalf("editQueue len after fold = %d, want 2", len(model.editQueue))
	}
	for _, commit := range commits {
		change := model.editMap[commit.Hash.String()]
		if change == nil || change.Operation != gitops.ForgeCombine {
			t.Fatalf("commit %s was not marked fold", commit.Hash)
		}
		if len(change.CombineGroup) != 2 || change.CombineGroup[0] != first || change.CombineGroup[1] != second {
			t.Fatalf("combine group = %v, want [%s %s]", change.CombineGroup, first, second)
		}
		if change.CombineAnchor != second {
			t.Fatalf("combine anchor = %s, want %s", change.CombineAnchor, second)
		}
	}

	model.toggleCombineForCommits(commits, second)
	if len(model.editQueue) != 0 {
		t.Fatalf("editQueue len after unmark = %d, want 0", len(model.editQueue))
	}
}

func TestToggleCombineForCommitsRequiresMultipleCommits(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	model := Model{}

	model.toggleCombineForCommits([]gitops.CommitInfo{{Hash: hash}}, hash)
	if len(model.editQueue) != 0 {
		t.Fatalf("editQueue len = %d, want 0", len(model.editQueue))
	}
}

func TestToggleCombineForCommitsRequiresAnchorInGroup(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	third := plumbing.NewHash("3333333333333333333333333333333333333333")
	model := Model{}

	model.toggleCombineForCommits([]gitops.CommitInfo{{Hash: first}, {Hash: second}}, third)
	if len(model.editQueue) != 0 {
		t.Fatalf("editQueue len = %d, want 0", len(model.editQueue))
	}
}

func TestFoldDisplayIndexAssignsStableIDsByCommitOrder(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")
	third := plumbing.NewHash("3333333333333333333333333333333333333333")
	fourth := plumbing.NewHash("4444444444444444444444444444444444444444")
	model := Model{
		commits: []gitops.CommitInfo{
			{Hash: first},
			{Hash: second},
			{Hash: third},
			{Hash: fourth},
		},
	}
	model.toggleCombineForCommits([]gitops.CommitInfo{{Hash: third}, {Hash: fourth}}, third)
	model.toggleCombineForCommits([]gitops.CommitInfo{{Hash: first}, {Hash: second}}, first)

	folds := model.foldDisplayIndex()
	if len(folds.Groups) != 2 {
		t.Fatalf("groups len = %d, want 2", len(folds.Groups))
	}
	if folds.ByHash[first.String()].Label != "[fold 1] " {
		t.Fatalf("first label = %q, want [fold 1]", folds.ByHash[first.String()].Label)
	}
	if folds.ByHash[third.String()].Label != "[fold 2] " {
		t.Fatalf("third label = %q, want [fold 2]", folds.ByHash[third.String()].Label)
	}
	if folds.ByHash[first.String()].Color == folds.ByHash[third.String()].Color {
		t.Fatalf("fold colors should differ")
	}
}

func TestShortHashList(t *testing.T) {
	first := plumbing.NewHash("1111111111111111111111111111111111111111")
	second := plumbing.NewHash("2222222222222222222222222222222222222222")

	got := shortHashList([]plumbing.Hash{first, second})
	want := "1111111, 2222222"
	if got != want {
		t.Fatalf("shortHashList = %q, want %q", got, want)
	}
}

func TestRenderCleanCommitUsesSingleSpacing(t *testing.T) {
	commit := gitops.CommitInfo{
		Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:  "1111111",
		AuthorName: "Jan",
		AuthorDate: time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Additions:  8,
		Deletions:  12,
		Message:    "message",
	}

	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	line := renderCleanCommit(commit, nil, foldDisplay{}, 120, false, "", 0, false, false, true)
	if !strings.Contains(line, "1111111 Jan 2024-01-02 03:04 +8 -12 message") {
		t.Fatalf("clean line has unexpected spacing: %q", line)
	}
	if strings.Contains(line, "   +8") {
		t.Fatalf("clean line contains padded stats: %q", line)
	}
}

func TestRenderCleanCommitCanShowEmail(t *testing.T) {
	commit := gitops.CommitInfo{
		Hash:        plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:   "1111111",
		AuthorName:  "Jan",
		AuthorEmail: "jan@example.com",
		AuthorDate:  time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Message:     "message",
	}

	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	line := renderCleanCommit(commit, nil, foldDisplay{}, 120, false, "", 0, false, true, true)
	if !strings.Contains(line, "Jan <jan@example.com>") {
		t.Fatalf("clean line does not include email: %q", line)
	}
}

func TestRenderCleanCommitCanHideLineDiffs(t *testing.T) {
	commit := gitops.CommitInfo{
		Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:  "1111111",
		AuthorName: "Jan",
		AuthorDate: time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Additions:  8,
		Deletions:  12,
		Message:    "message",
	}

	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	line := renderCleanCommit(commit, nil, foldDisplay{}, 120, false, "", 0, false, false, false)
	if strings.Contains(line, "+8") || strings.Contains(line, "-12") {
		t.Fatalf("clean line should not include line diffs: %q", line)
	}
	if !strings.Contains(line, "1111111 Jan 2024-01-02 03:04 message") {
		t.Fatalf("clean line has unexpected spacing: %q", line)
	}
}

func TestRenderCommitSuffixShowsEditedTimeAndMessage(t *testing.T) {
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	change := &gitops.ForgeChange{
		NewDate:    &date,
		NewMessage: "tampered message",
	}

	suffix, width := renderCommitSuffix(change, false, false, "237")
	if !strings.Contains(suffix, "[time]") {
		t.Fatalf("suffix missing time indicator: %q", suffix)
	}
	if !strings.Contains(suffix, "[msg]") {
		t.Fatalf("suffix missing message indicator: %q", suffix)
	}
	if width != len(" [time] [msg]") {
		t.Fatalf("width = %d, want %d", width, len(" [time] [msg]"))
	}
}

func TestHandleListKeyRefreshClearsState(t *testing.T) {
	dir := t.TempDir()

	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "first"},
		{"commit", "--allow-empty", "-m", "second"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	repo, err := gitops.Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	m := NewModel(repo)
	if len(m.commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(m.commits))
	}

	// Add some pending edits
	hash := m.commits[0].Hash.String()
	m.editQueue = []gitops.ForgeChange{{OriginalHash: m.commits[0].Hash, NewMessage: "edited"}}
	m.editMap = map[string]*gitops.ForgeChange{hash: &m.editQueue[0]}
	m.undoStack = [][]gitops.ForgeChange{{{OriginalHash: m.commits[0].Hash}}}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)

	if len(m.editQueue) != 0 {
		t.Fatalf("editQueue len after refresh = %d, want 0", len(m.editQueue))
	}
	if len(m.editMap) != 0 {
		t.Fatalf("editMap len after refresh = %d, want 0", len(m.editMap))
	}
	if m.undoStack != nil {
		t.Fatalf("undoStack after refresh = %v, want nil", m.undoStack)
	}
	if len(m.list.Items()) != 2 {
		t.Fatalf("list items after refresh = %d, want 2", len(m.list.Items()))
	}
}

func TestHandleListKeyResetRemovesChange(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	commits := []gitops.CommitInfo{{Hash: hash}}
	items := make([]list.Item, 1)
	items[0] = commitItem{commit: commits[0]}

	m := Model{
		commits: commits,
		list:    list.New(items, commitDelegate{}, 80, 20),
		keys:    defaultKeyMap(),
		options: Options{PlainView: true},
	}

	m.setChange(gitops.ForgeChange{OriginalHash: hash})
	if len(m.editQueue) != 1 {
		t.Fatalf("editQueue len before reset = %d, want 1", len(m.editQueue))
	}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = updated.(Model)

	if len(m.editQueue) != 0 {
		t.Fatalf("editQueue len after reset = %d, want 0", len(m.editQueue))
	}
	if _, ok := m.editMap[hash.String()]; ok {
		t.Fatalf("editMap still contains removed hash")
	}
}

func TestShortenMessageForCommitsKeepsFirstLineOnly(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	commits := []gitops.CommitInfo{{Hash: hash, Message: "subject\n\n- detail one\n- detail two"}}
	model := Model{}

	model.shortenMessageForCommits(commits)

	if len(model.editQueue) != 1 {
		t.Fatalf("editQueue len = %d, want 1", len(model.editQueue))
	}
	change := model.editMap[hash.String()]
	if change == nil {
		t.Fatalf("editMap missing entry for hash")
	}
	if change.NewMessage != "subject" {
		t.Fatalf("NewMessage = %q, want %q", change.NewMessage, "subject")
	}
}

func TestShortenMessageForCommitsSkipsSingleLine(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	commits := []gitops.CommitInfo{{Hash: hash, Message: "only subject"}}
	model := Model{}

	model.shortenMessageForCommits(commits)

	if len(model.editQueue) != 0 {
		t.Fatalf("editQueue len = %d, want 0 (single-line message should be skipped)", len(model.editQueue))
	}
}

func TestShortenMessageForCommitsPreservesOtherChanges(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	commits := []gitops.CommitInfo{{Hash: hash, Message: "subject\n- detail"}}
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	model := Model{}
	model.setChange(gitops.ForgeChange{
		OriginalHash: hash,
		NewAuthor:    &gitops.AuthorInfo{Name: "Jan", Email: "jan@example.com"},
		NewDate:      &date,
	})

	model.shortenMessageForCommits(commits)

	if len(model.editQueue) != 1 {
		t.Fatalf("editQueue len = %d, want 1", len(model.editQueue))
	}
	change := model.editMap[hash.String()]
	if change.NewMessage != "subject" {
		t.Fatalf("NewMessage = %q, want %q", change.NewMessage, "subject")
	}
	if change.NewAuthor == nil || change.NewAuthor.Name != "Jan" {
		t.Fatalf("NewAuthor not preserved: %+v", change.NewAuthor)
	}
	if change.NewDate == nil || !change.NewDate.Equal(date) {
		t.Fatalf("NewDate not preserved: %v", change.NewDate)
	}
}

func TestShortenMessageForCommitsSkipsDroppedAndCombined(t *testing.T) {
	dropHash := plumbing.NewHash("1111111111111111111111111111111111111111")
	combineHash := plumbing.NewHash("2222222222222222222222222222222222222222")
	commits := []gitops.CommitInfo{
		{Hash: dropHash, Message: "drop subject\n- detail"},
		{Hash: combineHash, Message: "combine subject\n- detail"},
	}
	model := Model{}
	model.setChange(gitops.ForgeChange{OriginalHash: dropHash, Operation: gitops.ForgeDrop})
	model.setChange(gitops.ForgeChange{OriginalHash: combineHash, Operation: gitops.ForgeCombine, CombineGroup: []plumbing.Hash{combineHash}, CombineAnchor: combineHash})

	model.shortenMessageForCommits(commits)

	for _, change := range model.editQueue {
		if change.NewMessage != "" {
			t.Fatalf("shorten should not modify drop/combine entries, got NewMessage = %q", change.NewMessage)
		}
	}
}

func TestMultilineMessagePrefix(t *testing.T) {
	if got := multilineMessagePrefix("single line"); got != "" {
		t.Fatalf("prefix for single line = %q, want empty", got)
	}
	if got := multilineMessagePrefix("subject\nbody"); got != "[2] " {
		t.Fatalf("prefix for two lines = %q, want [2] ", got)
	}
	if got := multilineMessagePrefix("subject\n\n- a\n- b\n- c"); got != "[5] " {
		t.Fatalf("prefix for five lines = %q, want [5] ", got)
	}
}

func TestHandleListKeyShortenUsesUppercaseX(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	commits := []gitops.CommitInfo{{Hash: hash, Message: "subject\n- detail"}}
	items := make([]list.Item, 1)
	items[0] = commitItem{commit: commits[0]}

	m := Model{
		commits: commits,
		list:    list.New(items, commitDelegate{}, 80, 20),
		keys:    defaultKeyMap(),
		options: Options{PlainView: true},
	}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	m = updated.(Model)

	if len(m.editQueue) != 1 {
		t.Fatalf("editQueue len = %d, want 1", len(m.editQueue))
	}
	if m.editQueue[0].NewMessage != "subject" {
		t.Fatalf("NewMessage = %q, want subject", m.editQueue[0].NewMessage)
	}

	m = Model{
		commits: commits,
		list:    list.New(items, commitDelegate{}, 80, 20),
		keys:    defaultKeyMap(),
		options: Options{PlainView: true},
	}
	updated, _ = m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(Model)
	if len(m.editQueue) != 0 {
		t.Fatalf("editQueue len after lowercase x = %d, want 0", len(m.editQueue))
	}
}

func TestRenderCleanCommitShowsMultilinePrefix(t *testing.T) {
	commit := gitops.CommitInfo{
		Hash:       plumbing.NewHash("1111111111111111111111111111111111111111"),
		ShortHash:  "1111111",
		AuthorName: "Jan",
		AuthorDate: time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
		Message:    "subject\n\n- detail",
	}

	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	time.Local = time.UTC

	line := renderCleanCommit(commit, nil, foldDisplay{}, 120, false, "", 0, false, false, true)
	if !strings.Contains(line, "[3] ") {
		t.Fatalf("clean line missing multiline prefix: %q", line)
	}
	if !strings.Contains(line, "[3] subject") {
		t.Fatalf("clean line should render prefix before subject: %q", line)
	}
}
