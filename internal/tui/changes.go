package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Jan/git-backtrack/internal/gitops"
	"github.com/go-git/go-git/v5/plumbing"
)

var foldColors = []string{"11", "13", "10", "12", "14", "9"}

type foldDisplay struct {
	ID     int
	Label  string
	Color  string
	Hashes []plumbing.Hash
}

type foldDisplayIndex struct {
	ByHash map[string]foldDisplay
	Groups []foldDisplay
}

func (m *Model) setChange(change gitops.ForgeChange) {
	if m.editMap == nil {
		m.editMap = make(map[string]*gitops.ForgeChange)
	}
	hash := change.OriginalHash.String()
	for i := range m.editQueue {
		if m.editQueue[i].OriginalHash.String() == hash {
			m.editQueue[i] = change
			m.rebuildEditMap()
			return
		}
	}
	m.editQueue = append(m.editQueue, change)
	m.rebuildEditMap()
}

func (m *Model) removeChange(hash string) {
	if m.editMap == nil || m.editMap[hash] == nil {
		return
	}
	newQueue := make([]gitops.ForgeChange, 0, len(m.editQueue))
	for _, change := range m.editQueue {
		if change.OriginalHash.String() != hash {
			newQueue = append(newQueue, change)
		}
	}
	m.editQueue = newQueue
	m.rebuildEditMap()
}

func (m Model) selectedCommitInfos() []gitops.CommitInfo {
	selected := make([]gitops.CommitInfo, 0)
	for _, commit := range m.commits {
		if m.selectedCommits[commit.Hash.String()] {
			selected = append(selected, commit)
		}
	}
	return selected
}

func (m *Model) toggleDropForCommits(commits []gitops.CommitInfo) {
	if len(commits) == 0 {
		return
	}

	allDropped := true
	for _, commit := range commits {
		change := m.editMap[commit.Hash.String()]
		if change == nil || change.Operation != gitops.ForgeDrop {
			allDropped = false
			break
		}
	}

	for _, commit := range commits {
		hash := commit.Hash.String()
		if allDropped {
			if change := m.editMap[hash]; change != nil && change.Operation == gitops.ForgeDrop {
				m.removeChange(hash)
			}
			continue
		}
		m.setChange(gitops.ForgeChange{
			OriginalHash: commit.Hash,
			Operation:    gitops.ForgeDrop,
		})
	}
}

func (m *Model) toggleCombineForCommits(commits []gitops.CommitInfo, anchor plumbing.Hash) {
	if len(commits) < 2 {
		return
	}

	combineGroup := make([]plumbing.Hash, len(commits))
	for i, commit := range commits {
		combineGroup[i] = commit.Hash
	}
	if !slices.Contains(combineGroup, anchor) {
		return
	}

	allCombined := true
	for _, commit := range commits {
		change := m.editMap[commit.Hash.String()]
		if change == nil || change.Operation != gitops.ForgeCombine || !slices.Equal(change.CombineGroup, combineGroup) || change.CombineAnchor != anchor {
			allCombined = false
			break
		}
	}

	for _, commit := range commits {
		hash := commit.Hash.String()
		if allCombined {
			if change := m.editMap[hash]; change != nil && change.Operation == gitops.ForgeCombine {
				m.removeChange(hash)
			}
			continue
		}
		m.setChange(gitops.ForgeChange{
			OriginalHash:  commit.Hash,
			Operation:     gitops.ForgeCombine,
			CombineGroup:  append([]plumbing.Hash(nil), combineGroup...),
			CombineAnchor: anchor,
		})
	}
}

func (m Model) foldDisplayIndex() foldDisplayIndex {
	index := foldDisplayIndex{ByHash: make(map[string]foldDisplay)}
	seen := make(map[string]bool)

	addGroup := func(change *gitops.ForgeChange) {
		if change == nil || change.Operation != gitops.ForgeCombine {
			return
		}
		hashes := append([]plumbing.Hash(nil), change.CombineGroup...)
		if len(hashes) == 0 {
			hashes = []plumbing.Hash{change.OriginalHash}
		}
		key := combineGroupKey(hashes)
		if seen[key] {
			return
		}
		seen[key] = true

		id := len(index.Groups) + 1
		group := foldDisplay{
			ID:     id,
			Label:  fmt.Sprintf("[fold %d] ", id),
			Color:  foldColors[(id-1)%len(foldColors)],
			Hashes: hashes,
		}
		index.Groups = append(index.Groups, group)
		for _, hash := range hashes {
			if _, ok := index.ByHash[hash.String()]; !ok {
				index.ByHash[hash.String()] = group
			}
		}
	}

	for _, commit := range m.commits {
		addGroup(m.editMap[commit.Hash.String()])
	}
	for i := range m.editQueue {
		addGroup(&m.editQueue[i])
	}

	return index
}

func (m *Model) rebuildEditMap() {
	m.editMap = make(map[string]*gitops.ForgeChange)
	for i := range m.editQueue {
		m.editMap[m.editQueue[i].OriginalHash.String()] = &m.editQueue[i]
	}
}

func combineGroupKey(hashes []plumbing.Hash) string {
	parts := make([]string, len(hashes))
	for i, hash := range hashes {
		parts[i] = hash.String()
	}
	return strings.Join(parts, ",")
}

func shortHashList(hashes []plumbing.Hash) string {
	parts := make([]string, len(hashes))
	for i, hash := range hashes {
		parts[i] = hash.String()[:7]
	}
	return strings.Join(parts, ", ")
}
