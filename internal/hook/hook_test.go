package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polymatx/dibs/internal/coord"
	"github.com/polymatx/dibs/internal/store"
)

func testPair(t *testing.T) (alice, bob *coord.Manager) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, "src", "auth"), 0o755)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	alice = &coord.Manager{St: store.At(state, repo), Agent: "alice", Now: func() time.Time { return now }}
	bob = &coord.Manager{St: store.At(state, repo), Agent: "bob", Now: func() time.Time { return now }}
	alice.St.EnsureState()
	return alice, bob
}

func TestRunClaudeBlocksForeignLease(t *testing.T) {
	alice, bob, in := testPairWithClaim(t, `{"tool_name":"Edit","tool_input":{"file_path":"src/auth/token.go"}}`)
	_ = alice
	var errOut strings.Builder
	code := RunClaude(bob, strings.NewReader(in), &errOut)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (block); stderr=%q", code, errOut.String())
	}
	msg := errOut.String()
	if !strings.Contains(msg, "alice") || !strings.Contains(msg, "refactor auth") {
		t.Fatalf("block message must say who and why: %q", msg)
	}
}

func testPairWithClaim(t *testing.T, input string) (*coord.Manager, *coord.Manager, string) {
	t.Helper()
	alice, bob := testPair(t)
	if _, conflicts, err := alice.Claim([]string{"src/auth"}, "refactor auth", 0); err != nil || len(conflicts) > 0 {
		t.Fatalf("setup claim failed: %v %v", conflicts, err)
	}
	return alice, bob, input
}

func TestRunClaudeAllowsFreeFileOwnLeaseAndGarbage(t *testing.T) {
	alice, bob, _ := testPairWithClaim(t, "")

	// Free file: allowed.
	in := `{"tool_name":"Edit","tool_input":{"file_path":"README.md"}}`
	if code := RunClaude(bob, strings.NewReader(in), os.Stderr); code != 0 {
		t.Fatalf("free file blocked: %d", code)
	}
	// Your own lease: allowed.
	in = `{"tool_name":"Edit","tool_input":{"file_path":"src/auth/token.go"}}`
	if code := RunClaude(alice, strings.NewReader(in), os.Stderr); code != 0 {
		t.Fatalf("own lease blocked: %d", code)
	}
	// Garbage input: fail open, never brick the editor.
	if code := RunClaude(bob, strings.NewReader("not json"), os.Stderr); code != 0 {
		t.Fatalf("garbage input must fail open, got %d", code)
	}
	// A tool call with no file path (e.g. Bash): allowed.
	in = `{"tool_name":"Bash","tool_input":{"command":"ls"}}`
	if code := RunClaude(bob, strings.NewReader(in), os.Stderr); code != 0 {
		t.Fatalf("pathless tool must pass, got %d", code)
	}
}

func TestRunClaudeAbsolutePathInsideRepo(t *testing.T) {
	_, bob, _ := testPairWithClaim(t, "")
	abs := filepath.Join(bob.St.Repo, "src", "auth", "token.go")
	in := `{"tool_name":"Write","tool_input":{"file_path":` + jsonString(abs) + `}}`
	var errOut strings.Builder
	if code := RunClaude(bob, strings.NewReader(in), &errOut); code != 2 {
		t.Fatalf("absolute path inside repo should block, got %d", code)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestInstallClaudePreservesExistingSettings(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	existing := map[string]any{
		"permissions": map[string]any{"allow": []any{"Bash(ls:*)"}},
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "my-linter"}}},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(settings, b, 0o644)

	changed, err := InstallClaude(settings)
	if err != nil || !changed {
		t.Fatalf("install: changed=%v err=%v", changed, err)
	}
	// Second install is a no-op.
	changed, err = InstallClaude(settings)
	if err != nil || changed {
		t.Fatalf("reinstall must be idempotent: changed=%v err=%v", changed, err)
	}

	raw, _ := os.ReadFile(settings)
	var got map[string]any
	json.Unmarshal(raw, &got)
	if _, ok := got["permissions"]; !ok {
		t.Fatal("existing permissions were dropped")
	}
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("want linter + dibs entries, got %d", len(pre))
	}
	if !strings.Contains(string(raw), "my-linter") || !strings.Contains(string(raw), "dibs hook claude") {
		t.Fatalf("merged settings wrong: %s", raw)
	}

	// Uninstall removes only ours.
	changed, err = UninstallClaude(settings)
	if err != nil || !changed {
		t.Fatalf("uninstall: %v %v", changed, err)
	}
	raw, _ = os.ReadFile(settings)
	if strings.Contains(string(raw), "dibs hook claude") || !strings.Contains(string(raw), "my-linter") {
		t.Fatalf("uninstall broke settings: %s", raw)
	}
}

func TestInstallClaudeRefusesInvalidJSON(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(settings, []byte("{broken"), 0o644)
	if _, err := InstallClaude(settings); err == nil {
		t.Fatal("must refuse to overwrite invalid settings.json")
	}
}
