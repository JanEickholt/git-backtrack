package tui

import (
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

func TestHandleListKeySettingsOpensSettings(t *testing.T) {
	m := Model{keys: defaultKeyMap()}

	updated, _ := m.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if m.state != ViewSettings {
		t.Fatalf("state = %v, want ViewSettings", m.state)
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

func TestHandleSettingsKeyClosesSettings(t *testing.T) {
	m := Model{state: ViewSettings, keys: defaultKeyMap()}

	updated, _ := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.state != ViewList {
		t.Fatalf("state = %v, want ViewList", m.state)
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
