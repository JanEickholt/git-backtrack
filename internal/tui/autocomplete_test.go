package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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
		keys:       defaultKeyMap(),
		focusField: FieldName,
		editFields: make([]textinput.Model, 4),
	}
	for i := range m.editFields {
		m.editFields[i] = textinput.New()
	}
	m.editFields[FieldName].SetValue("ja")

	updated, _ := m.handleEditKey(tea.KeyMsg{Type: tea.KeyCtrlN})
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
