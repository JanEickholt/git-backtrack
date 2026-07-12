package gitops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var ErrNotAGitRepository = errors.New("not a git repository")

type Repository struct {
	repo *git.Repository
	path string
}

type LogFilter struct {
	Since  string
	Before string
	Author string
	Email  string
}

func (f LogFilter) GitArgs() []string {
	args := make([]string, 0, 4)
	if f.Since != "" {
		args = append(args, "--since="+f.Since)
	}
	if f.Before != "" {
		args = append(args, "--before="+f.Before)
	}
	if f.Author != "" {
		args = append(args, "--author="+f.Author)
	}
	if f.Email != "" {
		args = append(args, "--author="+f.Email)
	}
	return args
}

func Open(path string) (*Repository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, ErrNotAGitRepository
		}
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}
	return &Repository{repo: repo, path: path}, nil
}

func (r *Repository) Reload() error {
	repo, err := git.PlainOpen(r.path)
	if err != nil {
		return fmt.Errorf("failed to reopen repository: %w", err)
	}
	r.repo = repo
	return nil
}

func (r *Repository) ListAllCommits() ([]CommitInfo, error) {
	return r.ListAllCommitsWithStats(true)
}

func (r *Repository) ListAllCommitsWithStats(includeStats bool) ([]CommitInfo, error) {
	return r.listCommitInfoFromGit(includeStats, "--exclude=refs/backtrack-backup/*", "--all")
}

func (r *Repository) ListCommitsFromRef(refName string) ([]CommitInfo, error) {
	return r.ListCommitsFromRefWithStats(refName, true)
}

func (r *Repository) ListCommitsFromRefWithStats(refName string, includeStats bool) ([]CommitInfo, error) {
	commits, err := r.listCommitInfoFromGit(includeStats, refName)
	if err != nil {
		return nil, err
	}

	branchName := ""
	if strings.HasPrefix(refName, "refs/heads/") {
		branchName = strings.TrimPrefix(refName, "refs/heads/")
	}
	r.markUnpushedCommits(commits, branchName)

	return commits, nil
}

func (r *Repository) ListCommitsFromRefWindowWithStats(refName string, includeStats bool, limit int, offset int, filter LogFilter) ([]CommitInfo, error) {
	filterArgs := filter.GitArgs()
	if limit <= 0 && offset <= 0 && len(filterArgs) == 0 {
		return r.ListCommitsFromRefWithStats(refName, includeStats)
	}

	args := []string{"--topo-order"}
	args = append(args, filterArgs...)
	if offset > 0 {
		args = append(args, "--skip="+strconv.Itoa(offset))
	}
	if limit > 0 {
		args = append(args, "--max-count="+strconv.Itoa(limit))
	}
	args = append(args, refName)

	commits, err := r.listCommitInfoFromGitPreservingOrder(includeStats, args...)
	if err != nil {
		return nil, err
	}

	branchName := ""
	if strings.HasPrefix(refName, "refs/heads/") {
		branchName = strings.TrimPrefix(refName, "refs/heads/")
	}
	r.markUnpushedCommits(commits, branchName)

	return commits, nil
}

func (r *Repository) CountCommitsFromRef(refName string, filter LogFilter) (int, error) {
	if err := r.Reload(); err != nil {
		return 0, err
	}
	args := []string{"rev-list", "--count"}
	args = append(args, filter.GitArgs()...)
	args = append(args, refName)
	output, err := r.gitOutput(args...)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("parse commit count: %w", err)
	}
	return count, nil
}

func (r *Repository) ListAllCommitsWithGraph() ([]CommitInfo, *Graph, error) {
	return r.ListAllCommitsWithGraphOrder(DefaultGraphOrder())
}

func (r *Repository) ListCommitsFromRefWithGraph(refName string) ([]CommitInfo, *Graph, error) {
	return r.ListCommitsFromRefWithGraphOrder(refName, DefaultGraphOrder())
}

func (r *Repository) ListAllCommitsWithGraphOrder(order GraphOrder) ([]CommitInfo, *Graph, error) {
	return r.listCommitsWithGraph(order, "", "--exclude=refs/backtrack-backup/*", "--all")
}

func (r *Repository) ListCommitsFromRefWithGraphOrder(refName string, order GraphOrder) ([]CommitInfo, *Graph, error) {
	branchName := ""
	if strings.HasPrefix(refName, "refs/heads/") {
		branchName = strings.TrimPrefix(refName, "refs/heads/")
	}
	return r.listCommitsWithGraph(order, branchName, refName)
}

func (r *Repository) listCommitsWithGraph(order GraphOrder, branchName string, refArgs ...string) ([]CommitInfo, *Graph, error) {
	return r.listCommitsWithGraphAndStats(order, true, branchName, refArgs...)
}

