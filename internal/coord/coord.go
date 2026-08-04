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
	"regexp"
	"strings"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

// Manager performs coordination operations as one named agent.
type Manager struct {
	St    *store.Store
	Agent string
	// AutoIdentity is true when Agent was derived from the worktree path
	// rather than chosen explicitly. An auto identity means "I am this
	// worktree", which widens what counts as our own lease — see isSelf.
	AutoIdentity bool
	Now          func() time.Time
}

// NewManager resolves the acting agent's identity and returns a manager.
// Identity precedence: explicit name (--agent) > DIBS_AGENT env > a stable
// name derived from the worktree path.
func NewManager(st *store.Store, explicit string) *Manager {
	name := SanitizeName(explicit)
	if name == "" {
		name = SanitizeName(os.Getenv("DIBS_AGENT"))
	}
	auto := name == ""
	if auto {
		name = AutoName(st.Worktree)
	}
	return &Manager{St: st, Agent: name, AutoIdentity: auto, Now: time.Now}
}

// isSelf reports whether a lease belongs to this agent. A lease with the
// same name is always ours. A lease taken from the same worktree is ours
// only when our identity is auto-derived: an auto identity means "I am
// this worktree", so anything this worktree claimed — under any name — is
// our own work. This keeps enforcement hooks (which often run without the
// DIBS_AGENT that claims were made under) from blocking an agent on its
// own claims, while two explicitly distinct identities sharing a worktree
// still protect their claims from each other.
func (m *Manager) isSelf(l Lease) bool {
	if l.Agent == m.Agent {
		return true
	}
	return m.AutoIdentity && l.Worktree != "" && l.Worktree == m.St.Worktree
}

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// SanitizeName restricts an agent name to filesystem- and display-safe
// characters. Presence records are stored under the agent's name, so this
// is a safety boundary, not cosmetics.
func SanitizeName(s string) string {
	s = invalidNameChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-._")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
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
