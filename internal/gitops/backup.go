package gitops

import (
	"fmt"
	"sort"
	"strings"
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

// RestoreFromBackup restores branch refs from the given backup prefix and
// returns the canonical branch refs (refs/heads/<branch>) that were restored.
func (hr *HistoryRewriter) RestoreFromBackup(backupPrefix string) ([]string, error) {
	refs, err := hr.repo.repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}

	restoreRefs := make(map[plumbing.ReferenceName]plumbing.Hash)
	restoreOrder := make([]plumbing.ReferenceName, 0)

	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if len(name) > len(backupPrefix) && name[:len(backupPrefix)] == backupPrefix {
			branchName := name[len(backupPrefix)+1:]
			originalName := plumbing.ReferenceName("refs/heads/" + branchName)
			if _, exists := restoreRefs[originalName]; !exists {
				restoreOrder = append(restoreOrder, originalName)
			}
			restoreRefs[originalName] = ref.Hash()
		}
		return nil
	}); err != nil {
		return nil, err
	}

	restored := make([]string, 0, len(restoreOrder))
	for refName, hash := range restoreRefs {
		newRef := plumbing.NewHashReference(refName, hash)
		if err := hr.repo.repo.Storer.SetReference(newRef); err != nil {
			return restored, fmt.Errorf("failed to restore reference %s: %w", refName, err)
		}
	}
	for _, refName := range restoreOrder {
		restored = append(restored, refName.String())
	}

	return restored, nil
}

func (hr *HistoryRewriter) ListBackups() ([]string, error) {
	refs, err := hr.repo.repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}

	backupSet := map[string]bool{}
	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if strings.HasPrefix(name, "refs/backtrack-backup/") {
			parts := strings.Split(name, "/")
			if len(parts) >= 3 {
				backupSet[strings.Join(parts[:3], "/")] = true
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	backups := make([]string, 0, len(backupSet))
	for backup := range backupSet {
		backups = append(backups, backup)
	}
	sort.Strings(backups)

	return backups, nil
}

func (hr *HistoryRewriter) LatestBackup() (string, error) {
	backups, err := hr.ListBackups()
	if err != nil {
		return "", err
	}
	if len(backups) == 0 {
		return "", fmt.Errorf("no backups found")
	}
	return backups[len(backups)-1], nil
}