func (r *Repository) ListAllCommitsWithGraphOrderAndStats(order GraphOrder, includeStats bool) ([]CommitInfo, *Graph, error) {
	return r.listCommitsWithGraphAndStats(order, includeStats, "", "--exclude=refs/backtrack-backup/*", "--all")
}

func (r *Repository) ListCommitsFromRefWithGraphOrderAndStats(refName string, order GraphOrder, includeStats bool) ([]CommitInfo, *Graph, error) {
	branchName := ""
	if strings.HasPrefix(refName, "refs/heads/") {
		branchName = strings.TrimPrefix(refName, "refs/heads/")
	}
	return r.listCommitsWithGraphAndStats(order, includeStats, branchName, refName)
}

func (r *Repository) listCommitsWithGraphAndStats(order GraphOrder, includeStats bool, branchName string, refArgs ...string) ([]CommitInfo, *Graph, error) {
	if err := r.Reload(); err != nil {
		return nil, nil, err
	}

	args := []string{"log", "--no-color", "--graph"}
	args = append(args, order.GitLogArgs()...)
	args = append(args, "--format="+graphCommitMarker+"%H")
	args = append(args, refArgs...)
	output, err := r.gitOutput(args...)
	if err != nil {
		return nil, nil, err
	}

	graph := ParseGraphRows(output)
	infoArgs := append([]string{}, order.GitLogArgs()...)
	infoArgs = append(infoArgs, refArgs...)
	commitInfos, err := r.listCommitInfoFromGit(includeStats, infoArgs...)
	if err != nil {
		return nil, nil, err
	}
	infoByHash := make(map[plumbing.Hash]CommitInfo, len(commitInfos))
	for _, commit := range commitInfos {
		infoByHash[commit.Hash] = commit
	}

	commits := make([]CommitInfo, 0, len(graph.CommitRows))
	seen := make(map[plumbing.Hash]bool)
	for _, rowIndex := range graph.CommitRows {
		row := graph.Rows[rowIndex]
		if seen[row.CommitHash] {
			continue
		}
		seen[row.CommitHash] = true

		commit, ok := infoByHash[row.CommitHash]
		if !ok {
			continue
		}
		commits = append(commits, commit)
	}
	graph.AttachCommitIndexes(commits)

	if branchName != "" {
		r.markUnpushedCommits(commits, branchName)
	}

	return commits, graph, nil
}

func (r *Repository) listCommitInfoFromGit(includeStats bool, refArgs ...string) ([]CommitInfo, error) {
	return r.listCommitInfoFromGitSorted(includeStats, true, refArgs...)
}

func (r *Repository) listCommitInfoFromGitPreservingOrder(includeStats bool, refArgs ...string) ([]CommitInfo, error) {
	return r.listCommitInfoFromGitSorted(includeStats, false, refArgs...)
}

func (r *Repository) listCommitInfoFromGitSorted(includeStats bool, sortByAuthorDate bool, refArgs ...string) ([]CommitInfo, error) {
	if err := r.Reload(); err != nil {
		return nil, err
	}

	args := []string{
		"log",
		"--no-color",
		"--format=" + commitRecordMarker + "%H%x1f%an%x1f%ae%x1f%aI%x1f%P%x1f%B%x1f",
	}
	if includeStats {
		args = append(args, "--numstat", "--diff-merges=first-parent")
	}
	args = append(args, refArgs...)
	output, err := r.gitOutput(args...)
	if err != nil {
		return nil, err
	}

	commits := parseCommitInfoLog(output, includeStats)
	if sortByAuthorDate {
		sort.Slice(commits, func(i, j int) bool {
			return commits[i].AuthorDate.After(commits[j].AuthorDate)
		})
	}
	return commits, nil
}

const commitRecordMarker = "\x1e"

func parseCommitInfoLog(output string, includeStats bool) []CommitInfo {
	records := strings.Split(output, commitRecordMarker)
	commits := make([]CommitInfo, 0, len(records))
	seen := make(map[plumbing.Hash]bool)

	for _, record := range records {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}

		fields := strings.SplitN(record, "\x1f", 7)
		if len(fields) != 7 {
			continue
		}

		hash := plumbing.NewHash(fields[0])
		if hash.IsZero() || seen[hash] {
			continue
		}
		seen[hash] = true

		date, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			date = time.Time{}
		}

		message := strings.TrimRight(fields[5], "\n")
		additions, deletions := parseNumstatTotals(fields[6])

		commits = append(commits, CommitInfo{
			Hash:        hash,
			ShortHash:   hash.String()[:7],
			AuthorName:  fields[1],
			AuthorEmail: fields[2],
			AuthorDate:  date,
			Message:     message,
			Parents:     parseParentHashes(fields[4]),
			Additions:   additions,
			Deletions:   deletions,
			StatsLoaded: includeStats,
		})
	}

	return commits
}

