package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestApplyChangesDropsMiddleCommit(t *testing.T) {
	dir := initGitRepo(t)

	commitFile(t, dir, "base.txt", "base\n", "base")
	commitFile(t, dir, "drop.txt", "drop\n", "bad")
	badHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFile(t, dir, "keep.txt", "keep\n", "good")

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	rewriter := NewHistoryRewriter(repo)

	result, err := rewriter.ApplyChanges([]ForgeChange{{
		OriginalHash: plumbing.NewHash(badHash),
		Operation:    ForgeDrop,
	}})
	if err != nil {
		t.Fatalf("apply changes: %v", err)
	}
	if len(result.ChangedRefs) != 2 {
		t.Fatalf("changed refs = %d, want 2", len(result.ChangedRefs))
	}

	log := gitOutput(t, dir, "log", "--format=%s")
	if strings.Contains(log, "bad") {
		t.Fatalf("dropped commit still appears in log:\n%s", log)
	}
	if !strings.Contains(log, "good") {
		t.Fatalf("descendant commit missing from log:\n%s", log)
	}

	if _, err := gitOutputErr(dir, "cat-file", "-e", "HEAD:drop.txt"); err == nil {
		t.Fatalf("dropped file is still present in HEAD")
	}
	if _, err := gitOutputErr(dir, "cat-file", "-e", "HEAD:keep.txt"); err != nil {
		t.Fatalf("kept descendant file missing from HEAD: %v", err)
	}
}

func TestApplyChangesDropConflictLeavesBranchUnchanged(t *testing.T) {
	dir := initGitRepo(t)

	commitFile(t, dir, "file.txt", "one\ntwo\nthree\n", "base")
	commitFile(t, dir, "file.txt", "ONE\ntwo\nthree\n", "first")
	commitFile(t, dir, "file.txt", "ONE\nTWO\nthree\n", "bad")
	badHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFile(t, dir, "file.txt", "ONE\nTWO\nTHREE\n", "good")
	originalHead := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	rewriter := NewHistoryRewriter(repo)

	_, err = rewriter.ApplyChanges([]ForgeChange{{
		OriginalHash: plumbing.NewHash(badHash),
		Operation:    ForgeDrop,
	}})
	if err == nil || !strings.Contains(err.Error(), "failed to replay commit") {
		t.Fatalf("expected replay conflict, got %v", err)
	}

	currentHead := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if currentHead != originalHead {
		t.Fatalf("HEAD changed after failed rewrite: got %s want %s", currentHead, originalHead)
	}
}

func TestApplyChangesDropRejectsRootCommit(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "base.txt", "base\n", "base")
	rootHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	rewriter := NewHistoryRewriter(repo)

	_, err = rewriter.ApplyChanges([]ForgeChange{{
		OriginalHash: plumbing.NewHash(rootHash),
		Operation:    ForgeDrop,
	}})
	if err == nil || !strings.Contains(err.Error(), "root commit") {
		t.Fatalf("expected root commit error, got %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := gitOutputErr(dir, "init", "-b", "main"); err != nil {
		gitOutput(t, dir, "init")
		gitOutput(t, dir, "checkout", "-b", "main")
	}
	return dir
}

func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitOutput(t, dir, "add", name)
	cmd := exec.Command("git", "-C", dir, "commit", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2024-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_COMMITTER_DATE=2024-01-01T00:00:00Z",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", strings.TrimSpace(string(output)), err)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := gitOutputErr(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return output
}

func gitOutputErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}
