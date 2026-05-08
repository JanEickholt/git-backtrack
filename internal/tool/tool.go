package tool

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Jan/git-backtrack/internal/gitops"
)

const planVersion = 1
const minHashPrefixLength = 7

type Plan struct {
	Version      int             `json:"version"`
	Ref          string          `json:"ref"`
	ExpectedHead string          `json:"expected_head"`
	Operations   []PlanOperation `json:"operations"`
}

type PlanOperation struct {
	Op          string   `json:"op"`
	Hash        string   `json:"hash,omitempty"`
	Hashes      []string `json:"hashes,omitempty"`
	Anchor      string   `json:"anchor,omitempty"`
	AuthorName  *string  `json:"author_name,omitempty"`
	AuthorEmail *string  `json:"author_email,omitempty"`
	AuthorDate  *string  `json:"author_date,omitempty"`
	Message     *string  `json:"message,omitempty"`
}

type Error struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Hash    string   `json:"hash,omitempty"`
	Matches []string `json:"matches,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hash    string `json:"hash,omitempty"`
}

type Commit struct {
	Hash        string   `json:"hash"`
	ShortHash   string   `json:"short_hash"`
	Parents     []string `json:"parents"`
	AuthorName  string   `json:"author_name"`
	AuthorEmail string   `json:"author_email"`
	AuthorDate  string   `json:"author_date"`
	Subject     string   `json:"subject"`
	Message     string   `json:"message"`
	Additions   int      `json:"additions"`
	Deletions   int      `json:"deletions"`
}

type ListResponse struct {
	OK      bool     `json:"ok"`
	Ref     string   `json:"ref"`
	Head    string   `json:"head"`
	Branch  string   `json:"branch,omitempty"`
	Commits []Commit `json:"commits"`
	Errors  []Error  `json:"errors,omitempty"`
}

type ValidateResponse struct {
	OK                 bool            `json:"ok"`
	Ref                string          `json:"ref,omitempty"`
	Head               string          `json:"head,omitempty"`
	ResolvedOperations []PlanOperation `json:"resolved_operations,omitempty"`
	Warnings           []Warning       `json:"warnings,omitempty"`
	Errors             []Error         `json:"errors,omitempty"`
}

type ApplyResponse struct {
	OK          bool              `json:"ok"`
	Ref         string            `json:"ref,omitempty"`
	Head        string            `json:"head,omitempty"`
	BackupRef   string            `json:"backup_ref,omitempty"`
	ChangedRefs map[string]string `json:"changed_refs,omitempty"`
	Warnings    []Warning         `json:"warnings,omitempty"`
	Errors      []Error           `json:"errors,omitempty"`
}

type validationResult struct {
	plan       Plan
	ref        string
	head       string
	commits    []gitops.CommitInfo
	resolved   []PlanOperation
	changes    []gitops.ForgeChange
	warnings   []Warning
	errors     []Error
	hasReplay  bool
	firstIndex int
}

func IsCommand(name string) bool {
	switch name {
	case "list", "validate", "apply":
		return true
	default:
		return false
	}
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing tool command")
		return 2
	}

	switch args[0] {
	case "list":
		return runList(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown tool command %q\n", args[0])
		return 2
	}
}

func runList(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	refArg := fs.String("ref", "", "ref or branch to list, defaults to HEAD")
	_ = fs.Bool("json", true, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	repo, err := gitops.Open(*repoPath)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Errors: []Error{errorObject("open_repository_failed", err.Error())}})
		return 1
	}
	ref, refHash, branch, err := resolveRef(repo, *refArg)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Errors: []Error{errorObject("resolve_ref_failed", err.Error())}})
		return 1
	}
	commits, err := commitsForRef(repo, ref)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Ref: ref, Head: refHash, Errors: []Error{errorObject("list_commits_failed", err.Error())}})
		return 1
	}

	writeJSON(stdout, ListResponse{OK: true, Ref: ref, Head: refHash, Branch: branch, Commits: jsonCommits(commits)})
	return 0
}

func runValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	planPath := fs.String("plan", "", "path to JSON plan")
	_ = fs.Bool("json", true, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	result, status := validatePlanFromPath(*repoPath, *planPath)
	writeJSON(stdout, ValidateResponse{OK: len(result.errors) == 0, Ref: result.ref, Head: result.head, ResolvedOperations: result.resolved, Warnings: result.warnings, Errors: result.errors})
	return status
}

func runApply(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	planPath := fs.String("plan", "", "path to JSON plan")
	yes := fs.Bool("yes", false, "apply the history rewrite")
	_ = fs.Bool("json", true, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	result, status := validatePlanFromPath(*repoPath, *planPath)
	if status != 0 {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: result.errors})
		return status
	}
	if !*yes {
		result.errors = append(result.errors, errorObject("confirmation_required", "apply requires --yes"))
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: result.errors})
		return 1
	}
	if len(result.changes) == 0 {
		writeJSON(stdout, ApplyResponse{OK: true, Ref: result.ref, Head: result.head, ChangedRefs: map[string]string{}, Warnings: result.warnings})
		return 0
	}

	repo, err := gitops.Open(*repoPath)
	if err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Errors: []Error{errorObject("open_repository_failed", err.Error())}})
		return 1
	}
	if err := ensureRefCheckedOut(repo, result.ref, result.head); err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: []Error{errorObject("ref_not_checked_out", err.Error())}})
		return 1
	}

	rewriter := gitops.NewHistoryRewriter(repo)
	backupRef, err := rewriter.CreateFullBackup()
	if err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: []Error{errorObject("backup_failed", err.Error())}})
		return 1
	}
	rewriteResult, err := rewriter.ApplyChanges(result.changes)
	if err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, BackupRef: backupRef, Warnings: result.warnings, Errors: []Error{errorObject("apply_failed", err.Error())}})
		return 1
	}

	writeJSON(stdout, ApplyResponse{OK: true, Ref: result.ref, Head: result.head, BackupRef: backupRef, ChangedRefs: changedRefsJSON(rewriteResult.ChangedRefs), Warnings: result.warnings})
	return 0
}

func validatePlanFromPath(repoPath string, planPath string) (validationResult, int) {
	var result validationResult
	if planPath == "" {
		result.errors = append(result.errors, errorObject("missing_plan", "--plan is required"))
		return result, 1
	}
	plan, err := readPlan(planPath)
	if err != nil {
		result.errors = append(result.errors, errorObject("read_plan_failed", err.Error()))
		return result, 1
	}
	repo, err := gitops.Open(repoPath)
	if err != nil {
		result.errors = append(result.errors, errorObject("open_repository_failed", err.Error()))
		return result, 1
	}
	result = validatePlan(repo, plan)
	if len(result.errors) > 0 {
		return result, 1
	}
	return result, 0
}

func validatePlan(repo *gitops.Repository, plan Plan) validationResult {
	result := validationResult{plan: plan, firstIndex: -1}
	if plan.Version != planVersion {
		result.errors = append(result.errors, errorObject("unsupported_plan_version", fmt.Sprintf("plan version must be %d", planVersion)))
	}
	if strings.TrimSpace(plan.Ref) == "" {
		result.errors = append(result.errors, errorObject("missing_ref", "plan ref is required"))
	}
	if strings.TrimSpace(plan.ExpectedHead) == "" {
		result.errors = append(result.errors, errorObject("missing_expected_head", "plan expected_head is required"))
	}
	if len(result.errors) > 0 {
		return result
	}

	ref, head, _, err := resolveRef(repo, plan.Ref)
	result.ref = ref
	result.head = head
	if err != nil {
		result.errors = append(result.errors, errorObject("resolve_ref_failed", err.Error()))
		return result
	}
	if !isFullHash(plan.ExpectedHead) || strings.ToLower(plan.ExpectedHead) != head {
		result.errors = append(result.errors, Error{Code: "expected_head_mismatch", Message: "ref moved since plan was created", Hash: plan.ExpectedHead})
	}

	commits, err := commitsForRef(repo, ref)
	if err != nil {
		result.errors = append(result.errors, errorObject("list_commits_failed", err.Error()))
		return result
	}
	result.commits = commits
	resolver := newHashResolver(commits)

	changes, resolved, warnings, errors := resolveOperations(plan.Operations, resolver)
	result.changes = changes
	result.resolved = resolved
	result.warnings = warnings
	result.errors = append(result.errors, errors...)
	if len(result.errors) > 0 {
		return result
	}

	result.errors = append(result.errors, validateReplayLimits(changes, commits)...)
	return result
}

func resolveOperations(operations []PlanOperation, resolver hashResolver) ([]gitops.ForgeChange, []PlanOperation, []Warning, []Error) {
	var changes []gitops.ForgeChange
	var resolved []PlanOperation
	var warnings []Warning
	var errorsOut []Error
	seen := make(map[string]string)

	for _, op := range operations {
		switch strings.ToLower(op.Op) {
		case "edit":
			hash, errs := resolver.resolve(op.Hash)
			errorsOut = append(errorsOut, errs...)
			if hash == "" {
				continue
			}
			if previous := seenOperation(seen, hash, "edit"); previous != "" {
				errorsOut = append(errorsOut, Error{Code: "duplicate_operation", Message: fmt.Sprintf("commit already has %s operation", previous), Hash: hash})
				continue
			}
			change, warning, err := editChange(hash, op)
			if err != nil {
				errorsOut = append(errorsOut, *err)
				continue
			}
			if warning != nil {
				warnings = append(warnings, *warning)
			}
			if change.HasChanges() {
				changes = append(changes, change)
			}
			resolved = append(resolved, resolvedEditOperation(hash, op))

		case "drop":
			hash, errs := resolver.resolve(op.Hash)
			errorsOut = append(errorsOut, errs...)
			if hash == "" {
				continue
			}
			if previous := seenOperation(seen, hash, "drop"); previous != "" {
				errorsOut = append(errorsOut, Error{Code: "duplicate_operation", Message: fmt.Sprintf("commit already has %s operation", previous), Hash: hash})
				continue
			}
			changes = append(changes, gitops.ForgeChange{OriginalHash: plumbing.NewHash(hash), Operation: gitops.ForgeDrop})
			resolved = append(resolved, PlanOperation{Op: "drop", Hash: hash})

		case "fold":
			if len(op.Hashes) < 2 {
				errorsOut = append(errorsOut, errorObject("fold_requires_multiple_commits", "fold requires at least two hashes"))
				continue
			}
			resolvedHashes := make([]string, 0, len(op.Hashes))
			for _, value := range op.Hashes {
				hash, errs := resolver.resolve(value)
				errorsOut = append(errorsOut, errs...)
				if hash != "" {
					resolvedHashes = append(resolvedHashes, hash)
				}
			}
			anchor := op.Anchor
			if anchor == "" && len(resolvedHashes) > 0 {
				anchor = resolvedHashes[0]
			}
			resolvedAnchor, errs := resolver.resolve(anchor)
			errorsOut = append(errorsOut, errs...)
			if len(resolvedHashes) != len(op.Hashes) || resolvedAnchor == "" {
				continue
			}
			if !containsString(resolvedHashes, resolvedAnchor) {
				errorsOut = append(errorsOut, Error{Code: "fold_anchor_not_in_group", Message: "fold anchor must be part of hashes", Hash: resolvedAnchor})
				continue
			}
			group := make([]plumbing.Hash, 0, len(resolvedHashes))
			for _, hash := range resolvedHashes {
				if previous := seenOperation(seen, hash, "fold"); previous != "" {
					errorsOut = append(errorsOut, Error{Code: "duplicate_operation", Message: fmt.Sprintf("commit already has %s operation", previous), Hash: hash})
					continue
				}
				group = append(group, plumbing.NewHash(hash))
			}
			if len(group) != len(resolvedHashes) {
				continue
			}
			anchorHash := plumbing.NewHash(resolvedAnchor)
			for _, hash := range group {
				changes = append(changes, gitops.ForgeChange{OriginalHash: hash, Operation: gitops.ForgeCombine, CombineGroup: group, CombineAnchor: anchorHash})
			}
			resolved = append(resolved, PlanOperation{Op: "fold", Hashes: resolvedHashes, Anchor: resolvedAnchor})

		default:
			errorsOut = append(errorsOut, Error{Code: "unknown_operation", Message: fmt.Sprintf("unknown operation %q", op.Op)})
		}
	}
	return changes, resolved, warnings, errorsOut
}

func editChange(hash string, op PlanOperation) (gitops.ForgeChange, *Warning, *Error) {
	change := gitops.ForgeChange{OriginalHash: plumbing.NewHash(hash)}
	if (op.AuthorName == nil) != (op.AuthorEmail == nil) {
		toolErr := Error{Code: "author_identity_incomplete", Message: "author_name and author_email must be supplied together", Hash: hash}
		return change, nil, &toolErr
	}
	if op.AuthorName != nil && op.AuthorEmail != nil {
		change.NewAuthor = &gitops.AuthorInfo{Name: *op.AuthorName, Email: *op.AuthorEmail}
	}
	if op.AuthorDate != nil {
		parsed, err := time.Parse(time.RFC3339, *op.AuthorDate)
		if err != nil {
			toolErr := Error{Code: "invalid_author_date", Message: "author_date must be RFC3339", Hash: hash}
			return change, nil, &toolErr
		}
		change.NewDate = &parsed
	}
	if op.Message != nil {
		change.NewMessage = *op.Message
	}
	if !change.HasChanges() {
		warning := Warning{Code: "empty_edit", Message: "edit operation does not change any fields", Hash: hash}
		return change, &warning, nil
	}
	return change, nil, nil
}

func resolvedEditOperation(hash string, op PlanOperation) PlanOperation {
	return PlanOperation{Op: "edit", Hash: hash, AuthorName: op.AuthorName, AuthorEmail: op.AuthorEmail, AuthorDate: op.AuthorDate, Message: op.Message}
}

type hashResolver struct {
	fullHashes map[string]bool
	prefixes   map[string][]string
}

func newHashResolver(commits []gitops.CommitInfo) hashResolver {
	resolver := hashResolver{fullHashes: make(map[string]bool), prefixes: make(map[string][]string)}
	for _, commit := range commits {
		full := strings.ToLower(commit.Hash.String())
		resolver.fullHashes[full] = true
		for i := minHashPrefixLength; i <= len(full); i++ {
			prefix := full[:i]
			resolver.prefixes[prefix] = append(resolver.prefixes[prefix], full)
		}
	}
	for prefix := range resolver.prefixes {
		resolver.prefixes[prefix] = uniqueStrings(resolver.prefixes[prefix])
	}
	return resolver
}

func (r hashResolver) resolve(value string) (string, []Error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", []Error{errorObject("missing_hash", "hash is required")}
	}
	if !isHashPrefix(value) {
		return "", []Error{{Code: "invalid_hash", Message: fmt.Sprintf("hash must be %d to 40 hex characters", minHashPrefixLength), Hash: value}}
	}
	if isFullHash(value) {
		if r.fullHashes[value] {
			return value, nil
		}
		return "", []Error{{Code: "hash_not_reachable", Message: "hash is not reachable from selected ref", Hash: value}}
	}
	matches := r.prefixes[value]
	if len(matches) == 0 {
		return "", []Error{{Code: "hash_not_found", Message: "hash prefix does not match a reachable commit", Hash: value}}
	}
	if len(matches) > 1 {
		return "", []Error{{Code: "ambiguous_hash", Message: "hash prefix matches multiple reachable commits", Hash: value, Matches: matches}}
	}
	return matches[0], nil
}

func validateReplayLimits(changes []gitops.ForgeChange, commits []gitops.CommitInfo) []Error {
	changeByHash := make(map[string]gitops.ForgeChange, len(changes))
	for _, change := range changes {
		changeByHash[change.OriginalHash.String()] = change
	}
	firstRewrite := -1
	for i := len(commits) - 1; i >= 0; i-- {
		if change, ok := changeByHash[commits[i].Hash.String()]; ok && change.HasChanges() {
			firstRewrite = i
			break
		}
	}
	if firstRewrite == -1 {
		return nil
	}
	hasReplay := false
	for _, change := range changes {
		if change.Operation == gitops.ForgeDrop || change.Operation == gitops.ForgeCombine {
			hasReplay = true
			break
		}
	}
	if !hasReplay {
		return nil
	}
	var errorsOut []Error
	oldestFirst := make([]gitops.CommitInfo, len(commits))
	for i := range commits {
		oldestFirst[len(commits)-1-i] = commits[i]
	}
	oldestIndex := len(commits) - 1 - firstRewrite
	if oldestIndex >= 0 && oldestIndex < len(oldestFirst) && len(oldestFirst[oldestIndex].Parents) == 0 {
		errorsOut = append(errorsOut, Error{Code: "root_replay_unsupported", Message: "dropping or folding from the root commit is not supported", Hash: oldestFirst[oldestIndex].Hash.String()})
	}
	for _, commit := range oldestFirst[oldestIndex:] {
		if len(commit.Parents) != 1 {
			errorsOut = append(errorsOut, Error{Code: "merge_replay_unsupported", Message: "dropping or folding across merge commits is not supported", Hash: commit.Hash.String()})
		}
	}
	return errorsOut
}

func seenOperation(seen map[string]string, hash string, op string) string {
	if previous := seen[hash]; previous != "" {
		return previous
	}
	seen[hash] = op
	return ""
}

func resolveRef(repo *gitops.Repository, refArg string) (string, string, string, error) {
	refArg = strings.TrimSpace(refArg)
	if refArg == "" || refArg == "HEAD" {
		ref, err := repo.GetHead()
		if err != nil {
			return "", "", "", err
		}
		branch := ""
		if ref.Name().IsBranch() {
			branch = ref.Name().Short()
		}
		return ref.Name().String(), strings.ToLower(ref.Hash().String()), branch, nil
	}
	canonical := refArg
	if !strings.HasPrefix(canonical, "refs/") {
		canonical = "refs/heads/" + canonical
	}
	ref, err := repo.GetReference(canonical)
	if err != nil {
		return "", "", "", err
	}
	branch := ""
	if ref.Name().IsBranch() {
		branch = ref.Name().Short()
	}
	return ref.Name().String(), strings.ToLower(ref.Hash().String()), branch, nil
}

func commitsForRef(repo *gitops.Repository, ref string) ([]gitops.CommitInfo, error) {
	commits, _, err := repo.ListCommitsFromRefWithGraph(ref)
	return commits, err
}

func ensureRefCheckedOut(repo *gitops.Repository, ref string, expectedHead string) error {
	head, err := repo.GetHead()
	if err != nil {
		return err
	}
	if head.Name().String() != ref {
		return fmt.Errorf("plan ref %s is not the checked out HEAD ref %s", ref, head.Name().String())
	}
	if strings.ToLower(head.Hash().String()) != expectedHead {
		return fmt.Errorf("HEAD changed from %s to %s", expectedHead, head.Hash().String())
	}
	return nil
}

func readPlan(path string) (Plan, error) {
	var plan Plan
	data, err := os.ReadFile(path)
	if err != nil {
		return plan, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return plan, errors.New("plan contains multiple JSON values")
	}
	return plan, nil
}

func jsonCommits(commits []gitops.CommitInfo) []Commit {
	out := make([]Commit, len(commits))
	for i, commit := range commits {
		parents := make([]string, len(commit.Parents))
		for j, parent := range commit.Parents {
			parents[j] = strings.ToLower(parent.String())
		}
		out[i] = Commit{
			Hash:        strings.ToLower(commit.Hash.String()),
			ShortHash:   commit.ShortHash,
			Parents:     parents,
			AuthorName:  commit.AuthorName,
			AuthorEmail: commit.AuthorEmail,
			AuthorDate:  commit.AuthorDate.Format(time.RFC3339),
			Subject:     strings.Split(commit.Message, "\n")[0],
			Message:     commit.Message,
			Additions:   commit.Additions,
			Deletions:   commit.Deletions,
		}
	}
	return out
}

func changedRefsJSON(refs map[plumbing.Hash]plumbing.Hash) map[string]string {
	out := make(map[string]string, len(refs))
	for oldHash, newHash := range refs {
		out[strings.ToLower(oldHash.String())] = strings.ToLower(newHash.String())
	}
	return out
}

func writeJSON(w io.Writer, value any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func errorObject(code string, message string) Error {
	return Error{Code: code, Message: message}
}

func isFullHash(value string) bool {
	return len(value) == 40 && isHex(value)
}

func isHashPrefix(value string) bool {
	return len(value) >= minHashPrefixLength && len(value) <= 40 && isHex(value)
}

func isHex(value string) bool {
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func uniqueStrings(values []string) []string {
	sort.Strings(values)
	unique := values[:0]
	var previous string
	for i, value := range values {
		if i > 0 && value == previous {
			continue
		}
		unique = append(unique, value)
		previous = value
	}
	return unique
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
