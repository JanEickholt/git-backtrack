package gitops

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type HistoryRewriter struct {
	repo *Repository
}

func NewHistoryRewriter(repo *Repository) *HistoryRewriter {
	return &HistoryRewriter{repo: repo}
}

func (hr *HistoryRewriter) ApplyChanges(changes []ForgeChange) (*RewriteResult, error) {
	if len(changes) == 0 {
		return &RewriteResult{}, nil
	}

	if hasReplayOperation(changes) {
		return hr.applyChangesWithDrop(changes)
	}

	result := &RewriteResult{
		ChangedRefs: make(map[plumbing.Hash]plumbing.Hash),
	}

	hashMap := make(map[plumbing.Hash]plumbing.Hash)
	changeMap := make(map[plumbing.Hash]ForgeChange)
	for _, change := range changes {
		changeMap[change.OriginalHash] = change
	}

	head, err := hr.repo.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commits, err := hr.collectCommitsParentFirst(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to collect commits: %w", err)
	}

	for _, commit := range commits {

		change, needsForge := changeMap[commit.Hash]

		var newCommit *object.Commit
		var newHash plumbing.Hash

		if needsForge {
			newCommit, err = hr.forgeCommit(commit, change, hashMap)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to forge commit %s: %w", commit.Hash.String()[:7], err))
				continue
			}
		} else {
			hasRewrittenParents := false
			for _, parentHash := range commit.ParentHashes {
				if _, rewritten := hashMap[parentHash]; rewritten {
					hasRewrittenParents = true
					break
				}
			}

			if hasRewrittenParents {
				newCommit, err = hr.recreateCommit(commit, hashMap)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("failed to recreate commit %s: %w", commit.Hash.String()[:7], err))
					continue
				}
			} else {
				continue
			}
		}

		newHash = newCommit.Hash

		shouldSign, _, _ := hr.repo.ShouldSignForAuthor(newCommit.Author.Email)
		if shouldSign {
			commitAuthorEmail := newCommit.Author.Email
			signedHash, err := hr.repo.SignCommitForAuthor(newHash, commitAuthorEmail)
			if err != nil {
				return nil, fmt.Errorf("failed to sign commit %s for %s: %w", commit.Hash.String()[:7], commitAuthorEmail, err)
			}
			newHash = signedHash
		}

		hashMap[commit.Hash] = newHash
		result.ChangedRefs[commit.Hash] = newHash
	}

	if newHeadHash, ok := hashMap[head.Hash()]; ok {
		newHeadRef := plumbing.NewHashReference(head.Name(), newHeadHash)
		if err := hr.repo.repo.Storer.SetReference(newHeadRef); err != nil {
			return nil, fmt.Errorf("failed to update HEAD: %w", err)
		}
	}

	branches, err := hr.repo.ListBranches()
	if err == nil {
		for _, branch := range branches {
			refName := plumbing.ReferenceName("refs/heads/" + branch)
			ref, err := hr.repo.repo.Reference(refName, true)
			if err != nil {
				continue
			}
			if newHash, ok := hashMap[ref.Hash()]; ok {
				newRef := plumbing.NewHashReference(refName, newHash)
				hr.repo.repo.Storer.SetReference(newRef)
			}
		}
	}

	return result, nil
}

func (hr *HistoryRewriter) forgeCommit(original *object.Commit, change ForgeChange, hashMap map[plumbing.Hash]plumbing.Hash) (*object.Commit, error) {
	author := original.Author
	committer := original.Committer

	if change.NewAuthor != nil {
		author.Name = change.NewAuthor.Name
		author.Email = change.NewAuthor.Email
		committer.Name = change.NewAuthor.Name
		committer.Email = change.NewAuthor.Email
	}
	if change.NewDate != nil {
		author.When = *change.NewDate
		committer.When = *change.NewDate
	}

	message := original.Message
	if change.NewMessage != "" {
		message = change.NewMessage
	}

	parentHashes := make([]plumbing.Hash, len(original.ParentHashes))
	for i, parentHash := range original.ParentHashes {
		if newHash, ok := hashMap[parentHash]; ok {
			parentHashes[i] = newHash
		} else {
			parentHashes[i] = parentHash
		}
	}

	commit := &object.Commit{
		Author:       author,
		Committer:    committer,
		Message:      message,
		TreeHash:     original.TreeHash,
		ParentHashes: parentHashes,
	}

	obj := hr.repo.repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return nil, fmt.Errorf("failed to encode commit: %w", err)
	}

	newHash, err := hr.repo.repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to store commit: %w", err)
	}

	commit.Hash = newHash
	return commit, nil
}

func (hr *HistoryRewriter) recreateCommit(original *object.Commit, hashMap map[plumbing.Hash]plumbing.Hash) (*object.Commit, error) {
	parentHashes := make([]plumbing.Hash, len(original.ParentHashes))
	for i, parentHash := range original.ParentHashes {
		if newHash, ok := hashMap[parentHash]; ok {
			parentHashes[i] = newHash
		} else {
			parentHashes[i] = parentHash
		}
	}

	commit := &object.Commit{
		Author:       original.Author,
		Committer:    original.Committer,
		Message:      original.Message,
		TreeHash:     original.TreeHash,
		ParentHashes: parentHashes,
	}

	obj := hr.repo.repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return nil, fmt.Errorf("failed to encode commit: %w", err)
	}

	newHash, err := hr.repo.repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to store commit: %w", err)
	}

	commit.Hash = newHash
	return commit, nil
}

func (hr *HistoryRewriter) collectCommits(startHash plumbing.Hash) ([]*object.Commit, error) {
	var commits []*object.Commit
	seen := make(map[plumbing.Hash]bool)
	err := walkCommitHistory(hr.repo.repo, startHash, seen, func(commit *object.Commit) error {
		commits = append(commits, commit)
		return nil
	})
	return commits, err
}

func (hr *HistoryRewriter) collectCommitsParentFirst(startHash plumbing.Hash) ([]*object.Commit, error) {
	var commits []*object.Commit
	seen := make(map[plumbing.Hash]bool)

	var visit func(plumbing.Hash) error
	visit = func(hash plumbing.Hash) error {
		if seen[hash] {
			return nil
		}
		seen[hash] = true

		commit, err := hr.repo.repo.CommitObject(hash)
		if err != nil {
			return err
		}
		for _, parentHash := range commit.ParentHashes {
			if err := visit(parentHash); err != nil {
				return err
			}
		}

		commits = append(commits, commit)
		return nil
	}

	if err := visit(startHash); err != nil {
		return nil, err
	}
	return commits, nil
}
