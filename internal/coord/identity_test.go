package coord

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polymatx/dibs/internal/store"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"alice":                 "alice",
		"agent one":             "agent-one",
		"../../etc/passwd":      "etc-passwd", // path traversal neutralized
		"foo/bar":               "foo-bar",
		"...":                   "",
		"UPPER.case_ok-1":       "UPPER.case_ok-1",
		strings.Repeat("x", 80): strings.Repeat("x", 64),
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewManagerFallsBackToAutoOnGarbageName(t *testing.T) {
	st := store.At(t.TempDir(), t.TempDir())
	m := NewManager(st, "///")
	if m.Agent == "" || !m.AutoIdentity {
		t.Fatalf("garbage explicit name must fall back to auto identity, got %q auto=%v", m.Agent, m.AutoIdentity)
	}
	m2 := NewManager(st, "alice")
	if m2.Agent != "alice" || m2.AutoIdentity {
		t.Fatalf("explicit name mishandled: %q auto=%v", m2.Agent, m2.AutoIdentity)
	}
}

// TestAutoIdentitySameWorktreeIsSelf covers the hook scenario: a claim is
// made under an explicit DIBS_AGENT, but the enforcement hook runs without
// that variable and derives an auto identity. Same worktree → own claim →
// no self-blocking. A different worktree still conflicts, and an
// explicitly named second agent in the same worktree still conflicts.
func TestAutoIdentitySameWorktreeIsSelf(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	wtA := t.TempDir()
	wtB := t.TempDir()
	os.MkdirAll(filepath.Join(wtA, "src", "auth"), 0o755)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	alice := &Manager{St: store.At(state, wtA), Agent: "alice", Now: clock}
	alice.St.EnsureState()
	if _, conflicts, err := alice.Claim([]string{"src/auth"}, "refactor", 0); err != nil || len(conflicts) > 0 {
		t.Fatalf("setup claim: %v %v", conflicts, err)
	}

	hookSameWT := &Manager{St: store.At(state, wtA), Agent: AutoName(wtA), AutoIdentity: true, Now: clock}
	if got, _ := hookSameWT.Check([]string{"src/auth/token.go"}); len(got) != 0 {
		t.Fatalf("auto identity in the claiming worktree must not be blocked by its own claim: %v", got)
	}

	hookOtherWT := &Manager{St: store.At(state, wtB), Agent: AutoName(wtB), AutoIdentity: true, Now: clock}
	if got, _ := hookOtherWT.Check([]string{"src/auth/token.go"}); len(got) != 1 {
		t.Fatalf("auto identity in another worktree must still see the conflict: %v", got)
	}

	bobSameWT := &Manager{St: store.At(state, wtA), Agent: "bob", Now: clock}
	if got, _ := bobSameWT.Check([]string{"src/auth/token.go"}); len(got) != 1 {
		t.Fatalf("an explicitly distinct agent must conflict even in the same worktree: %v", got)
	}
}
