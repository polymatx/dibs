package coord

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

// clock is a settable fake time source shared by test managers.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// pair returns two managers ("alice", "bob") sharing one state dir, as two
// agents on the same repository would.
func pair(t *testing.T) (*Manager, *Manager, *clock) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, "src", "auth"), 0o755)
	os.MkdirAll(filepath.Join(repo, "src", "api"), 0o755)
	ck := &clock{t: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
	a := &Manager{St: store.At(state, repo), Agent: "alice", Now: ck.now}
	b := &Manager{St: store.At(state, repo), Agent: "bob", Now: ck.now}
	for _, m := range []*Manager{a, b} {
		if err := m.St.EnsureState(); err != nil {
			t.Fatal(err)
		}
	}
	return a, b, ck
}

func TestClaimDenyReleaseFlow(t *testing.T) {
	alice, bob, _ := pair(t)

	lease, conflicts, err := alice.Claim([]string{"src/auth"}, "refactor", 0)
	if err != nil || len(conflicts) > 0 || lease == nil {
		t.Fatalf("alice claim: lease=%v conflicts=%v err=%v", lease, conflicts, err)
	}
	if lease.Patterns[0] != "src/auth/**" {
		t.Fatalf("dir not expanded: %v", lease.Patterns)
	}

	_, conflicts, err = bob.Claim([]string{"src/auth/token.go"}, "fix", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0].Holder.Agent != "alice" {
		t.Fatalf("bob should be denied by alice, got %v", conflicts)
	}

	if got, _, _ := bob.Claim([]string{"src/api"}, "endpoints", 0); got == nil {
		t.Fatal("bob's disjoint claim should be granted")
	}

	released, err := alice.Release(nil, nil, true)
	if err != nil || len(released) != 1 {
		t.Fatalf("release: %v %v", released, err)
	}

	if _, conflicts, _ = bob.Claim([]string{"src/auth/token.go"}, "fix", 0); len(conflicts) != 0 {
		t.Fatalf("after release bob should get the claim, conflicts=%v", conflicts)
	}
}

func TestClaimExpiry(t *testing.T) {
	alice, bob, ck := pair(t)

	if _, _, err := alice.Claim([]string{"src/auth"}, "slow work", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	ck.advance(11 * time.Minute)

	_, conflicts, err := bob.Claim([]string{"src/auth"}, "my turn", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("alice's lease expired, bob should win; got %v", conflicts)
	}

	active, _ := bob.ActiveLeases()
	if len(active) != 1 || active[0].Agent != "bob" {
		t.Fatalf("expired lease should be pruned, active=%v", active)
	}
}

func TestReclaimRenews(t *testing.T) {
	alice, _, ck := pair(t)

	l1, _, _ := alice.Claim([]string{"src/auth"}, "work", 10*time.Minute)
	ck.advance(5 * time.Minute)
	l2, conflicts, err := alice.Claim([]string{"src/auth"}, "still working", 10*time.Minute)
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("re-claim by same agent must renew, got conflicts=%v err=%v", conflicts, err)
	}
	if l2.ID != l1.ID {
		t.Fatalf("renewal must keep the lease ID: %s vs %s", l1.ID, l2.ID)
	}
	if !l2.ExpiresAt.After(l1.ExpiresAt) {
		t.Fatal("renewal must extend expiry")
	}
	if l2.Renewals != 1 {
		t.Fatalf("renewals = %d, want 1", l2.Renewals)
	}
}

