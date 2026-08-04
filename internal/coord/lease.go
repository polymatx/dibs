package coord

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

// Lease is one agent's claim on a set of file patterns for a limited time.
type Lease struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Patterns  []string  `json:"patterns"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Renewals  int       `json:"renewals,omitempty"`
	Worktree  string    `json:"worktree,omitempty"`
	Branch    string    `json:"branch,omitempty"`
}

// ExpiresIn returns the remaining lease time, floored at zero.
func (l Lease) ExpiresIn(now time.Time) time.Duration {
	d := l.ExpiresAt.Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

// Conflict pairs a requested pattern (or checked path) with the foreign
// lease that covers it.
type Conflict struct {
	Pattern string `json:"pattern"`
	Holder  Lease  `json:"holder"`
}

const (
	DefaultTTL = 30 * time.Minute
	MaxTTL     = 24 * time.Hour
)

func (m *Manager) leaseDir() string { return filepath.Join(m.St.State, "leases") }

// Claim asks for a lease on patterns. It returns either the granted (or
// renewed) lease, or the list of conflicts that denied it.
func (m *Manager) Claim(patterns []string, reason string, ttl time.Duration) (*Lease, []Conflict, error) {
	if len(patterns) == 0 {
		return nil, nil, fmt.Errorf("claim needs at least one pattern")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	norm := make([]string, 0, len(patterns))
	for _, p := range patterns {
		n, err := Normalize(p, m.St.Repo)
		if err != nil {
			return nil, nil, err
		}
		norm = append(norm, n)
	}
	slices.Sort(norm)
	norm = slices.Compact(norm)

	var granted *Lease
	var conflicts []Conflict
	err := m.St.WithLock(func() error {
		active, err := m.pruneExpiredLocked()
		if err != nil {
			return err
		}
		for _, lease := range active {
			if lease.Agent == m.Agent {
				continue
			}
			for _, want := range norm {
				for _, held := range lease.Patterns {
					if Intersect(want, held) {
						conflicts = append(conflicts, Conflict{Pattern: want, Holder: lease})
					}
				}
			}
		}
		if len(conflicts) > 0 {
			m.journal("deny", map[string]any{"patterns": norm, "held_by": conflicts[0].Holder.Agent})
			return nil
		}
		now := m.Now()
		// Re-claiming your own identical pattern set renews it in place.
		for _, lease := range active {
			if lease.Agent == m.Agent && slices.Equal(lease.Patterns, norm) {
				lease.ExpiresAt = now.Add(ttl)
				lease.Renewals++
				if reason != "" {
					lease.Reason = reason
				}
				granted = &lease
				return store.WriteJSON(filepath.Join(m.leaseDir(), lease.ID+".json"), lease)
			}
		}
		granted = &Lease{
			ID:        newID(),
			Agent:     m.Agent,
			Patterns:  norm,
			Reason:    reason,
			CreatedAt: now,
			ExpiresAt: now.Add(ttl),
			Worktree:  m.St.Worktree,
			Branch:    m.St.Branch(),
		}
		m.journal("claim", map[string]any{"lease": granted.ID, "patterns": norm, "reason": reason})
		return store.WriteJSON(filepath.Join(m.leaseDir(), granted.ID+".json"), *granted)
	})
	if err != nil {
		return nil, nil, err
	}
	m.touch(reason)
	return granted, conflicts, nil
}

// Release drops leases held by this agent: all of them, by lease ID, or by
// exact pattern. It returns the leases that were released.
func (m *Manager) Release(ids, patterns []string, all bool) ([]Lease, error) {
	var released []Lease
	err := m.St.WithLock(func() error {
		active, err := m.pruneExpiredLocked()
		if err != nil {
			return err
		}
		for _, lease := range active {
			if lease.Agent != m.Agent {
				continue
			}
			drop := all || slices.Contains(ids, lease.ID)
			if !drop {
				for _, p := range patterns {
					n, err := Normalize(p, m.St.Repo)
					if err == nil && slices.Contains(lease.Patterns, n) {
						drop = true
						break
					}
				}
			}
			if drop {
				if err := os.Remove(filepath.Join(m.leaseDir(), lease.ID+".json")); err != nil && !os.IsNotExist(err) {
					return err
				}
				released = append(released, lease)
				m.journal("release", map[string]any{"lease": lease.ID, "patterns": lease.Patterns})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.touch("")
	return released, nil
}

// Renew extends every lease this agent holds by ttl from now.
func (m *Manager) Renew(ttl time.Duration) ([]Lease, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	var renewed []Lease
	err := m.St.WithLock(func() error {
		active, err := m.pruneExpiredLocked()
		if err != nil {
			return err
		}
		now := m.Now()
		for _, lease := range active {
			if lease.Agent != m.Agent {
				continue
			}
			lease.ExpiresAt = now.Add(ttl)
			lease.Renewals++
			if err := store.WriteJSON(filepath.Join(m.leaseDir(), lease.ID+".json"), lease); err != nil {
				return err
			}
			renewed = append(renewed, lease)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.touch("")
	return renewed, nil
}

// Check reports which of the given repo-relative paths are covered by
// another agent's active lease. It never mutates state, so it is safe and
// fast to call from hooks on every file edit.
func (m *Manager) Check(paths []string) ([]Conflict, error) {
	active, err := m.ActiveLeases()
	if err != nil {
		return nil, err
	}
	var conflicts []Conflict
	for _, raw := range paths {
		p, err := Normalize(raw, m.St.Repo)
		if err != nil {
			continue
		}
		for _, lease := range active {
			if lease.Agent == m.Agent {
				continue
			}
			for _, held := range lease.Patterns {
				if Intersect(p, held) {
					conflicts = append(conflicts, Conflict{Pattern: p, Holder: lease})
				}
			}
		}
	}
	return conflicts, nil
}

// ActiveLeases returns unexpired leases without mutating state.
func (m *Manager) ActiveLeases() ([]Lease, error) {
	leases, err := store.ListJSON[Lease](m.leaseDir())
	if err != nil {
		return nil, err
	}
	now := m.Now()
	active := leases[:0]
	for _, l := range leases {
		if l.ExpiresAt.After(now) {
			active = append(active, l)
		}
	}
	slices.SortFunc(active, func(a, b Lease) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return active, nil
}

// pruneExpiredLocked removes expired lease files (journaling each) and
// returns the remaining active leases. Callers must hold the store lock.
func (m *Manager) pruneExpiredLocked() ([]Lease, error) {
	leases, err := store.ListJSON[Lease](m.leaseDir())
	if err != nil {
		return nil, err
	}
	now := m.Now()
	var active []Lease
	for _, l := range leases {
		if l.ExpiresAt.After(now) {
			active = append(active, l)
			continue
		}
		if err := os.Remove(filepath.Join(m.leaseDir(), l.ID+".json")); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		m.journal("expire", map[string]any{"lease": l.ID, "agent": l.Agent, "patterns": l.Patterns})
	}
	slices.SortFunc(active, func(a, b Lease) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return active, nil
}
