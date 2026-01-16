package gitops

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type ForgeChange struct {
	OriginalHash plumbing.Hash
	NewAuthor    *AuthorInfo
	NewMessage   string
	NewDate      *time.Time
}

func (c ForgeChange) HasChanges() bool {
	return c.NewAuthor != nil || c.NewDate != nil || c.NewMessage != ""
}

type AuthorInfo struct {
	Name  string
	Email string
}

type RewriteResult struct {
	BackupRef   string
	ChangedRefs map[plumbing.Hash]plumbing.Hash
	Errors      []error
}

type HistoryRewriter struct {
	repo *Repository
}

func NewHistoryRewriter(repo *Repository) *HistoryRewriter {
	return &HistoryRewriter{repo: repo}
}

func (hr *HistoryRewriter) CreateBackup(refName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupRefName := fmt.Sprintf("refs/backtrack-backup/%s", timestamp)

	ref, err := hr.repo.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return "", fmt.Errorf("failed to get reference %s: %w", refName, err)
	}

	backupRef := plumbing.NewHashReference(plumbing.ReferenceName(backupRefName), ref.Hash())
	if err := hr.repo.repo.Storer.SetReference(backupRef); err != nil {
		return "", fmt.Errorf("failed to create backup reference: %w", err)
	}

	return backupRefName, nil
}

func (hr *HistoryRewriter) CreateFullBackup() (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupPrefix := fmt.Sprintf("refs/backtrack-backup/%s", timestamp)

	branches, err := hr.repo.ListBranches()
	if err != nil {
		return "", fmt.Errorf("failed to list branches: %w", err)
	}

	for _, branch := range branches {
		refName := plumbing.ReferenceName("refs/heads/" + branch)
		ref, err := hr.repo.repo.Reference(refName, true)
		if err != nil {
			continue
		}

		backupRef := plumbing.NewHashReference(
			plumbing.ReferenceName(backupPrefix+"/"+branch),
			ref.Hash(),
		)
		if err := hr.repo.repo.Storer.SetReference(backupRef); err != nil {
			return "", fmt.Errorf("failed to backup branch %s: %w", branch, err)
		}
	}

	return backupPrefix, nil
}

func (hr *HistoryRewriter) ApplyChanges(changes []ForgeChange) (*RewriteResult, error) {
	if len(changes) == 0 {
		return &RewriteResult{}, nil
	}

	result := &RewriteResult{
		ChangedRefs: make(map[plumbing.Hash]plumbing.Hash),
	}

	signingConfig, _ := hr.repo.GetSigningConfig()
	useSigning := signingConfig != nil && signingConfig.SignCommits

	hashMap := make(map[plumbing.Hash]plumbing.Hash)
	changeMap := make(map[plumbing.Hash]ForgeChange)
	for _, change := range changes {
		changeMap[change.OriginalHash] = change
	}

	head, err := hr.repo.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commits, err := hr.collectCommits(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to collect commits: %w", err)
	}

	for i := len(commits) - 1; i >= 0; i-- {
		commit := commits[i]

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

		commitAuthorEmail := newCommit.Author.Email
		shouldSign := useSigning
		if shouldSign {
			if myID, err := hr.repo.GetUserIdentity(); err == nil {
				shouldSign = myID.Email != "" && myID.Email == commitAuthorEmail
			} else {
				shouldSign = false
			}
		}
		if shouldSign {
			if signedHash, err := hr.repo.SignCommit(newHash); err == nil {
				newHash = signedHash
			}
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

	stack := []plumbing.Hash{startHash}

	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if seen[hash] {
			continue
		}
		seen[hash] = true

		commit, err := hr.repo.repo.CommitObject(hash)
		if err != nil {
			continue
		}

		commits = append(commits, commit)

		for _, parentHash := range commit.ParentHashes {
			if !seen[parentHash] {
				stack = append(stack, parentHash)
			}
		}
	}

	return commits, nil
}

func (hr *HistoryRewriter) RestoreFromBackup(backupPrefix string) error {
	refs, err := hr.repo.repo.References()
	if err != nil {
		return fmt.Errorf("failed to list references: %w", err)
	}

	restoreRefs := make(map[plumbing.ReferenceName]plumbing.Hash)

	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if len(name) > len(backupPrefix) && name[:len(backupPrefix)] == backupPrefix {
			branchName := name[len(backupPrefix)+1:]
			originalName := plumbing.ReferenceName("refs/heads/" + branchName)
			restoreRefs[originalName] = ref.Hash()
		}
		return nil
	}); err != nil {
		return err
	}

	for refName, hash := range restoreRefs {
		newRef := plumbing.NewHashReference(refName, hash)
		if err := hr.repo.repo.Storer.SetReference(newRef); err != nil {
			return fmt.Errorf("failed to restore reference %s: %w", refName, err)
		}
	}

	return nil
}

func (hr *HistoryRewriter) ListBackups() ([]string, error) {
	refs, err := hr.repo.repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}

	var backups []string
	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if len(name) > 20 && name[:20] == "refs/backtrack-backup" {
			backups = append(backups, name)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return backups, nil
}

