package tui

import (
	"strings"
	"testing"
	"time"

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

	line := renderCleanCommit(commit, nil, foldDisplay{}, 120, false, "", 0)
	if !strings.Contains(line, "1111111 Jan 2024-01-02 03:04 +0000 +8 -12 message") {
		t.Fatalf("clean line has unexpected spacing: %q", line)
	}
	if strings.Contains(line, "   +8") {
		t.Fatalf("clean line contains padded stats: %q", line)
	}
}
