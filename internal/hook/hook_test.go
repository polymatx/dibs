package hook

import (
	"encoding/json"
	"os"
	"os/exec"
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

// Review regressions: valid-but-unexpected JSON shapes must neither panic
// nor destroy user configuration.
func TestInstallClaudeUnexpectedShapes(t *testing.T) {
	dir := t.TempDir()

	// A file containing JSON null must not panic and must install cleanly.
	nullFile := filepath.Join(dir, "null.json")
	os.WriteFile(nullFile, []byte("null\n"), 0o644)
	if changed, err := InstallClaude(nullFile); err != nil || !changed {
		t.Fatalf("null settings: changed=%v err=%v", changed, err)
	}

	// hooks with a non-object shape: refuse, and leave the file untouched.
	arrFile := filepath.Join(dir, "arr.json")
	orig := []byte(`{"hooks": ["something-custom"]}`)
	os.WriteFile(arrFile, orig, 0o644)
	if _, err := InstallClaude(arrFile); err == nil {
		t.Fatal("hooks-as-array must be refused, not overwritten")
	}
	if raw, _ := os.ReadFile(arrFile); string(raw) != string(orig) {
		t.Fatalf("refusal must not modify the file, got %s", raw)
	}

	// PreToolUse with a non-array shape: refuse, preserve.
	objFile := filepath.Join(dir, "obj.json")
	orig = []byte(`{"hooks": {"PreToolUse": {"matcher": "Bash", "hooks": []}}}`)
	os.WriteFile(objFile, orig, 0o644)
	if _, err := InstallClaude(objFile); err == nil {
		t.Fatal("PreToolUse-as-object must be refused, not replaced")
	}
	if raw, _ := os.ReadFile(objFile); string(raw) != string(orig) {
		t.Fatalf("refusal must not modify the file, got %s", raw)
	}
}

// Review regression: uninstall must remove only the dibs command, keeping
// user hooks that share the same PreToolUse entry.
func TestUninstallClaudeKeepsGroupedUserHooks(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	content := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {"type": "command", "command": "dibs hook claude"},
          {"type": "command", "command": "./my-lint.sh"}
        ]
      }
    ]
  }
}`
	os.WriteFile(settings, []byte(content), 0o644)
	changed, err := UninstallClaude(settings)
	if err != nil || !changed {
		t.Fatalf("uninstall: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(settings)
	if strings.Contains(string(raw), "dibs hook claude") {
		t.Fatalf("dibs command not removed: %s", raw)
	}
	if !strings.Contains(string(raw), "my-lint.sh") {
		t.Fatalf("user hook grouped with dibs was deleted: %s", raw)
	}
}

// RunPreCommit must exit 2 for "held by another agent", matching the
// documented exit-code contract.
func TestPreCommitExitCode(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	state := filepath.Join(t.TempDir(), "state")
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(repo, "src", "auth"), 0o755)
	os.WriteFile(filepath.Join(repo, "src", "auth", "token.go"), []byte("x"), 0o644)
	git("add", "-A")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
	os.WriteFile(filepath.Join(repo, "src", "auth", "token.go"), []byte("changed"), 0o644)
	git("add", "src/auth/token.go")

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	alice := &coord.Manager{St: store.At(state, repo), Agent: "alice", Now: func() time.Time { return now }}
	alice.St.EnsureState()
	// Claim from a DIFFERENT worktree so bob's auto identity can't be self.
	aliceElsewhere := &coord.Manager{St: store.At(state, t.TempDir()), Agent: "alice", Now: func() time.Time { return now }}
	if _, conflicts, err := aliceElsewhere.Claim([]string{"src/auth/**"}, "refactor", 0); err != nil || len(conflicts) > 0 {
		t.Fatalf("setup claim: %v %v", conflicts, err)
	}

	bob := &coord.Manager{St: store.At(state, repo), Agent: "bob", Now: func() time.Time { return now }}
	var errOut strings.Builder
	if code := RunPreCommit(bob, &errOut); code != 2 {
		t.Fatalf("pre-commit exit = %d, want 2; stderr=%q", code, errOut.String())
	}
}
