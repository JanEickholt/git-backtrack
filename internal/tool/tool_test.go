package tool

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jan/git-backtrack/internal/gitops"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestRunListOutputsJSON(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "hello\n", "hello")

	var stdout bytes.Buffer
	status := Run([]string{"list", "--path", dir, "--json"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}

	var response ListResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not ok: %+v", response)
	}
	if response.Ref != "refs/heads/main" || response.Branch != "main" {
		t.Fatalf("ref = %q branch = %q, want main branch", response.Ref, response.Branch)
	}
	if len(response.Commits) != 1 || response.Commits[0].Subject != "hello" {
		t.Fatalf("commits = %+v, want hello commit", response.Commits)
	}
}

func TestRunHelpOutputsToolContract(t *testing.T) {
	var stdout bytes.Buffer
	status := Run([]string{"help", "--json"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}

	var response HelpResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not ok: %+v", response)
	}
	for _, command := range []string{"help", "list", "validate", "apply"} {
		if !hasCommandHelp(response.Commands, command) {
			t.Fatalf("help missing command %q: %+v", command, response.Commands)
		}
	}
	if response.HashRules.MinimumPrefixLength != minHashPrefixLength {
		t.Fatalf("minimum prefix length = %d, want %d", response.HashRules.MinimumPrefixLength, minHashPrefixLength)
	}
	if len(response.ExamplePlan.Operations) == 0 {
		t.Fatalf("help missing example plan operations")
	}
}

func TestValidateResolvesUnambiguousShortHash(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "one\n", "one")
	firstHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFile(t, dir, "file.txt", "two\n", "two")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations:   []PlanOperation{{Op: "edit", Hash: firstHash[:7], Message: stringPtr("changed one")}},
	})

	var stdout bytes.Buffer
	status := Run([]string{"validate", "--path", dir, "--plan", planPath, "--json"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}

	var response ValidateResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not ok: %+v", response)
	}
	if len(response.ResolvedOperations) != 1 || response.ResolvedOperations[0].Hash != firstHash {
		t.Fatalf("resolved operations = %+v, want full hash %s", response.ResolvedOperations, firstHash)
	}
}

func TestHashResolverRejectsAmbiguousPrefix(t *testing.T) {
	resolver := newHashResolver([]gitops.CommitInfo{
		{Hash: plumbing.NewHash("abcdef0111111111111111111111111111111111")},
		{Hash: plumbing.NewHash("abcdef0222222222222222222222222222222222")},
	})

	_, errors := resolver.resolve("abcdef0")
	if len(errors) != 1 || errors[0].Code != "ambiguous_hash" {
		t.Fatalf("errors = %+v, want ambiguous_hash", errors)
	}
	if len(errors[0].Matches) != 2 {
		t.Fatalf("matches = %+v, want 2 matches", errors[0].Matches)
	}
}

func TestApplyRequiresYes(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "hello\n", "hello")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations:   []PlanOperation{{Op: "edit", Hash: headHash, Message: stringPtr("changed")}},
	})

	var stdout bytes.Buffer
	status := Run([]string{"apply", "--path", dir, "--plan", planPath, "--json"}, &stdout, &bytes.Buffer{})
	if status == 0 {
		t.Fatalf("apply without --yes succeeded: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "confirmation_required") {
		t.Fatalf("output missing confirmation_required: %s", stdout.String())
	}
}

func TestApplyEditsCommitMessage(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "hello\n", "hello")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations:   []PlanOperation{{Op: "edit", Hash: headHash, Message: stringPtr("changed")}},
	})

	var stdout bytes.Buffer
	status := Run([]string{"apply", "--path", dir, "--plan", planPath, "--json", "--yes"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}

	message := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%B"))
	if message != "changed" {
		t.Fatalf("message = %q, want changed", message)
	}
	var response ApplyResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.BackupRef == "" || len(response.ChangedRefs) != 1 {
		t.Fatalf("response = %+v, want backup and changed refs", response)
	}
}

func TestApplyNoOpPlanDoesNotCreateBackup(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "hello\n", "hello")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations:   []PlanOperation{{Op: "edit", Hash: headHash}},
	})

	var stdout bytes.Buffer
	status := Run([]string{"apply", "--path", dir, "--plan", planPath, "--json", "--yes"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}

	var response ApplyResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.BackupRef != "" || len(response.ChangedRefs) != 0 {
		t.Fatalf("response = %+v, want ok no-op without backup", response)
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

func commitFile(t *testing.T, dir string, name string, content string, message string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitOutput(t, dir, "add", name)
	cmd := exec.Command("git", "-C", dir, "commit", "-m", message)
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_COMMITTER_DATE="+date,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", strings.TrimSpace(string(output)), err)
	}
}

func writePlan(t *testing.T, dir string, plan Plan) string {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	return path
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

func stringPtr(value string) *string {
	return &value
}

func hasCommandHelp(commands []CommandHelp, name string) bool {
	for _, command := range commands {
		if command.Name == name {
			return true
		}
	}
	return false
}
