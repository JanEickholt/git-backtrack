package gitops

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

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
