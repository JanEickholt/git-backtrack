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

func (m *Model) rebuildEditMap() {
	m.editMap = make(map[string]*gitops.ForgeChange)
	for i := range m.editQueue {
		m.editMap[m.editQueue[i].OriginalHash.String()] = &m.editQueue[i]
	}
}
