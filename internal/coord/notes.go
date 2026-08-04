package coord

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

// Note is a small broadcast message between agents: "I changed the User
// schema — regenerate types before touching api/". Notes are the handoff
// mechanism; anything bigger belongs in a lesson or a commit message.
type Note struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

const noteExpiry = 7 * 24 * time.Hour

func (m *Manager) noteDir() string { return filepath.Join(m.St.State, "notes") }

// PostNote broadcasts a message to every agent on this repository. The
// timestamp is assigned inside the lock, so a note can never be created
// in another agent's past (which would let a read cursor skip it).
func (m *Manager) PostNote(msg string) (*Note, error) {
	if msg == "" {
		return nil, fmt.Errorf("note message is empty")
	}
	var n Note
	err := m.St.WithLock(func() error {
		n = Note{ID: newID(), From: m.Agent, Message: msg, CreatedAt: m.Now()}
		m.pruneNotesLocked()
		name := fmt.Sprintf("%020d-%s.json", n.CreatedAt.UnixNano(), n.ID)
		m.journal("note", map[string]any{"from": n.From, "message": msg})
		return store.WriteJSON(filepath.Join(m.noteDir(), name), n)
	})
	if err != nil {
		return nil, err
	}
	m.touch("")
	return &n, nil
}

// Notes returns all live notes, oldest first.
func (m *Manager) Notes() ([]Note, error) {
	notes, err := store.ListJSON[Note](m.noteDir())
	if err != nil {
		return nil, err
	}
	now := m.Now()
	live := notes[:0]
	for _, n := range notes {
		if now.Sub(n.CreatedAt) < noteExpiry {
			live = append(live, n)
		}
	}
	slices.SortFunc(live, func(a, b Note) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return live, nil
}

// UnreadNotes returns notes from other agents newer than this agent's read
// cursor.
func (m *Manager) UnreadNotes() ([]Note, error) {
	all, err := m.Notes()
	if err != nil {
		return nil, err
	}
	var info AgentInfo
	_ = store.ReadJSON(m.agentPath(), &info)
	var unread []Note
	for _, n := range all {
		if n.From != m.Agent && n.CreatedAt.After(info.LastNoteRead) {
			unread = append(unread, n)
		}
	}
	return unread, nil
}

// MarkNotesRead advances this agent's read cursor to the newest note it
// actually saw — never to "now", which would silently swallow any note
// that lands between reading and acknowledging.
func (m *Manager) MarkNotesRead(seen []Note) error {
	var newest time.Time
	for _, n := range seen {
		if n.CreatedAt.After(newest) {
			newest = n.CreatedAt
		}
	}
	if newest.IsZero() {
		return nil
	}
	return m.St.WithLock(func() error {
		var info AgentInfo
		if err := store.ReadJSON(m.agentPath(), &info); err != nil {
			info = AgentInfo{Name: m.Agent, FirstSeen: m.Now()}
		}
		if newest.After(info.LastNoteRead) {
			info.LastNoteRead = newest
		}
		info.LastSeen = m.Now()
		return store.WriteJSON(m.agentPath(), info)
	})
}

func (m *Manager) pruneNotesLocked() {
	entries, err := os.ReadDir(m.noteDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(m.noteDir(), e.Name())
		var n Note
		if store.ReadJSON(path, &n) == nil && m.Now().Sub(n.CreatedAt) >= noteExpiry {
			os.Remove(path)
		}
	}
}