func parseParentHashes(value string) []plumbing.Hash {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Fields(value)
	parents := make([]plumbing.Hash, 0, len(parts))
	for _, part := range parts {
		parents = append(parents, plumbing.NewHash(part))
	}
	return parents
}

func parseNumstatTotals(value string) (int, int) {
	additions := 0
	deletions := 0
	for _, line := range strings.Split(value, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if add, err := strconv.Atoi(fields[0]); err == nil {
			additions += add
		}
		if del, err := strconv.Atoi(fields[1]); err == nil {
			deletions += del
		}
	}
	return additions, deletions
}

func (r *Repository) LoadCommitStats(hashes []plumbing.Hash) (map[plumbing.Hash]CommitStats, error) {
	if len(hashes) == 0 {
		return map[plumbing.Hash]CommitStats{}, nil
	}

	args := []string{
		"log",
		"--no-color",
		"--no-walk",
		"--numstat",
		"--diff-merges=first-parent",
		"--format=" + commitRecordMarker + "%H%x1f%x1f",
	}
	for _, hash := range hashes {
		args = append(args, hash.String())
	}
	output, err := r.gitOutput(args...)
	if err != nil {
		return nil, err
	}
	return parseCommitStatsLog(output), nil
}

func parseCommitStatsLog(output string) map[plumbing.Hash]CommitStats {
	records := strings.Split(output, commitRecordMarker)
	stats := make(map[plumbing.Hash]CommitStats, len(records))
	for _, record := range records {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 3)
		if len(fields) != 3 {
			continue
		}
		hash := plumbing.NewHash(fields[0])
		if hash.IsZero() {
			continue
		}
		additions, deletions := parseNumstatTotals(fields[2])
		stats[hash] = CommitStats{Additions: additions, Deletions: deletions}
	}
	return stats
}

func (r *Repository) gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.path}, args...)...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), text, err)
	}
	return string(output), nil
}

func (r *Repository) SwitchBranch(branchName string) error {
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	refName := plumbing.ReferenceName("refs/heads/" + branchName)
	err = w.Checkout(&git.CheckoutOptions{
		Branch: refName,
	})
	if err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	return nil
}

func (r *Repository) GetHead() (*plumbing.Reference, error) {
	return r.repo.Head()
}

func (r *Repository) GetReference(refName string) (*plumbing.Reference, error) {
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r.repo.Reference(plumbing.ReferenceName(refName), true)
}

func (r *Repository) ListBranches() ([]string, error) {
	branches, err := r.repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var names []string
	if err := branches.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	}); err != nil {
		return nil, err
	}

	return names, nil
}

func commitHistory(repo *git.Repository, start plumbing.Hash, commits *[]CommitInfo, seen map[plumbing.Hash]bool) error {
	return walkCommitHistory(repo, start, seen, func(commit *object.Commit) error {
		*commits = append(*commits, commitInfo(commit))
		return nil
	})
}

func commitInfo(commit *object.Commit) CommitInfo {
	stats, _ := commit.Stats()
	additions := 0
	deletions := 0
	for _, stat := range stats {
		additions += stat.Addition
		deletions += stat.Deletion
	}

	return CommitInfo{
		Hash:        commit.Hash,
		ShortHash:   commit.Hash.String()[:7],
		AuthorName:  commit.Author.Name,
		AuthorEmail: commit.Author.Email,
		AuthorDate:  commit.Author.When,
		Message:     commit.Message,
		Parents:     commit.ParentHashes,
		Additions:   additions,
		Deletions:   deletions,
		StatsLoaded: true,
	}
}

func (r *Repository) markUnpushedCommits(commits []CommitInfo, branchName string) error {
	if branchName == "" {
		return nil
	}
	upstream, err := r.gitOutput("rev-parse", "--abbrev-ref", branchName+"@{upstream}")
	upstream = strings.TrimSpace(upstream)
	if err != nil || upstream == "" {
		return nil
	}
	output, err := r.gitOutput("rev-list", "--no-color", "refs/heads/"+branchName, "--not", upstream)
	if err != nil {
		return nil
	}
	unpushed := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		unpushed[line] = true
	}
	for i := range commits {
		if unpushed[commits[i].Hash.String()] {
			commits[i].IsUnpushed = true
		}
	}
	return nil
}

func (r *Repository) GetRepository() *git.Repository {
	return r.repo
}
