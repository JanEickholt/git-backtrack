package tool

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Jan/git-backtrack/internal/gitops"
)

const planVersion = 1
const minHashPrefixLength = 7
const defaultListLimit = 5

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
	Adjustment  *string  `json:"adjustment,omitempty"`
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
	Additions   *int     `json:"additions,omitempty"`
	Deletions   *int     `json:"deletions,omitempty"`
}

type ListResponse struct {
	OK        bool     `json:"ok"`
	Ref       string   `json:"ref"`
	Head      string   `json:"head"`
	Branch    string   `json:"branch,omitempty"`
	Total     int      `json:"total"`
	Limit     int      `json:"limit,omitempty"`
	Offset    int      `json:"offset,omitempty"`
	Remaining int      `json:"remaining,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Commits   []Commit `json:"commits"`
	Errors    []Error  `json:"errors,omitempty"`
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

type BackupEntry struct {
	Name      string `json:"name"`
	Ref       string `json:"ref"`
	CreatedAt string `json:"created_at,omitempty"`
}

type BackupsResponse struct {
	OK      bool          `json:"ok"`
	Backups []BackupEntry `json:"backups"`
	Errors  []Error       `json:"errors,omitempty"`
}

type RestoreResponse struct {
	OK           bool      `json:"ok"`
	BackupRef    string    `json:"backup_ref,omitempty"`
	RestoredRefs []string  `json:"restored_refs,omitempty"`
	Warnings     []Warning `json:"warnings,omitempty"`
	Errors       []Error   `json:"errors,omitempty"`
}

type HelpResponse struct {
	OK                  bool              `json:"ok"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Workflow            []string          `json:"workflow"`
	Commands            []CommandHelp     `json:"commands"`
	HashRules           HashRules         `json:"hash_rules"`
	PlanSchema          PlanSchema        `json:"plan_schema"`
	OperationSchemas    []OperationSchema `json:"operation_schemas"`
	ExamplePlan         Plan              `json:"example_plan"`
	ResponseShapes      map[string]string `json:"response_shapes"`
	ErrorCodes          []string          `json:"error_codes"`
	WarningCodes        []string          `json:"warning_codes"`
	SafetyNotes         []string          `json:"safety_notes"`
	RecommendedSequence []string          `json:"recommended_sequence"`
}

type CommandHelp struct {
	Name        string   `json:"name"`
	Usage       string   `json:"usage"`
	Description string   `json:"description"`
	Flags       []string `json:"flags"`
}

type HashRules struct {
	MinimumPrefixLength int      `json:"minimum_prefix_length"`
	Accepted            []string `json:"accepted"`
	Rejected            []string `json:"rejected"`
}

type PlanSchema struct {
	Version      string `json:"version"`
	Ref          string `json:"ref"`
	ExpectedHead string `json:"expected_head"`
	Operations   string `json:"operations"`
}

