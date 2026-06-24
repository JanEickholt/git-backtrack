package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Jan/git-backtrack/internal/gitops"
)

func TestAuthorCompletionCandidatesUseRecentUniqueAuthors(t *testing.T) {
	commits := []gitops.CommitInfo{
		{AuthorName: "Jane Doe", AuthorEmail: "jane@example.com"},
		{AuthorName: "Jan Eickholt", AuthorEmail: "jan@example.com"},
		{AuthorName: "Jane Doe", AuthorEmail: "jane@work.example"},
		{AuthorName: "Alice", AuthorEmail: "alice@example.com"},
	}

	got := authorCompletionCandidates(commits, authorCompletionName, "ja", 5)
	want := []string{"Jane Doe", "Jan Eickholt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("name candidates = %#v, want %#v", got, want)
	}

	got = authorCompletionCandidates(commits, authorCompletionEmail, "work", 5)
	want = []string{"jane@work.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("email candidates = %#v, want %#v", got, want)
	}
}

func TestHandleEditKeyAcceptsAuthorCompletion(t *testing.T) {
	m := Model{
		commits: []gitops.CommitInfo{
			{AuthorName: "Jane Doe", AuthorEmail: "jane@example.com"},
			{AuthorName: "Jan Eickholt", AuthorEmail: "jan@example.com"},
		},
		keys:        defaultKeyMap(),
		focusField:  FieldName,
		editFields:  make([]textinput.Model, 4),
		completeIdx: noCompletionIndex,
	}
	for i := range m.editFields {
		m.editFields[i] = textinput.New()
	}
	m.editFields[FieldName].SetValue("ja")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = updated.(Model)
	updated, _ = m.handleEditKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = updated.(Model)
	updated, _ = m.handleEditKey(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = updated.(Model)

	if got := m.editFields[FieldName].Value(); got != "Jan Eickholt" {
		t.Fatalf("accepted completion = %q, want %q", got, "Jan Eickholt")
	}
}

func TestHandleBatchEditKeyAcceptsEmailCompletion(t *testing.T) {
	m := Model{
		commits: []gitops.CommitInfo{
			{AuthorName: "Jane Doe", AuthorEmail: "jane@example.com"},
			{AuthorName: "Jan Eickholt", AuthorEmail: "jan@example.com"},
		},
		keys:        defaultKeyMap(),
		batchFocus:  1,
		batchFields: make([]textinput.Model, 5),
	}
	for i := range m.batchFields {
		m.batchFields[i] = textinput.New()
	}
	m.batchFields[1].SetValue("jan@")

	updated, _ := m.handleBatchEditKey(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = updated.(Model)

	if got := m.batchFields[1].Value(); got != "jan@example.com" {
		t.Fatalf("accepted batch completion = %q, want %q", got, "jan@example.com")
	}
}

func TestHandleEditKeyEnterAcceptsSelectedCompletion(t *testing.T) {
	m := Model{
		state: ViewEdit,
		commits: []gitops.CommitInfo{
			{AuthorName: "Jane Doe", AuthorEmail: "jane@example.com"},
			{AuthorName: "Jan Eickholt", AuthorEmail: "jan@example.com"},
		},
		keys:        defaultKeyMap(),
		focusField:  FieldName,
		editFields:  make([]textinput.Model, 4),
		completeIdx: noCompletionIndex,
	}
	for i := range m.editFields {
		m.editFields[i] = textinput.New()
	}
	m.editFields[FieldName].SetValue("ja")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	updated, _ = m.handleEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.editFields[FieldName].Value(); got != "Jane Doe" {
		t.Fatalf("accepted completion = %q, want %q", got, "Jane Doe")
	}
	if m.state != ViewEdit {
		t.Fatalf("state = %v, want ViewEdit", m.state)
	}
}

func TestHandleEditKeyEnterSavesWhenNoCompletionSelected(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	m := Model{
		state:           ViewEdit,
		keys:            defaultKeyMap(),
		editingCommit:   &gitops.CommitInfo{Hash: hash, AuthorName: "Jan", AuthorEmail: "jan@example.com", AuthorDate: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Message: "old"},
		editMap:         make(map[string]*gitops.ForgeChange),
		selectedCommits: make(map[string]bool),
		focusField:      FieldName,
		editFields:      make([]textinput.Model, 4),
		completeIdx:     noCompletionIndex,
	}
	for i := range m.editFields {
		m.editFields[i] = textinput.New()
	}
	m.editFields[FieldName].SetValue("new author")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.state != ViewList {
		t.Fatalf("state = %v, want ViewList", m.state)
	}
	if len(m.editQueue) != 1 || m.editQueue[0].NewAuthor == nil || m.editQueue[0].NewAuthor.Name != "new author" {
		t.Fatalf("editQueue = %+v, want author change", m.editQueue)
	}
}

func TestHandleEditKeyLeftRightNavigatesCompletions(t *testing.T) {
	m := Model{
		commits: []gitops.CommitInfo{
			{AuthorName: "Jane Doe", AuthorEmail: "jane@example.com"},
			{AuthorName: "Jan Eickholt", AuthorEmail: "jan@example.com"},
			{AuthorName: "Jack", AuthorEmail: "jack@example.com"},
		},
		keys:        defaultKeyMap(),
		focusField:  FieldName,
		editFields:  make([]textinput.Model, 4),
		completeIdx: noCompletionIndex,
	}
	for i := range m.editFields {
		m.editFields[i] = textinput.New()
	}
	m.editFields[FieldName].SetValue("ja")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.completeIdx != 0 {
		t.Fatalf("completeIdx after right = %d, want 0", m.completeIdx)
	}
	updated, _ = m.handleEditKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.completeIdx != 1 {
		t.Fatalf("completeIdx after second right = %d, want 1", m.completeIdx)
	}
	updated, _ = m.handleEditKey(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.completeIdx != 0 {
		t.Fatalf("completeIdx after left = %d, want 0", m.completeIdx)
	}
	updated, _ = m.handleEditKey(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.completeIdx != 2 {
		t.Fatalf("completeIdx after wrap left = %d, want 2", m.completeIdx)
	}
	_ = updated
}

func TestHandleEditKeyShiftEnterNextField(t *testing.T) {
	m := Model{
		keys:        defaultKeyMap(),
		focusField:  FieldName,
		editFields:  make([]textinput.Model, 4),
		completeIdx: noCompletionIndex,
	}
	for i := range m.editFields {
		m.editFields[i] = textinput.New()
	}
	m.editFields[FieldName].Focus()

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shift+enter")})
	m = updated.(Model)

	if m.focusField != Email {
		t.Fatalf("focusField = %v, want Email", m.focusField)
	}
}

func TestHandleBatchEditKeyShiftEnterNextField(t *testing.T) {
	m := Model{
		keys:        defaultKeyMap(),
		batchFocus:  0,
		batchFields: make([]textinput.Model, 5),
	}
	for i := range m.batchFields {
		m.batchFields[i] = textinput.New()
	}
	m.batchFields[0].Focus()

	updated, _ := m.handleBatchEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shift+enter")})
	m = updated.(Model)

	if m.batchFocus != 1 {
		t.Fatalf("batchFocus = %d, want 1", m.batchFocus)
	}
}

func TestHandleBatchEditKeyEnterAcceptsSelectedCompletion(t *testing.T) {
	m := Model{
		commits: []gitops.CommitInfo{
			{AuthorName: "Jane Doe", AuthorEmail: "jane@example.com"},
			{AuthorName: "Jan Eickholt", AuthorEmail: "jan@example.com"},
		},
		keys:        defaultKeyMap(),
		batchFocus:  1,
		batchFields: make([]textinput.Model, 5),
		completeIdx: noCompletionIndex,
	}
	for i := range m.batchFields {
		m.batchFields[i] = textinput.New()
	}
	m.batchFields[1].SetValue("jan@")

	updated, _ := m.handleBatchEditKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	updated, _ = m.handleBatchEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.batchFields[1].Value(); got != "jan@example.com" {
		t.Fatalf("accepted batch completion = %q, want %q", got, "jan@example.com")
	}
}
