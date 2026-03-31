package gitops

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type combineGroup struct {
	Leader plumbing.Hash
	Hashes []plumbing.Hash
	Anchor plumbing.Hash
}

func hasReplayOperation(changes []ForgeChange) bool {
	for _, change := range changes {
		if change.Operation == ForgeDrop || change.Operation == ForgeCombine {
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
	combineGroups, combineMembers, err := buildCombineGroups(changes, commits)
	if err != nil {
		return nil, err
	}
	commitByHash := make(map[plumbing.Hash]*object.Commit, len(commits))
	for _, commit := range commits {
		commitByHash[commit.Hash] = commit
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
		if leader, ok := combineMembers[commit.Hash]; ok && leader != commit.Hash {
			continue
		}

		change, hasChange := changeMap[commit.Hash]
		if group, ok := combineGroups[commit.Hash]; ok {
			if err := hr.replayCombinedCommit(tmpDir, group, commitByHash, change, hasChange); err != nil {
				return nil, err
			}

			newHashStr, err := hr.gitOutput(tmpDir, "rev-parse", "HEAD")
			if err != nil {
				return nil, err
			}
			currentHash = plumbing.NewHash(strings.TrimSpace(newHashStr))
			for _, hash := range group.Hashes {
				result.ChangedRefs[hash] = currentHash
			}
			continue
		}

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

func buildCombineGroups(changes []ForgeChange, commits []*object.Commit) (map[plumbing.Hash]combineGroup, map[plumbing.Hash]plumbing.Hash, error) {
	commitIndex := make(map[plumbing.Hash]int, len(commits))
	for i, commit := range commits {
		commitIndex[commit.Hash] = i
	}

	groups := make(map[plumbing.Hash]combineGroup)
	members := make(map[plumbing.Hash]plumbing.Hash)
	seenGroups := make(map[string]bool)
	for _, change := range changes {
		if change.Operation != ForgeCombine {
			continue
		}

		hashes := append([]plumbing.Hash(nil), change.CombineGroup...)
		anchor := change.CombineAnchor
		if anchor == plumbing.ZeroHash {
			anchor = change.OriginalHash
		}
		if len(hashes) == 0 {
			hashes = []plumbing.Hash{change.OriginalHash}
		}
		if !containsHash(hashes, change.OriginalHash) {
			hashes = append(hashes, change.OriginalHash)
		}
		if len(hashes) < 2 {
			return nil, nil, fmt.Errorf("combining commits requires at least two commits")
		}
		for _, hash := range hashes {
			if _, ok := commitIndex[hash]; !ok {
				return nil, nil, fmt.Errorf("combined commit %s is not reachable from HEAD", hash.String()[:7])
			}
		}
		if _, ok := commitIndex[anchor]; !ok {
			return nil, nil, fmt.Errorf("fold anchor commit %s is not reachable from HEAD", anchor.String()[:7])
		}
		if !containsHash(hashes, anchor) {
			return nil, nil, fmt.Errorf("fold anchor commit %s is not part of its fold group", anchor.String()[:7])
		}

		sort.Slice(hashes, func(i, j int) bool {
			return commitIndex[hashes[i]] < commitIndex[hashes[j]]
		})
		hashes = uniqueHashes(hashes)
		if len(hashes) < 2 {
			return nil, nil, fmt.Errorf("combining commits requires at least two commits")
		}
		leader := hashes[0]
		key := hashGroupKey(hashes)
		if seenGroups[key] {
			continue
		}
		seenGroups[key] = true

		for _, hash := range hashes {
			if existingLeader, ok := members[hash]; ok && existingLeader != leader {
				return nil, nil, fmt.Errorf("commit %s is part of multiple combine groups", hash.String()[:7])
			}
			members[hash] = leader
		}
		groups[leader] = combineGroup{Leader: leader, Hashes: hashes, Anchor: anchor}
	}

	return groups, members, nil
}

func (hr *HistoryRewriter) replayCombinedCommit(dir string, group combineGroup, commitByHash map[plumbing.Hash]*object.Commit, change ForgeChange, hasChange bool) error {
	groupCommits := make([]*object.Commit, 0, len(group.Hashes))
	for _, hash := range group.Hashes {
		commit := commitByHash[hash]
		if commit == nil {
			return fmt.Errorf("combined commit %s is not available", hash.String()[:7])
		}
		if err := hr.runGit(dir, "cherry-pick", "--no-commit", hash.String()); err != nil {
			return fmt.Errorf("failed to replay combined commit %s: %w", hash.String()[:7], err)
		}
		groupCommits = append(groupCommits, commit)
	}

	anchorCommit := commitByHash[group.Anchor]
	if anchorCommit == nil {
		anchorCommit = groupCommits[0]
	}
	messageFile, err := hr.writeReplayMessage(dir, anchorCommit, change, hasChange)
	if err != nil {
		return err
	}
	env := replayCommitEnv(anchorCommit, change, hasChange)
	if err := hr.runGitEnv(dir, env, "commit", "--allow-empty", "-F", messageFile); err != nil {
		return fmt.Errorf("failed to create combined commit %s: %w", group.Leader.String()[:7], err)
	}
	return nil
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
	return writeMessageFile(dir, message)
}

func writeMessageFile(dir string, message string) (string, error) {
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

func containsHash(hashes []plumbing.Hash, target plumbing.Hash) bool {
	for _, hash := range hashes {
		if hash == target {
			return true
		}
	}
	return false
}

func uniqueHashes(hashes []plumbing.Hash) []plumbing.Hash {
	unique := hashes[:0]
	var previous plumbing.Hash
	for i, hash := range hashes {
		if i > 0 && hash == previous {
			continue
		}
		unique = append(unique, hash)
		previous = hash
	}
	return unique
}

func hashGroupKey(hashes []plumbing.Hash) string {
	parts := make([]string, len(hashes))
	for i, hash := range hashes {
		parts[i] = hash.String()
	}
	return strings.Join(parts, ",")
}
