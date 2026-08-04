package coord

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

// AgentInfo is what dibs knows about one agent, updated as a side effect of
// every command that agent runs. There are no heartbeats: freshness is
// simply "when did it last do something".
type AgentInfo struct {
	Name         string    `json:"name"`
	Worktree     string    `json:"worktree,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	LastReason   string    `json:"last_reason,omitempty"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	LastNoteRead time.Time `json:"last_note_read,omitempty"`
}

const presenceExpiry = 7 * 24 * time.Hour

func (m *Manager) agentDir() string { return filepath.Join(m.St.State, "agents") }
func (m *Manager) agentPath() string {
	return filepath.Join(m.agentDir(), m.Agent+".json")
}

// touch upserts this agent's presence record. Errors are deliberately
// dropped: presence is best-effort decoration, never worth failing a
// coordination operation over.
func (m *Manager) touch(reason string) {
	_ = m.St.WithLock(func() error {
		var info AgentInfo
		if err := store.ReadJSON(m.agentPath(), &info); err != nil {
			info = AgentInfo{Name: m.Agent, FirstSeen: m.Now()}
		}
		info.Worktree = m.St.Worktree
		info.Branch = m.St.Branch()
		info.LastSeen = m.Now()
		if reason != "" {
			info.LastReason = reason
		}
		m.pruneAgentsLocked()
		return store.WriteJSON(m.agentPath(), info)
	})
}

// Touch records activity for this agent; exported for commands that don't
// otherwise mutate state (status, mcp startup).
func (m *Manager) Touch() { m.touch("") }

// Agents lists every agent seen recently, most recently active first.
func (m *Manager) Agents() ([]AgentInfo, error) {
	agents, err := store.ListJSON[AgentInfo](m.agentDir())
	if err != nil {
		return nil, err
	}
	now := m.Now()
	live := agents[:0]
	for _, a := range agents {
		if now.Sub(a.LastSeen) < presenceExpiry {
			live = append(live, a)
		}
	}
	slices.SortFunc(live, func(a, b AgentInfo) int { return b.LastSeen.Compare(a.LastSeen) })
	return live, nil
}

func (m *Manager) pruneAgentsLocked() {
	agents, _ := store.ListJSON[AgentInfo](m.agentDir())
	for _, a := range agents {
		if m.Now().Sub(a.LastSeen) >= presenceExpiry {
			os.Remove(filepath.Join(m.agentDir(), a.Name+".json"))
		}
	}
}