type OperationSchema struct {
	Op          string   `json:"op"`
	Required    []string `json:"required"`
	Optional    []string `json:"optional,omitempty"`
	Description string   `json:"description"`
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
	case "help", "list", "validate", "apply", "backups", "restore":
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
	case "help":
		return runHelp(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "backups":
		return runBackups(args[1:], stdout, stderr)
	case "restore":
		return runRestore(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown tool command %q\n", args[0])
		return 2
	}
}

func runHelp(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	fs.SetOutput(stderr)
	_ = fs.Bool("json", true, "emit JSON")
	compact := fs.Bool("compact", false, "emit JSON on a single line")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	writeJSON(stdout, toolHelp(), *compact)
	return 0
}

func runList(args []string, stdout io.Writer, stderr io.Writer) int {
	args = normalizeListArgs(args)
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	refArg := fs.String("ref", "", "ref or branch to list, defaults to HEAD")
	limit := fs.Int("limit", defaultListLimit, "maximum number of newest commits to return (0 or negative disables the limit)")
	offset := fs.Int("offset", 0, "number of newest commits to skip before applying --limit (ignored unless > 0)")
	all := fs.Bool("all", false, "return every reachable commit (overrides --limit)")
	stats := fs.Bool("stats", false, "include additions/deletions per commit (omitted by default to keep responses small)")
	date := fs.String("date", "", "only commits on this date (YYYY-MM-DD)")
	dateRange := fs.String("date-range", "", "only commits in this date range (YYYY-MM-DD..YYYY-MM-DD)")
	since := fs.String("since", "", "only commits on or after this date/time")
	after := fs.String("after", "", "alias for --since")
	until := fs.String("until", "", "only commits on or before this date/time")
	before := fs.String("before", "", "alias for --until")
	author := fs.String("author", "", "only commits whose author name/email matches this pattern")
	email := fs.String("email", "", "only commits whose author email matches this pattern")
	mail := fs.String("mail", "", "alias for --email")
	_ = fs.Bool("json", true, "emit JSON")
	compact := fs.Bool("compact", false, "emit JSON on a single line")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	filter, filterErr := buildListFilter(*date, *dateRange, firstNonEmpty(*since, *after), firstNonEmpty(*until, *before), *author, firstNonEmpty(*email, *mail))
	if filterErr != nil {
		writeJSON(stdout, ListResponse{OK: false, Errors: []Error{errorObject(filterErr.code, filterErr.message)}}, *compact)
		return 2
	}
	repo, err := gitops.Open(*repoPath)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Errors: []Error{errorObject("open_repository_failed", err.Error())}}, *compact)
		return 1
	}
	ref, refHash, branch, err := resolveRef(repo, *refArg)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Errors: []Error{errorObject("resolve_ref_failed", err.Error())}}, *compact)
		return 1
	}
	total, err := repo.CountCommitsFromRef(ref, filter)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Ref: ref, Head: refHash, Errors: []Error{errorObject("count_commits_failed", err.Error())}}, *compact)
		return 1
	}

	effectiveLimit, effectiveOffset, remaining, truncated := listWindow(total, *limit, *offset, *all)
	commits, err := listCommitsForResponse(repo, ref, *stats, effectiveLimit, effectiveOffset, *all, filter)
	if err != nil {
		writeJSON(stdout, ListResponse{OK: false, Ref: ref, Head: refHash, Errors: []Error{errorObject("list_commits_failed", err.Error())}}, *compact)
		return 1
	}

	writeJSON(stdout, ListResponse{OK: true, Ref: ref, Head: refHash, Branch: branch, Total: total, Limit: effectiveLimit, Offset: effectiveOffset, Remaining: remaining, Truncated: truncated, Commits: jsonCommits(commits, *stats)}, *compact)
	return 0
}

func parsePositionalLimit(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("positional limit %q is not an integer", value)
	}
	return parsed, nil
}

func normalizeListArgs(args []string) []string {
	if hasListLimitFlag(args) {
		return args
	}
	normalized := make([]string, 0, len(args)+1)
	limitSet := false
	for _, arg := range args {
		if !limitSet && !strings.HasPrefix(arg, "-") {
			if parsed, err := parsePositionalLimit(arg); err == nil {
				normalized = append(normalized, "--limit", strconv.Itoa(parsed))
				limitSet = true
				continue
			}
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func hasListLimitFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--limit" || arg == "-limit" || strings.HasPrefix(arg, "--limit=") || strings.HasPrefix(arg, "-limit=") {
			return true
		}
	}
	return false
}

func listWindow(total int, limit int, offset int, all bool) (effectiveLimit int, effectiveOffset int, remaining int, truncated bool) {
	if all {
		return 0, 0, 0, false
	}
	skip := 0
	if offset > 0 {
		skip = min(offset, total)
		effectiveOffset = skip
	}
	visible := total - skip
	capped := visible
	if limit > 0 && visible > limit {
		capped = limit
		effectiveLimit = limit
	}
	remaining = total - skip - capped
	truncated = remaining > 0
	return effectiveLimit, effectiveOffset, remaining, truncated
}

type listFilterError struct {
	code    string
	message string
}

func buildListFilter(date string, dateRange string, since string, until string, author string, email string) (gitops.LogFilter, *listFilterError) {
	filter := gitops.LogFilter{Author: strings.TrimSpace(author), Email: strings.TrimSpace(email)}
	if strings.TrimSpace(date) != "" && strings.TrimSpace(dateRange) != "" {
		return filter, &listFilterError{code: "invalid_date_filter", message: "--date and --date-range cannot be combined"}
	}
	if strings.TrimSpace(date) != "" && (strings.TrimSpace(since) != "" || strings.TrimSpace(until) != "") {
		return filter, &listFilterError{code: "invalid_date_filter", message: "--date cannot be combined with --since/--after/--until/--before"}
	}
	if strings.TrimSpace(dateRange) != "" && (strings.TrimSpace(since) != "" || strings.TrimSpace(until) != "") {
		return filter, &listFilterError{code: "invalid_date_filter", message: "--date-range cannot be combined with --since/--after/--until/--before"}
	}

	if strings.TrimSpace(date) != "" {
		start, end, err := dayRange(strings.TrimSpace(date))
		if err != nil {
			return filter, &listFilterError{code: "invalid_date", message: err.Error()}
		}
		filter.Since = start
		filter.Before = end
		return filter, nil
	}
	if strings.TrimSpace(dateRange) != "" {
		start, end, err := parseDateRange(strings.TrimSpace(dateRange))
		if err != nil {
			return filter, &listFilterError{code: "invalid_date_range", message: err.Error()}
		}
		filter.Since = start
		filter.Before = end
		return filter, nil
	}
	filter.Since = normalizeSince(strings.TrimSpace(since))
	filter.Before = normalizeUntil(strings.TrimSpace(until))
	return filter, nil
}

