package tui

import "github.com/Jan/git-backtrack/internal/gitops"

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

func (m *Model) rebuildEditMap() {
	m.editMap = make(map[string]*gitops.ForgeChange)
	for i := range m.editQueue {
		m.editMap[m.editQueue[i].OriginalHash.String()] = &m.editQueue[i]
	}
}
