package gitops

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func hasDropOperation(changes []ForgeChange) bool {
	for _, change := range changes {
		if change.Operation == ForgeDrop {
			return true
		}
	}
	return false
}

func (hr *HistoryRewriter) applyChangesWithDrop(changes []ForgeChange) (*RewriteResult, error) {
	result := &RewriteResult{
		ChangedRefs: make(map[plumbing.Hash]plumbing.Hash),
	}

	head, err := hr.repo.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return nil, fmt.Errorf("dropping commits requires HEAD to point at a branch")
	}

	collected, err := hr.collectCommits(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to collect commits: %w", err)
	}
	commits := make([]*object.Commit, len(collected))
	for i := range collected {
		commits[len(collected)-1-i] = collected[i]
	}

	changeMap := make(map[plumbing.Hash]ForgeChange)
	for _, change := range changes {
		changeMap[change.OriginalHash] = change
	}

	firstRewrite := -1
	for i, commit := range commits {
		if change, ok := changeMap[commit.Hash]; ok && change.HasChanges() {
			firstRewrite = i
			break
		}
	}
	if firstRewrite == -1 {
		return result, nil
	}
	if len(commits[firstRewrite].ParentHashes) == 0 {
		return nil, fmt.Errorf("dropping or replaying from the root commit is not supported yet")
	}
	for _, commit := range commits[firstRewrite:] {
		if len(commit.ParentHashes) != 1 {
			return nil, fmt.Errorf("dropping commits across merge commits is not supported yet: %s", commit.Hash.String()[:7])
		}
	}

	tmpDir, err := os.MkdirTemp("", "git-backtrack-replay-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary replay worktree: %w", err)
	}
	os.RemoveAll(tmpDir)
	defer os.RemoveAll(tmpDir)

	baseHash := commits[firstRewrite].ParentHashes[0].String()
	if err := hr.runGit(hr.repo.path, "worktree", "add", "--detach", tmpDir, baseHash); err != nil {
		return nil, err
	}
	defer hr.runGit(hr.repo.path, "worktree", "remove", "--force", tmpDir)

	var currentHash plumbing.Hash = commits[firstRewrite].ParentHashes[0]
	for _, commit := range commits[firstRewrite:] {
		change, hasChange := changeMap[commit.Hash]
		if hasChange && change.Operation == ForgeDrop {
			result.ChangedRefs[commit.Hash] = currentHash
			continue
		}

		if err := hr.runGit(tmpDir, "cherry-pick", "--no-commit", commit.Hash.String()); err != nil {
			return nil, fmt.Errorf("failed to replay commit %s: %w", commit.Hash.String()[:7], err)
		}

		messageFile, err := hr.writeReplayMessage(tmpDir, commit, change, hasChange)
		if err != nil {
			return nil, err
		}

		env := replayCommitEnv(commit, change, hasChange)
		if err := hr.runGitEnv(tmpDir, env, "commit", "--allow-empty", "-F", messageFile); err != nil {
			return nil, fmt.Errorf("failed to create replayed commit %s: %w", commit.Hash.String()[:7], err)
		}

		newHashStr, err := hr.gitOutput(tmpDir, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		currentHash = plumbing.NewHash(strings.TrimSpace(newHashStr))
		result.ChangedRefs[commit.Hash] = currentHash
	}

	if err := hr.runGit(hr.repo.path, "update-ref", head.Name().String(), currentHash.String(), head.Hash().String()); err != nil {
		return nil, fmt.Errorf("failed to update %s: %w", head.Name().Short(), err)
	}
	if err := hr.repo.Reload(); err != nil {
		return nil, err
	}

	return result, nil
}

func replayCommitEnv(commit *object.Commit, change ForgeChange, hasChange bool) []string {
	author := commit.Author
	committer := commit.Committer

	if hasChange && change.NewAuthor != nil {
		author.Name = change.NewAuthor.Name
		author.Email = change.NewAuthor.Email
		committer.Name = change.NewAuthor.Name
		committer.Email = change.NewAuthor.Email
	}
	if hasChange && change.NewDate != nil {
		author.When = *change.NewDate
		committer.When = *change.NewDate
	}

	return append(os.Environ(),
		"GIT_AUTHOR_NAME="+author.Name,
		"GIT_AUTHOR_EMAIL="+author.Email,
		"GIT_AUTHOR_DATE="+author.When.Format(time.RFC3339),
		"GIT_COMMITTER_NAME="+committer.Name,
		"GIT_COMMITTER_EMAIL="+committer.Email,
		"GIT_COMMITTER_DATE="+committer.When.Format(time.RFC3339),
	)
}

func (hr *HistoryRewriter) writeReplayMessage(dir string, commit *object.Commit, change ForgeChange, hasChange bool) (string, error) {
	message := commit.Message
	if hasChange && change.NewMessage != "" {
		message = change.NewMessage
	}
	file, err := os.CreateTemp(dir, "git-backtrack-message-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary commit message: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(message); err != nil {
		return "", fmt.Errorf("failed to write temporary commit message: %w", err)
	}
	return file.Name(), nil
}
