package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestRunListAcceptsPositionalLimitBeforeFlags(t *testing.T) {
	dir := initGitRepo(t)
	for i := 0; i < 7; i++ {
		commitFile(t, dir, "file.txt", strings.Repeat("x", i+1)+"\n", fmt.Sprintf("commit %d", i))
	}

	var stdout bytes.Buffer
	status := Run([]string{"list", "--path", dir, "--json", "2", "--compact"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}
	if strings.Contains(stdout.String(), "\n  ") {
		t.Fatalf("output was not compact: %q", stdout.String())
	}

	var response ListResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || len(response.Commits) != 2 || response.Total != 7 || response.Limit != 2 || response.Remaining != 5 || !response.Truncated {
		t.Fatalf("response = %+v, want compact positional limit window", response)
	}
}

func TestRunListFiltersByDateAuthorAndEmail(t *testing.T) {
	dir := initGitRepo(t)
	commitFileWithIdentityDate(t, dir, "file.txt", "one\n", "alice january", "Alice", "alice@example.com", "2024-01-05T12:00:00Z")
	commitFileWithIdentityDate(t, dir, "file.txt", "two\n", "bob february", "Bob", "bob@example.com", "2024-02-10T12:00:00Z")
	commitFileWithIdentityDate(t, dir, "file.txt", "three\n", "alice february", "Alice", "alice@example.com", "2024-02-11T12:00:00Z")

	dateResponse := runListResponse(t, dir, "--date", "2024-02-10", "--all")
	if dateResponse.Total != 1 || len(dateResponse.Commits) != 1 || dateResponse.Commits[0].Subject != "bob february" {
		t.Fatalf("date response = %+v, want bob february", dateResponse)
	}

	authorResponse := runListResponse(t, dir, "--date-range", "2024-02-01..2024-02-28", "--author", "Alice", "--all")
	if authorResponse.Total != 1 || len(authorResponse.Commits) != 1 || authorResponse.Commits[0].Subject != "alice february" {
		t.Fatalf("author range response = %+v, want alice february", authorResponse)
	}

	emailResponse := runListResponse(t, dir, "--mail", "bob@example.com", "--all")
	if emailResponse.Total != 1 || len(emailResponse.Commits) != 1 || emailResponse.Commits[0].AuthorEmail != "bob@example.com" {
		t.Fatalf("email response = %+v, want bob@example.com", emailResponse)
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

func runListResponse(t *testing.T, dir string, args ...string) ListResponse {
	t.Helper()
	listArgs := append([]string{"list", "--path", dir, "--json"}, args...)
	var stdout bytes.Buffer
	status := Run(listArgs, &stdout, &bytes.Buffer{})
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
	return response
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

func TestHelpCompactEmitsSingleLineJSON(t *testing.T) {
	var stdout bytes.Buffer
	status := Run([]string{"help", "--json", "--compact"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}
	raw := stdout.String()
	if !strings.HasSuffix(raw, "\n") {
		t.Fatalf("compact output must end with newline, got %q", raw)
	}
	line := strings.TrimSuffix(raw, "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("compact output must be single line, got:\n%s", raw)
	}
	if strings.Contains(line, "\n  ") {
		t.Fatalf("compact output must not contain 2-space indent, got:\n%s", raw)
	}
	var response HelpResponse
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatalf("unmarshal compact response: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not ok: %+v", response)
	}
}

func TestHelpIndentedByDefault(t *testing.T) {
	var stdout bytes.Buffer
	status := Run([]string{"help", "--json"}, &stdout, &bytes.Buffer{})
	if status != 0 {
		t.Fatalf("status = %d, output = %s", status, stdout.String())
	}
	raw := stdout.String()
	if !strings.Contains(raw, "\n  ") {
		t.Fatalf("default help output should contain 2-space indentation, got:\n%s", raw)
	}
}

func TestBackupsListsAndRestoreRequiresYes(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "hello\n", "hello")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations:   []PlanOperation{{Op: "edit", Hash: headHash, Message: stringPtr("changed")}},
	})

	var applyStdout bytes.Buffer
	if status := Run([]string{"apply", "--path", dir, "--plan", planPath, "--json", "--yes"}, &applyStdout, &bytes.Buffer{}); status != 0 {
		t.Fatalf("apply status = %d, output = %s", status, applyStdout.String())
	}
	var applyResp ApplyResponse
	if err := json.Unmarshal(applyStdout.Bytes(), &applyResp); err != nil {
		t.Fatalf("unmarshal apply response: %v", err)
	}
	if !applyResp.OK || applyResp.BackupRef == "" {
		t.Fatalf("apply did not produce backup: %+v", applyResp)
	}

	var backupsStdout bytes.Buffer
	if status := Run([]string{"backups", "--path", dir, "--json"}, &backupsStdout, &bytes.Buffer{}); status != 0 {
		t.Fatalf("backups status = %d, output = %s", status, backupsStdout.String())
	}
	var backupsResp BackupsResponse
	if err := json.Unmarshal(backupsStdout.Bytes(), &backupsResp); err != nil {
		t.Fatalf("unmarshal backups response: %v", err)
	}
	if !backupsResp.OK || len(backupsResp.Backups) == 0 {
		t.Fatalf("backups response empty: %+v", backupsResp)
	}
	backupName := backupsResp.Backups[len(backupsResp.Backups)-1].Name

	var restoreNoYesStdout bytes.Buffer
	restoreStatus := Run([]string{"restore", "--path", dir, "--json", "--backup", backupName}, &restoreNoYesStdout, &bytes.Buffer{})
	if restoreStatus == 0 {
		t.Fatalf("restore without --yes succeeded: %s", restoreNoYesStdout.String())
	}
	if !strings.Contains(restoreNoYesStdout.String(), "confirmation_required") {
		t.Fatalf("restore missing --yes should return confirmation_required: %s", restoreNoYesStdout.String())
	}

	var restoreBogusStdout bytes.Buffer
	bogusStatus := Run([]string{"restore", "--path", dir, "--json", "--yes", "--backup", "99999999-000000"}, &restoreBogusStdout, &bytes.Buffer{})
	if bogusStatus == 0 {
		t.Fatalf("restore with bogus backup succeeded: %s", restoreBogusStdout.String())
	}
	if !strings.Contains(restoreBogusStdout.String(), "backup_not_found") {
		t.Fatalf("restore with bogus backup should return backup_not_found: %s", restoreBogusStdout.String())
	}

	var restoreStdout bytes.Buffer
	if status := Run([]string{"restore", "--path", dir, "--json", "--yes", "--backup", backupName}, &restoreStdout, &bytes.Buffer{}); status != 0 {
		t.Fatalf("restore status = %d, output = %s", status, restoreStdout.String())
	}
	var restoreResp RestoreResponse
	if err := json.Unmarshal(restoreStdout.Bytes(), &restoreResp); err != nil {
		t.Fatalf("unmarshal restore response: %v", err)
	}
	if !restoreResp.OK {
		t.Fatalf("restore not ok: %+v", restoreResp)
	}
	if len(restoreResp.RestoredRefs) == 0 {
		t.Fatalf("restore produced no restored refs: %+v", restoreResp)
	}
}

func TestValidateWarnsOnDateAfterChild(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.txt", "a\n", "a")
	commitFileDated(t, dir, "b.txt", "b\n", "b", "2024-01-02T00:00:00Z")
	bHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFileDated(t, dir, "c.txt", "c\n", "c", "2024-01-03T00:00:00Z")
	cHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: cHash,
		Operations: []PlanOperation{
			{Op: "edit", Hash: bHash, AuthorDate: stringPtr("2024-01-10T00:00:00Z")},
		},
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
	found := false
	for _, warning := range response.Warnings {
		if warning.Code == "date_after_child" && strings.ToLower(warning.Hash) == strings.ToLower(bHash) {
			found = true
		}
	}
	if !found {
		t.Fatalf("did not find date_after_child warning for %s in %+v", bHash, response.Warnings)
	}
}

func TestValidateWarnsOnDateBeforeParent(t *testing.T) {
	dir := initGitRepo(t)
	commitFileDated(t, dir, "a.txt", "a\n", "a", "2024-01-01T00:00:00Z")
	commitFileDated(t, dir, "b.txt", "b\n", "b", "2024-01-02T00:00:00Z")
	bHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFileDated(t, dir, "c.txt", "c\n", "c", "2024-01-03T00:00:00Z")
	cHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: cHash,
		Operations: []PlanOperation{
			{Op: "edit", Hash: bHash, AuthorDate: stringPtr("2023-01-01T00:00:00Z")},
		},
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
	found := false
	for _, warning := range response.Warnings {
		if warning.Code == "date_before_parent" && strings.ToLower(warning.Hash) == strings.ToLower(bHash) {
			found = true
		}
	}
	if !found {
		t.Fatalf("did not find date_before_parent warning for %s in %+v", bHash, response.Warnings)
	}
}

func TestIsCommandIncludesNewCommands(t *testing.T) {
	for _, name := range []string{"help", "list", "validate", "apply", "backups", "restore"} {
		if !IsCommand(name) {
			t.Fatalf("IsCommand(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"unknown", "mcp", ""} {
		if IsCommand(name) {
			t.Fatalf("IsCommand(%q) = true, want false", name)
		}
	}
}

func TestToolHelpExposesBackupsRestoreAndWarningCodes(t *testing.T) {
	response := toolHelp()
	if !hasCommandHelp(response.Commands, "backups") {
		t.Fatalf("help missing backups command: %+v", response.Commands)
	}
	if !hasCommandHelp(response.Commands, "restore") {
		t.Fatalf("help missing restore command: %+v", response.Commands)
	}
	if _, ok := response.ResponseShapes["backups"]; !ok {
		t.Fatalf("help missing backups response shape: %+v", response.ResponseShapes)
	}
	if _, ok := response.ResponseShapes["restore"]; !ok {
		t.Fatalf("help missing restore response shape: %+v", response.ResponseShapes)
	}
	for _, code := range []string{"date_before_parent", "date_after_child", "empty_edit"} {
		if !containsWarningCode(response.WarningCodes, code) {
			t.Fatalf("help missing warning code %q: %+v", code, response.WarningCodes)
		}
	}
	for _, code := range []string{"no_backups_found", "backup_not_found"} {
		if !containsErrorCode(response.ErrorCodes, code) {
			t.Fatalf("help missing error code %q: %+v", code, response.ErrorCodes)
		}
	}
	foundSmartAdjust := false
	for _, schema := range response.OperationSchemas {
		if schema.Op == "smart_adjust" {
			foundSmartAdjust = true
			break
		}
	}
	if !foundSmartAdjust {
		t.Fatalf("help missing smart_adjust schema: %+v", response.OperationSchemas)
	}
	found := false
	for _, step := range response.RecommendedSequence {
		if strings.Contains(step, "restore") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("recommended sequence missing restore step: %+v", response.RecommendedSequence)
	}
}

func TestApplySmartAdjustUpdatesSelectedAuthorDates(t *testing.T) {
	dir := initGitRepo(t)
	commitFileDated(t, dir, "a.txt", "a\n", "chore: base", "2024-01-01T00:00:00Z")
	oldestHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFileDated(t, dir, "b.txt", "b\n", "fix: middle", "2024-01-02T00:00:00Z")
	middleHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFileDated(t, dir, "c.txt", "c\n", "feat: newest", "2024-01-03T00:00:00Z")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	adjustment := "+6h"

	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations: []PlanOperation{
			{Op: "smart_adjust", Hashes: []string{headHash[:7], middleHash[:7], oldestHash[:7]}, Adjustment: &adjustment},
		},
	})

	var validateStdout bytes.Buffer
	if status := Run([]string{"validate", "--path", dir, "--plan", planPath, "--json"}, &validateStdout, &bytes.Buffer{}); status != 0 {
		t.Fatalf("validate status = %d, output = %s", status, validateStdout.String())
	}
	var validateResp ValidateResponse
	if err := json.Unmarshal(validateStdout.Bytes(), &validateResp); err != nil {
		t.Fatalf("unmarshal validate response: %v", err)
	}
	if !validateResp.OK || len(validateResp.ResolvedOperations) != 1 || validateResp.ResolvedOperations[0].Op != "smart_adjust" {
		t.Fatalf("validate response = %+v, want resolved smart_adjust", validateResp)
	}

	var applyStdout bytes.Buffer
	if status := Run([]string{"apply", "--path", dir, "--plan", planPath, "--json", "--yes"}, &applyStdout, &bytes.Buffer{}); status != 0 {
		t.Fatalf("apply status = %d, output = %s", status, applyStdout.String())
	}

	dates := logAuthorDatesBySubject(t, dir)
	if dates["chore: base"] != "2024-01-01T00:00:00Z" {
		t.Fatalf("oldest date = %q, want unchanged", dates["chore: base"])
	}
	if dates["feat: newest"] != "2024-01-03T06:00:00Z" {
		t.Fatalf("newest date = %q, want +6h", dates["feat: newest"])
	}
	middleDate, err := time.Parse(time.RFC3339, dates["fix: middle"])
	if err != nil {
		t.Fatalf("parse middle date: %v", err)
	}
	if !middleDate.After(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)) || !middleDate.Before(time.Date(2024, 1, 2, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("middle date = %s, want between original and +6h", middleDate.Format(time.RFC3339))
	}
}

func TestValidateSmartAdjustRejectsInvalidAdjustment(t *testing.T) {
	dir := initGitRepo(t)
	commitFileDated(t, dir, "a.txt", "a\n", "a", "2024-01-01T00:00:00Z")
	oldestHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	commitFileDated(t, dir, "b.txt", "b\n", "b", "2024-01-02T00:00:00Z")
	headHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	adjustment := "tomorrow"
	planPath := writePlan(t, dir, Plan{
		Version:      1,
		Ref:          "refs/heads/main",
		ExpectedHead: headHash,
		Operations:   []PlanOperation{{Op: "smart_adjust", Hashes: []string{headHash, oldestHash}, Adjustment: &adjustment}},
	})

	var stdout bytes.Buffer
	status := Run([]string{"validate", "--path", dir, "--plan", planPath, "--json"}, &stdout, &bytes.Buffer{})
	if status == 0 {
		t.Fatalf("validate with invalid adjustment succeeded: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "invalid_adjustment") {
		t.Fatalf("output missing invalid_adjustment: %s", stdout.String())
	}
}

func containsWarningCode(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func containsErrorCode(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func logAuthorDatesBySubject(t *testing.T, dir string) map[string]string {
	t.Helper()
	output := strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s%x00%aI"))
	dates := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 2 {
			t.Fatalf("unexpected log line %q", line)
		}
		dates[parts[0]] = parts[1]
	}
	return dates
}

func commitFileDated(t *testing.T, dir string, name string, content string, message string, date string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitOutput(t, dir, "add", name)
	cmd := exec.Command("git", "-C", dir, "commit", "-m", message)
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

func commitFileWithIdentityDate(t *testing.T, dir string, name string, content string, message string, authorName string, authorEmail string, date string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitOutput(t, dir, "add", name)
	cmd := exec.Command("git", "-C", dir, "commit", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME="+authorName,
		"GIT_COMMITTER_EMAIL="+authorEmail,
		"GIT_COMMITTER_DATE="+date,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", strings.TrimSpace(string(output)), err)
	}
}
