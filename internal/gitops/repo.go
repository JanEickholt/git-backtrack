package gitops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var ErrNotAGitRepository = errors.New("not a git repository")

type Repository struct {
	repo *git.Repository
	path string
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
	r.Reload()

	refs, err := r.repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to get references: %w", err)
	}

	seen := make(map[plumbing.Hash]bool)
	var commits []CommitInfo

	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		refName := ref.Name().String()
		if strings.HasPrefix(refName, "refs/backtrack-backup/") {
			return nil
		}

		hash := ref.Hash()
		if seen[hash] {
			return nil
		}

		return commitHistory(r.repo, hash, &commits, seen)
	}); err != nil {
		return nil, err
	}

	// Sort commits by date, newest first
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].AuthorDate.After(commits[j].AuthorDate)
	})

	return commits, nil
}

func (r *Repository) ListCommitsFromRef(refName string) ([]CommitInfo, error) {
	r.Reload()

	ref, err := r.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference %s: %w", refName, err)
	}

	seen := make(map[plumbing.Hash]bool)
	var commits []CommitInfo

	if err := commitHistory(r.repo, ref.Hash(), &commits, seen); err != nil {
		return nil, err
	}

	// Sort commits by date, newest first
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].AuthorDate.After(commits[j].AuthorDate)
	})

	return commits, nil
}

func (r *Repository) ListAllCommitsWithGraph() ([]CommitInfo, *Graph, error) {
	return r.listCommitsWithGraph("--exclude=refs/backtrack-backup/*", "--all")
}

func (r *Repository) ListCommitsFromRefWithGraph(refName string) ([]CommitInfo, *Graph, error) {
	return r.listCommitsWithGraph(refName)
}

func (r *Repository) listCommitsWithGraph(refArgs ...string) ([]CommitInfo, *Graph, error) {
	if err := r.Reload(); err != nil {
		return nil, nil, err
	}

	args := []string{"log", "--no-color", "--graph", "--topo-order", "--format=" + graphCommitMarker + "%H"}
	args = append(args, refArgs...)
	output, err := r.gitOutput(args...)
	if err != nil {
		return nil, nil, err
	}

	graph := ParseGraphRows(output)
	commits := make([]CommitInfo, 0, len(graph.CommitRows))
	seen := make(map[plumbing.Hash]bool)
	for _, rowIndex := range graph.CommitRows {
		row := graph.Rows[rowIndex]
		if seen[row.CommitHash] {
			continue
		}
		seen[row.CommitHash] = true

		commit, err := r.repo.CommitObject(row.CommitHash)
		if err != nil {
			continue
		}
		commits = append(commits, commitInfo(commit))
	}
	graph.AttachCommitIndexes(commits)

	return commits, graph, nil
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
	}
}

func (r *Repository) GetRepository() *git.Repository {
	return r.repo
}