func dayRange(value string) (string, string, error) {
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", "", fmt.Errorf("date %q must use YYYY-MM-DD", value)
	}
	return day.Format(time.RFC3339), day.AddDate(0, 0, 1).Format(time.RFC3339), nil
}

func parseDateRange(value string) (string, string, error) {
	parts := strings.Split(value, "..")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("date range %q must use YYYY-MM-DD..YYYY-MM-DD", value)
	}
	start, _, err := dayRange(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", "", err
	}
	_, end, err := dayRange(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", "", err
	}
	return start, end, nil
}

func normalizeSince(value string) string {
	return value
}

func normalizeUntil(value string) string {
	if _, end, err := dayRange(value); err == nil {
		return end
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func listCommitsForResponse(repo *gitops.Repository, ref string, includeStats bool, limit int, offset int, all bool, filter gitops.LogFilter) ([]gitops.CommitInfo, error) {
	if all {
		return repo.ListCommitsFromRefWindowWithStats(ref, includeStats, 0, 0, filter)
	}
	return repo.ListCommitsFromRefWindowWithStats(ref, includeStats, limit, offset, filter)
}

func runValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	planPath := fs.String("plan", "", "path to JSON plan")
	_ = fs.Bool("json", true, "emit JSON")
	compact := fs.Bool("compact", false, "emit JSON on a single line")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	result, status := validatePlanFromPath(*repoPath, *planPath)
	writeJSON(stdout, ValidateResponse{OK: len(result.errors) == 0, Ref: result.ref, Head: result.head, ResolvedOperations: result.resolved, Warnings: result.warnings, Errors: result.errors}, *compact)
	return status
}

func runApply(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	planPath := fs.String("plan", "", "path to JSON plan")
	yes := fs.Bool("yes", false, "apply the history rewrite")
	_ = fs.Bool("json", true, "emit JSON")
	compact := fs.Bool("compact", false, "emit JSON on a single line")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	result, status := validatePlanFromPath(*repoPath, *planPath)
	if status != 0 {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: result.errors}, *compact)
		return status
	}
	if !*yes {
		result.errors = append(result.errors, errorObject("confirmation_required", "apply requires --yes"))
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: result.errors}, *compact)
		return 1
	}
	if len(result.changes) == 0 {
		writeJSON(stdout, ApplyResponse{OK: true, Ref: result.ref, Head: result.head, ChangedRefs: map[string]string{}, Warnings: result.warnings}, *compact)
		return 0
	}

	repo, err := gitops.Open(*repoPath)
	if err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Errors: []Error{errorObject("open_repository_failed", err.Error())}}, *compact)
		return 1
	}
	if err := ensureRefCheckedOut(repo, result.ref, result.head); err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: []Error{errorObject("ref_not_checked_out", err.Error())}}, *compact)
		return 1
	}

	rewriter := gitops.NewHistoryRewriter(repo)
	backupRef, err := rewriter.CreateFullBackup()
	if err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, Warnings: result.warnings, Errors: []Error{errorObject("backup_failed", err.Error())}}, *compact)
		return 1
	}
	rewriteResult, err := rewriter.ApplyChanges(result.changes)
	if err != nil {
		writeJSON(stdout, ApplyResponse{OK: false, Ref: result.ref, Head: result.head, BackupRef: backupRef, Warnings: result.warnings, Errors: []Error{errorObject("apply_failed", err.Error())}}, *compact)
		return 1
	}

	writeJSON(stdout, ApplyResponse{OK: true, Ref: result.ref, Head: result.head, BackupRef: backupRef, ChangedRefs: changedRefsJSON(rewriteResult.ChangedRefs), Warnings: result.warnings}, *compact)
	return 0
}

