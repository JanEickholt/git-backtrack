package gitops

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func walkCommitHistory(repo *git.Repository, start plumbing.Hash, seen map[plumbing.Hash]bool, visit func(*object.Commit) error) error {
	stack := []plumbing.Hash{start}

	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if seen[hash] {
			continue
		}
		seen[hash] = true

		commit, err := repo.CommitObject(hash)
		if err != nil {
			continue
		}

		if err := visit(commit); err != nil {
			return err
		}

		for _, parentHash := range commit.ParentHashes {
			if !seen[parentHash] {
				stack = append(stack, parentHash)
			}
		}
	}

	return nil
}