func TestCheck(t *testing.T) {
	alice, bob, _ := pair(t)
	alice.Claim([]string{"src/auth/**", "docs/adr"}, "auth + adr", 0)

	conflicts, err := bob.Check([]string{"src/auth/token.go", "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0].Pattern != "src/auth/token.go" {
		t.Fatalf("want one conflict on token.go, got %v", conflicts)
	}

	// Your own claims never conflict with you.
	own, err := alice.Check([]string{"src/auth/token.go"})
	if err != nil || len(own) != 0 {
		t.Fatalf("own claim reported as conflict: %v %v", own, err)
	}
}

// TestCheckAbsolutePaths is the review regression: agents pass absolute
// paths constantly, and an absolute path to a held file must not report
// FREE.
func TestCheckAbsolutePaths(t *testing.T) {
	alice, bob, _ := pair(t)
	if _, conflicts, err := alice.Claim([]string{"src/auth"}, "refactor", 0); err != nil || len(conflicts) > 0 {
		t.Fatalf("setup: %v %v", conflicts, err)
	}
	abs := filepath.Join(bob.St.Repo, "src", "auth", "token.go")
	got, err := bob.Check([]string{abs})
	if err != nil || len(got) != 1 {
		t.Fatalf("absolute path to a held file must conflict: %v %v", got, err)
	}
	// And an absolute path outside the repo is not ours to police.
	got, err = bob.Check([]string{"/somewhere/else/entirely.go"})
	if err != nil || len(got) != 0 {
		t.Fatalf("outside-repo path must be ignored: %v %v", got, err)
	}
}

// TestClaimAbsolutePattern: an absolute path inside the repo claims the
// repo-relative form; outside the repo it errors instead of granting a
// junk lease that protects nothing.
func TestClaimAbsolutePattern(t *testing.T) {
	alice, bob, _ := pair(t)
	abs := filepath.Join(alice.St.Repo, "src", "auth")
	lease, conflicts, err := alice.Claim([]string{abs}, "refactor", 0)
	if err != nil || len(conflicts) > 0 || lease.Patterns[0] != "src/auth/**" {
		t.Fatalf("absolute claim mishandled: %+v %v %v", lease, conflicts, err)
	}
	if _, _, err := bob.Claim([]string{"/absolutely/not/in/repo"}, "junk", 0); err == nil {
		t.Fatal("claiming a path outside the repo must error")
	}
}

// TestJournalTruncatesOversizedDetails: user content in journal details is
// capped so a single huge note can never make the journal unreadable
// (which would silently hide the newest events and corrupt rotation).
func TestJournalTruncatesOversizedDetails(t *testing.T) {
	alice, _, _ := pair(t)
	huge := strings.Repeat("x", 5000)
	if _, err := alice.PostNote(huge); err != nil {
		t.Fatal(err)
	}
	events, err := alice.Journal(0)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	msg, _ := last.Details["message"].(string)
	if len(msg) > 1100 || !strings.HasSuffix(msg, "…") {
		t.Fatalf("journal detail not truncated: len=%d", len(msg))
	}
}

// TestConcurrentClaims hammers the same pattern from two agents in
// parallel; the lock must ensure at most one holds it at any time.
func TestConcurrentClaims(t *testing.T) {
	alice, bob, _ := pair(t)

	const rounds = 15
	var wg sync.WaitGroup
	grants := make([][]bool, 2)
	for i, m := range []*Manager{alice, bob} {
		grants[i] = make([]bool, rounds)
		wg.Add(1)
		go func(i int, m *Manager) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				lease, conflicts, err := m.Claim([]string{"hot/path.go"}, "race", time.Minute)
				if err != nil {
					t.Errorf("claim error: %v", err)
					return
				}
				if lease != nil && len(conflicts) == 0 {
					grants[i][r] = true
					if _, err := m.Release(nil, nil, true); err != nil {
						t.Errorf("release: %v", err)
						return
					}
				}
			}
		}(i, m)
	}
	wg.Wait()

	anyGrant := false
	for i := range grants {
		for _, g := range grants[i] {
			anyGrant = anyGrant || g
		}
	}
	if !anyGrant {
		t.Fatal("nobody ever got the lease — locking is too strict or broken")
	}
	// Final state: released or single-holder, never two live leases.
	active, _ := alice.ActiveLeases()
	if len(active) > 1 {
		t.Fatalf("two live leases on the same pattern: %v", active)
	}
}

func TestNotesFlow(t *testing.T) {
	alice, bob, ck := pair(t)

	if _, err := alice.PostNote("heads up"); err != nil {
		t.Fatal(err)
	}
	unread, err := bob.UnreadNotes()
	if err != nil || len(unread) != 1 {
		t.Fatalf("bob unread = %v, err %v", unread, err)
	}
	// Own notes are never "unread" for their author.
	if u, _ := alice.UnreadNotes(); len(u) != 0 {
		t.Fatalf("alice sees own note as unread: %v", u)
	}
	all, _ := bob.Notes()
	if err := bob.MarkNotesRead(all); err != nil {
		t.Fatal(err)
	}
	if u, _ := bob.UnreadNotes(); len(u) != 0 {
		t.Fatalf("after MarkNotesRead unread should be empty, got %v", u)
	}
	// Acknowledging nothing must not advance the cursor: a note that
	// lands after a read is still unread.
	if err := bob.MarkNotesRead(nil); err != nil {
		t.Fatal(err)
	}
	ck.advance(time.Second)
	if _, err := alice.PostNote("second"); err != nil {
		t.Fatal(err)
	}
	if u, _ := bob.UnreadNotes(); len(u) != 1 {
		t.Fatalf("note posted after read must be unread, got %v", u)
	}
}

func TestJournalRecordsFlow(t *testing.T) {
	alice, bob, _ := pair(t)
	alice.Claim([]string{"a"}, "", 0)
	bob.Claim([]string{"a"}, "", 0) // deny
	alice.Release(nil, nil, true)

	events, err := alice.Journal(0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Event)
	}
	want := []string{"claim", "deny", "release"}
	if len(kinds) != len(want) {
		t.Fatalf("journal = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("journal = %v, want %v", kinds, want)
		}
	}
}
