// Package coord implements the coordination layer of dibs: leases on file
// patterns, agent presence, broadcast notes, and the event journal.
//
// All state is plain JSON files under the store's State dir. Nothing here
// runs in the background: expiry is evaluated lazily whenever state is read
// or mutated, so dibs needs no daemon.
package coord

import (
	"crypto/rand"
	"encoding/hex"
	"hash/fnv"
	"os"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

// Manager performs coordination operations as one named agent.
type Manager struct {
	St    *store.Store
	Agent string
	Now   func() time.Time
}

// NewManager resolves the acting agent's identity and returns a manager.
// Identity precedence: explicit name (--agent) > DIBS_AGENT env > a stable
// name derived from the worktree path.
func NewManager(st *store.Store, explicit string) *Manager {
	name := explicit
	if name == "" {
		name = os.Getenv("DIBS_AGENT")
	}
	if name == "" {
		name = AutoName(st.Worktree)
	}
	return &Manager{St: st, Agent: name, Now: time.Now}
}

// AutoName derives a stable, human-friendly agent name from a worktree path.
// The same worktree always maps to the same name, so an agent working in
// ./wt-auth is "the same agent" across sessions without any configuration.
func AutoName(worktree string) string {
	h := fnv.New64a()
	h.Write([]byte(worktree))
	n := h.Sum64()
	return adjectives[n%uint64(len(adjectives))] + "-" + animals[(n/uint64(len(adjectives)))%uint64(len(animals))]
}

var adjectives = []string{
	"brisk", "calm", "clever", "daring", "deft", "eager", "fleet", "gentle",
	"keen", "lively", "lucid", "merry", "nimble", "plucky", "quick", "quiet",
	"rapid", "sharp", "shrewd", "sleek", "spry", "steady", "swift", "witty",
}

var animals = []string{
	"badger", "crane", "dingo", "falcon", "ferret", "fox", "gecko", "heron",
	"ibex", "jackal", "kestrel", "lemur", "lynx", "marten", "mole", "otter",
	"owl", "panda", "raven", "stoat", "swift", "tapir", "vole", "wren",
}

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
