package tui

import (
	"testing"

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