func runBackups(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("backups", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	_ = fs.Bool("json", true, "emit JSON")
	compact := fs.Bool("compact", false, "emit JSON on a single line")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	repo, err := gitops.Open(*repoPath)
	if err != nil {
		writeJSON(stdout, BackupsResponse{OK: false, Errors: []Error{errorObject("open_repository_failed", err.Error())}}, *compact)
		return 1
	}
	rewriter := gitops.NewHistoryRewriter(repo)
	backups, err := rewriter.ListBackups()
	if err != nil {
		writeJSON(stdout, BackupsResponse{OK: false, Errors: []Error{errorObject("list_backups_failed", err.Error())}}, *compact)
		return 1
	}

	writeJSON(stdout, BackupsResponse{OK: true, Backups: backupEntries(backups)}, *compact)
	return 0
}

func runRestore(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("path", ".", "path to git repository")
	yes := fs.Bool("yes", false, "confirm history rewrite")
	backupArg := fs.String("backup", "", "backup name or full ref to restore")
	refArg := fs.String("ref", "", "branch to checkout after restore")
	_ = fs.Bool("json", true, "emit JSON")
	compact := fs.Bool("compact", false, "emit JSON on a single line")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !*yes {
		writeJSON(stdout, RestoreResponse{OK: false, Errors: []Error{errorObject("confirmation_required", "restore requires --yes")}}, *compact)
		return 1
	}

	repo, err := gitops.Open(*repoPath)
	if err != nil {
		writeJSON(stdout, RestoreResponse{OK: false, Errors: []Error{errorObject("open_repository_failed", err.Error())}}, *compact)
		return 1
	}
	rewriter := gitops.NewHistoryRewriter(repo)

	backupPrefix, backupErr := resolveBackupPrefix(rewriter, *backupArg)
	if backupErr != nil {
		writeJSON(stdout, RestoreResponse{OK: false, Errors: []Error{errorObject(backupErr.code, backupErr.message)}}, *compact)
		return 1
	}

	restoredRefs, err := rewriter.RestoreFromBackup(backupPrefix)
	if err != nil {
		writeJSON(stdout, RestoreResponse{OK: false, BackupRef: backupPrefix, Errors: []Error{errorObject("restore_failed", err.Error())}}, *compact)
		return 1
	}

	if *refArg != "" {
		if err := repo.SwitchBranch(*refArg); err != nil {
			writeJSON(stdout, RestoreResponse{OK: false, BackupRef: backupPrefix, RestoredRefs: restoredRefs, Errors: []Error{errorObject("ref_not_checked_out", err.Error())}}, *compact)
			return 1
		}
	}

	writeJSON(stdout, RestoreResponse{OK: true, BackupRef: backupPrefix, RestoredRefs: restoredRefs}, *compact)
	return 0
}

func toolHelp() HelpResponse {
	exampleMessage := "new commit message"
	exampleAdjustment := "+10h"
	return HelpResponse{
		OK:          true,
		Name:        "git-backtrack tool mode",
		Description: "JSON interface for inspecting, validating, and applying git history rewrite plans without driving the TUI.",
		Workflow: []string{
			"Run list --json to discover the current ref, expected_head, and commit hashes. The newest 5 commits are returned by default; use --offset to paginate, --stats for additions/deletions, --date/--date-range/--author/--email to filter, or --all (or --limit 0) when older commits are needed.",
			"Create a version 1 plan using hash values from list output or unambiguous hash prefixes.",
			"Run validate --plan plan.json --json and inspect ok/errors/resolved_operations.",
			"Run apply --plan plan.json --json --yes only after validation succeeds and the user wants to rewrite history.",
		},
		Commands: []CommandHelp{
			{Name: "help", Usage: "git-backtrack help --json", Description: "Print this machine-readable tool contract.", Flags: []string{"--json", "--compact"}},
			{Name: "list", Usage: "git-backtrack list --path . --json [--ref main] [--limit N] [--offset N] [--date YYYY-MM-DD] [--date-range YYYY-MM-DD..YYYY-MM-DD] [--author PATTERN] [--email PATTERN] [--stats] [--all]", Description: "List reachable commits for a ref and return current head metadata. Defaults to the 5 newest commits; pass --offset to skip newer ones, date/author filters to narrow results, --stats for additions/deletions, or --all / --limit 0 for every reachable commit.", Flags: []string{"--path <repo>", "--ref <ref-or-branch>", "--limit <n>", "--offset <n>", "--date <yyyy-mm-dd>", "--date-range <yyyy-mm-dd..yyyy-mm-dd>", "--since <date>", "--after <date>", "--until <date>", "--before <date>", "--author <pattern>", "--email <pattern>", "--mail <pattern>", "--stats", "--all", "--json", "--compact"}},
			{Name: "validate", Usage: "git-backtrack validate --path . --plan plan.json --json", Description: "Validate a rewrite plan and return normalized resolved operations.", Flags: []string{"--path <repo>", "--plan <file>", "--json", "--compact"}},
			{Name: "apply", Usage: "git-backtrack apply --path . --plan plan.json --json --yes", Description: "Validate then apply a rewrite plan. Requires --yes and creates a backup for real rewrites.", Flags: []string{"--path <repo>", "--plan <file>", "--json", "--compact", "--yes"}},
			{Name: "backups", Usage: "git-backtrack backups --path . --json", Description: "List all backtrack backup refs in the repository.", Flags: []string{"--path <repo>", "--json", "--compact"}},
			{Name: "restore", Usage: "git-backtrack restore --path . --json --yes [--backup <name-or-ref>] [--ref <branch>]", Description: "Restore branch refs from a backup. Requires --yes. Defaults to the latest backup when --backup is omitted.", Flags: []string{"--path <repo>", "--json", "--compact", "--yes", "--backup <name-or-ref>", "--ref <branch>"}},
		},
		HashRules: HashRules{
			MinimumPrefixLength: minHashPrefixLength,
			Accepted:            []string{"full 40-character hex commit hashes reachable from the selected ref", "hex hash prefixes of at least 7 characters that match exactly one reachable commit"},
			Rejected:            []string{"ambiguous prefixes", "prefixes shorter than 7 characters", "non-hex strings", "hashes not reachable from the selected ref"},
		},
		PlanSchema: PlanSchema{
			Version:      "integer, required, must be 1",
			Ref:          "string, required, e.g. refs/heads/main or main",
			ExpectedHead: "string, required, full 40-character head hash from list output",
			Operations:   "array, required, contains edit/drop/fold operations",
		},
		OperationSchemas: []OperationSchema{
			{Op: "edit", Required: []string{"op", "hash"}, Optional: []string{"author_name", "author_email", "author_date", "message"}, Description: "Edit metadata/message for one commit. author_name and author_email must be supplied together. author_date must be RFC3339."},
			{Op: "drop", Required: []string{"op", "hash"}, Description: "Drop one commit during replay."},
			{Op: "fold", Required: []string{"op", "hashes", "anchor"}, Description: "Fold multiple commits into one. hashes must contain at least two commits; anchor must be one of hashes and provides message/date metadata."},
			{Op: "smart_adjust", Required: []string{"op", "hashes", "adjustment"}, Description: "Adjust selected commit author dates with the same weighted distribution as the TUI smart adjust field. adjustment accepts +10h, -30m, 1d, or compound values like 1h +30m."},
		},
		ExamplePlan: Plan{
			Version:      planVersion,
			Ref:          "refs/heads/main",
			ExpectedHead: "0123456789abcdef0123456789abcdef01234567",
			Operations: []PlanOperation{
				{Op: "edit", Hash: "89abcde", Message: &exampleMessage},
				{Op: "drop", Hash: "fedcba9876543210fedcba9876543210fedcba98"},
				{Op: "fold", Hashes: []string{"1111111", "2222222"}, Anchor: "2222222"},
				{Op: "smart_adjust", Hashes: []string{"3333333", "4444444", "5555555"}, Adjustment: &exampleAdjustment},
			},
		},
		ResponseShapes: map[string]string{
			"list":     "{ok, ref, head, branch, total, limit, offset, remaining, truncated, commits[], errors[]}",
			"validate": "{ok, ref, head, resolved_operations[], warnings[], errors[]}",
			"apply":    "{ok, ref, head, backup_ref, changed_refs, warnings[], errors[]}",
			"backups":  "{ok, backups[{name, ref, created_at}], errors[]}",
			"restore":  "{ok, backup_ref, restored_refs[], warnings[], errors[]}",
		},
		ErrorCodes: []string{
			"ambiguous_hash",
			"apply_failed",
			"author_identity_incomplete",
			"backup_failed",
			"backup_not_found",
			"confirmation_required",
			"duplicate_operation",
			"expected_head_mismatch",
			"fold_anchor_not_in_group",
			"fold_requires_multiple_commits",
			"hash_not_found",
			"hash_not_reachable",
			"invalid_author_date",
			"invalid_date",
			"invalid_date_filter",
			"invalid_date_range",
			"invalid_hash",
			"invalid_adjustment",
			"list_backups_failed",
			"merge_replay_unsupported",
			"missing_expected_head",
			"missing_hash",
			"missing_plan",
			"missing_ref",
			"no_backups_found",
			"ref_not_checked_out",
			"resolve_ref_failed",
			"restore_failed",
			"root_replay_unsupported",
			"smart_adjust_requires_multiple_commits",
			"unknown_operation",
			"unsupported_plan_version",
		},
		WarningCodes: []string{
			"date_after_child",
			"date_before_parent",
			"empty_edit",
		},
		SafetyNotes: []string{
			"apply refuses to run without --yes.",
			"apply validates the plan before rewriting history.",
			"apply requires the plan ref to be the checked-out HEAD ref.",
			"apply creates refs/backtrack-backup/* before real rewrites.",
			"restore refuses to run without --yes.",
			"No-op plans return ok without creating a backup.",
		},
		RecommendedSequence: []string{
			"git-backtrack help --json",
			"git-backtrack list --path . --json --ref main",
			"write plan.json using ref/head/hash data from list output",
			"git-backtrack validate --path . --plan plan.json --json",
			"git-backtrack apply --path . --plan plan.json --json --yes",
			"git-backtrack backups --path . --json",
			"git-backtrack restore --path . --json --yes --backup <name>",
		},
	}
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

	changes, resolved, warnings, errors := resolveOperations(plan.Operations, resolver, commits)
	result.changes = changes
	result.resolved = resolved
	result.warnings = warnings
	result.errors = append(result.errors, errors...)
	if len(result.errors) > 0 {
		return result
	}

	result.errors = append(result.errors, validateReplayLimits(changes, commits)...)
	if len(result.errors) > 0 {
		return result
	}
	result.warnings = append(result.warnings, validateDateOrdering(changes, commits)...)
	return result
}

func resolveOperations(operations []PlanOperation, resolver hashResolver, commits []gitops.CommitInfo) ([]gitops.ForgeChange, []PlanOperation, []Warning, []Error) {
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

		case "smart_adjust":
			if len(op.Hashes) < 2 {
				errorsOut = append(errorsOut, errorObject("smart_adjust_requires_multiple_commits", "smart_adjust requires at least two hashes"))
				continue
			}
			if op.Adjustment == nil || strings.TrimSpace(*op.Adjustment) == "" {
				errorsOut = append(errorsOut, errorObject("invalid_adjustment", "smart_adjust adjustment is required"))
				continue
			}
			adjustment, ok := parseToolDuration(*op.Adjustment)
			if !ok {
				errorsOut = append(errorsOut, errorObject("invalid_adjustment", "adjustment must be a signed duration like +10h, -30m, or 1d"))
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
			if len(resolvedHashes) != len(op.Hashes) {
				continue
			}
			duplicate := false
			for _, hash := range resolvedHashes {
				if previous := seenOperation(seen, hash, "smart_adjust"); previous != "" {
					errorsOut = append(errorsOut, Error{Code: "duplicate_operation", Message: fmt.Sprintf("commit already has %s operation", previous), Hash: hash})
					duplicate = true
				}
			}
			if duplicate {
				continue
			}

			smartChanges := smartAdjustChanges(commits, resolvedHashes, adjustment)
			changes = append(changes, smartChanges...)
			resolved = append(resolved, PlanOperation{Op: "smart_adjust", Hashes: resolvedHashes, Adjustment: op.Adjustment})

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

func smartAdjustChanges(commits []gitops.CommitInfo, selectedHashes []string, timeToAdd time.Duration) []gitops.ForgeChange {
	adjustments := calculateSmartTimeAdjust(commits, selectedHashes, timeToAdd)
	if len(adjustments) == 0 {
		return nil
	}
	selected := make(map[string]bool, len(selectedHashes))
	for _, hash := range selectedHashes {
		selected[strings.ToLower(hash)] = true
	}
	changes := make([]gitops.ForgeChange, 0, len(adjustments))
	for _, commit := range commits {
		hash := strings.ToLower(commit.Hash.String())
		if !selected[hash] {
			continue
		}
		adjustment, ok := adjustments[hash]
		if !ok || adjustment == 0 {
			continue
		}
		newDate := commit.AuthorDate.Add(adjustment)
		changes = append(changes, gitops.ForgeChange{OriginalHash: commit.Hash, NewDate: &newDate})
	}
	return changes
}

func calculateSmartTimeAdjust(commits []gitops.CommitInfo, selectedHashes []string, timeToAdd time.Duration) map[string]time.Duration {
	result := make(map[string]time.Duration)
	selected := make(map[string]bool, len(selectedHashes))
	for _, hash := range selectedHashes {
		selected[strings.ToLower(hash)] = true
	}

	selectedCommits := make([]gitops.CommitInfo, 0, len(selectedHashes))
	for _, commit := range commits {
		if selected[strings.ToLower(commit.Hash.String())] {
			selectedCommits = append(selectedCommits, commit)
		}
	}
	if len(selectedCommits) < 2 || timeToAdd == 0 {
		return result
	}

	weights := make([]float64, len(selectedCommits)-1)
	var totalWeight float64
	for i := 0; i < len(weights); i++ {
		weight := smartCommitWeight(selectedCommits[i])
		weights[i] = weight
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return result
	}

	cumulative := time.Duration(0)
	oldestIndex := len(selectedCommits) - 1
	result[strings.ToLower(selectedCommits[oldestIndex].Hash.String())] = 0
	for i := oldestIndex - 1; i >= 0; i-- {
		gap := time.Duration(float64(timeToAdd) * weights[i] / totalWeight)
		cumulative += gap
		result[strings.ToLower(selectedCommits[i].Hash.String())] = cumulative
	}
	result[strings.ToLower(selectedCommits[0].Hash.String())] = timeToAdd
	return result
}

func smartCommitWeight(commit gitops.CommitInfo) float64 {
	changedLines := commit.Additions + commit.Deletions
	if changedLines < 0 {
		changedLines = 0
	}

	weight := 1.0 + math.Sqrt(float64(changedLines))
	message := strings.ToLower(strings.TrimSpace(commit.Message))
	subject := strings.SplitN(message, "\n", 2)[0]

	switch {
	case strings.HasPrefix(subject, "chore") || strings.HasPrefix(subject, "docs"):
		weight *= 0.45
	case strings.HasPrefix(subject, "test"):
		weight *= 0.65
	case strings.HasPrefix(subject, "fix"):
		weight *= 1.15
	case strings.HasPrefix(subject, "feat"):
		weight *= 1.2
	case strings.HasPrefix(subject, "refactor"):
		weight *= 1.05
	}

	if strings.Contains(subject, "lint") || strings.Contains(subject, "format") {
		weight *= 0.5
	}
	if strings.Contains(subject, "error") || strings.Contains(subject, "bug") || strings.Contains(subject, "crash") {
		weight *= 1.15
	}

	return weight
}

func parseToolDuration(adjustment string) (time.Duration, bool) {
	adj := strings.TrimSpace(adjustment)
	if adj == "" {
		return 0, false
	}

	var total time.Duration
	i := 0
	for i < len(adj) {
		for i < len(adj) && adj[i] == ' ' {
			i++
		}
		if i >= len(adj) {
			break
		}

		sign := int64(1)
		if adj[i] == '-' {
			sign = -1
			i++
		} else if adj[i] == '+' {
			i++
		}
		for i < len(adj) && adj[i] == ' ' {
			i++
		}

		amountStart := i
		for i < len(adj) && adj[i] >= '0' && adj[i] <= '9' {
			i++
		}
		if amountStart == i {
			return 0, false
		}
		amount, err := strconv.ParseInt(adj[amountStart:i], 10, 64)
		if err != nil {
			return 0, false
		}

		unitStart := i
		for i < len(adj) && ((adj[i] >= 'a' && adj[i] <= 'z') || (adj[i] >= 'A' && adj[i] <= 'Z')) {
			i++
		}
		if unitStart == i {
			return 0, false
		}
		unitDuration, ok := toolDurationUnit(strings.ToLower(adj[unitStart:i]))
		if !ok {
			return 0, false
		}
		total += time.Duration(sign*amount) * unitDuration

		for i < len(adj) && adj[i] == ' ' {
			i++
		}
		if i < len(adj) && adj[i] != '+' && adj[i] != '-' && (adj[i] < '0' || adj[i] > '9') {
			return 0, false
		}
	}
	return total, true
}

func toolDurationUnit(unit string) (time.Duration, bool) {
	switch unit {
	case "s", "sec", "second", "seconds":
		return time.Second, true
	case "m", "min", "minute", "minutes":
		return time.Minute, true
	case "h", "hour", "hours":
		return time.Hour, true
	case "d", "day", "days":
		return 24 * time.Hour, true
	case "w", "week", "weeks":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

// validateDateOrdering inspects edited commits whose author_date was changed
// and emits informational warnings when the new date is earlier than a parent
// commit's author_date, or later than a child commit's author_date. The
// commits slice is ordered newest-first per ListCommitsFromRefWithGraph.
func validateDateOrdering(changes []gitops.ForgeChange, commits []gitops.CommitInfo) []Warning {
	edits := make(map[string]time.Time)
	for _, change := range changes {
		if change.NewDate == nil {
			continue
		}
		edits[strings.ToLower(change.OriginalHash.String())] = *change.NewDate
	}
	if len(edits) == 0 {
		return nil
	}

	commitByHash := make(map[string]gitops.CommitInfo, len(commits))
	for _, commit := range commits {
		commitByHash[strings.ToLower(commit.Hash.String())] = commit
	}

	seen := make(map[string]bool)
	var warnings []Warning
	for hash, newDate := range edits {
		commit, ok := commitByHash[hash]
		if !ok {
			continue
		}
		for _, parentHash := range commit.Parents {
			parentKey := strings.ToLower(parentHash.String())
			parent, ok := commitByHash[parentKey]
			if !ok {
				continue
			}
			if newDate.Before(parent.AuthorDate) {
				key := "date_before_parent:" + hash
				if !seen[key] {
					seen[key] = true
					warnings = append(warnings, Warning{Code: "date_before_parent", Message: "edited author_date is earlier than parent commit's author_date", Hash: hash})
				}
			}
		}
		for _, candidate := range commits {
			matches := false
			for _, parentHash := range candidate.Parents {
				if strings.ToLower(parentHash.String()) == hash {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			if newDate.After(candidate.AuthorDate) {
				key := "date_after_child:" + hash
				if !seen[key] {
					seen[key] = true
					warnings = append(warnings, Warning{Code: "date_after_child", Message: "edited author_date is later than child commit's author_date", Hash: hash})
				}
			}
		}
	}
	return warnings
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

func jsonCommits(commits []gitops.CommitInfo, withStats bool) []Commit {
	out := make([]Commit, len(commits))
	for i, commit := range commits {
		parents := make([]string, len(commit.Parents))
		for j, parent := range commit.Parents {
			parents[j] = strings.ToLower(parent.String())
		}
		entry := Commit{
			Hash:        strings.ToLower(commit.Hash.String()),
			ShortHash:   commit.ShortHash,
			Parents:     parents,
			AuthorName:  commit.AuthorName,
			AuthorEmail: commit.AuthorEmail,
			AuthorDate:  commit.AuthorDate.Format(time.RFC3339),
			Subject:     strings.Split(commit.Message, "\n")[0],
			Message:     commit.Message,
		}
		if withStats {
			additions := commit.Additions
			deletions := commit.Deletions
			entry.Additions = &additions
			entry.Deletions = &deletions
		}
		out[i] = entry
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

const backupRefPrefix = "refs/backtrack-backup/"

func backupEntries(backups []string) []BackupEntry {
	entries := make([]BackupEntry, 0, len(backups))
	for _, backup := range backups {
		name := strings.TrimPrefix(backup, backupRefPrefix)
		entry := BackupEntry{Name: name, Ref: backup}
		if parsed, err := time.Parse("20060102-150405", name); err == nil {
			entry.CreatedAt = parsed.UTC().Format(time.RFC3339)
		}
		entries = append(entries, entry)
	}
	return entries
}

type backupResolveError struct {
	code    string
	message string
}

func (e *backupResolveError) Error() string {
	return e.message
}

// resolveBackupPrefix resolves a user-supplied backup identifier (bare name,
// full ref, or full ref with branch suffix) to the canonical backup prefix
// refs/backtrack-backup/<name> and validates it exists in the repository.
func resolveBackupPrefix(rewriter *gitops.HistoryRewriter, backupArg string) (string, *backupResolveError) {
	backups, err := rewriter.ListBackups()
	if err != nil {
		return "", &backupResolveError{code: "list_backups_failed", message: err.Error()}
	}
	if len(backups) == 0 {
		return "", &backupResolveError{code: "no_backups_found", message: "no backtrack backups exist in this repository"}
	}

	backupSet := make(map[string]bool, len(backups))
	for _, backup := range backups {
		backupSet[backup] = true
	}

	if backupArg == "" {
		latest, err := rewriter.LatestBackup()
		if err != nil {
			return "", &backupResolveError{code: "no_backups_found", message: err.Error()}
		}
		return latest, nil
	}

	name := strings.TrimSpace(backupArg)
	name = strings.TrimPrefix(name, backupRefPrefix)
	if idx := strings.Index(name, "/"); idx > 0 {
		name = name[:idx]
	}
	prefix := backupRefPrefix + name
	if !backupSet[prefix] {
		return "", &backupResolveError{code: "backup_not_found", message: fmt.Sprintf("backup %q not found", backupArg)}
	}
	return prefix, nil
}

func writeJSON(w io.Writer, value any, compact bool) {
	encoder := json.NewEncoder(w)
	if !compact {
		encoder.SetIndent("", "  ")
	}
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
