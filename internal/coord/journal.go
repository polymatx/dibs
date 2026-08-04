package coord

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/polymatx/dibs/internal/store"
)

// Event is one line of the append-only journal: who did what, when. The
// journal is the answer to "what have the agents been doing?".
type Event struct {
	At      string         `json:"at"`
	Event   string         `json:"event"`
	Agent   string         `json:"agent"`
	Details map[string]any `json:"details,omitempty"`
}

const journalMaxBytes = 5 << 20 // rotate at 5 MiB; coordination history, not an archive

func (m *Manager) journalPath() string { return filepath.Join(m.St.State, "journal.jsonl") }

// journal appends an event. Best-effort by design: the journal must never
// make a claim or release fail. Callers hold the store lock.
func (m *Manager) journal(event string, details map[string]any) {
	e := Event{At: m.Now().UTC().Format("2006-01-02T15:04:05Z"), Event: event, Agent: m.Agent, Details: details}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	m.rotateJournalLocked()
	_ = store.AppendLine(m.journalPath(), line)
}

// Journal returns the most recent n events, oldest first.
func (m *Manager) Journal(n int) ([]Event, error) {
	f, err := os.Open(m.journalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			events = append(events, e)
		}
	}
	if n > 0 && len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}

// rotateJournalLocked halves the journal when it outgrows its budget,
// keeping the newest entries.
func (m *Manager) rotateJournalLocked() {
	st, err := os.Stat(m.journalPath())
	if err != nil || st.Size() < journalMaxBytes {
		return
	}
	events, err := m.Journal(0)
	if err != nil || len(events) < 2 {
		return
	}
	keep := events[len(events)/2:]
	var buf []byte
	for _, e := range keep {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	_ = store.WriteAtomic(m.journalPath(), buf)
}
