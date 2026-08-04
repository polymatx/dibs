// Package hook makes dibs leases enforceable instead of advisory.
//
// Claude Code: a PreToolUse hook runs `dibs hook claude` before every
// Edit/Write; if the target file is covered by another agent's lease, the
// hook exits with code 2, which blocks the tool call and feeds the reason
// back to the model — the agent sees who holds the file and why, and can
// wait, renegotiate, or work elsewhere.
//
// Any other agent: a git pre-commit hook (`dibs hook pre-commit`) refuses
// commits that touch files someone else has claimed.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/polymatx/dibs/internal/coord"
	"github.com/polymatx/dibs/internal/store"
)

// claudeInput is the part of Claude Code's PreToolUse payload dibs needs.
type claudeInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// pathKeys are the tool_input fields that name a file being modified.
var pathKeys = []string{"file_path", "notebook_path", "path"}

// RunClaude implements `dibs hook claude`. It reads the PreToolUse JSON on
// stdin and exits 2 (block) when the edited file is leased by another
// agent. Anything unexpected fails open: a broken hook must never lock a
// developer out of their own repository.
func RunClaude(m *coord.Manager, stdin io.Reader, stderr io.Writer) int {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return 0
	}
	var in claudeInput
	if json.Unmarshal(raw, &in) != nil {
		return 0
	}
	var paths []string
	for _, k := range pathKeys {
		if v, ok := in.ToolInput[k].(string); ok && v != "" {
			paths = append(paths, v)
		}
	}
	if len(paths) == 0 {
		return 0
	}
	rel := relToRepo(m.St.Repo, paths)
	if len(rel) == 0 {
		return 0
	}
	conflicts, err := m.Check(rel)
	if err != nil || len(conflicts) == 0 {
		return 0
	}
	c := conflicts[0]
	fmt.Fprintf(stderr,
		"dibs: %s is claimed by %s%s — the claim expires in %s.\n"+
			"Coordinate instead of colliding: wait for the lease, message them with `dibs note`, "+
			"or work on files outside their claim. Run `dibs status` to see all active claims.\n",
		c.Pattern, c.Holder.Agent, reasonSuffix(c.Holder.Reason), c.Holder.ExpiresIn(m.Now()).Round(1e9))
	return 2
}

// RunPreCommit implements `dibs hook pre-commit`: it blocks a commit when
// staged files intersect another agent's lease.
func RunPreCommit(m *coord.Manager, stderr io.Writer) int {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "-z")
	cmd.Dir = m.St.Worktree
	out, err := cmd.Output()
	if err != nil {
		return 0 // fail open
	}
	var staged []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			staged = append(staged, p)
		}
	}
	if len(staged) == 0 {
		return 0
	}
	conflicts, err := m.Check(staged)
	if err != nil || len(conflicts) == 0 {
		return 0
	}
	fmt.Fprintln(stderr, "dibs: commit blocked — these staged files are claimed by other agents:")
	seen := map[string]bool{}
	for _, c := range conflicts {
		line := fmt.Sprintf("  %-40s held by %s%s, expires in %s",
			c.Pattern, c.Holder.Agent, reasonSuffix(c.Holder.Reason), c.Holder.ExpiresIn(m.Now()).Round(1e9))
		if !seen[line] {
			seen[line] = true
			fmt.Fprintln(stderr, line)
		}
	}
	fmt.Fprintln(stderr, "Wait for the lease, coordinate via `dibs note`, or release/renegotiate. (`git commit --no-verify` overrides.)")
	return 1
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", reason)
}

func relToRepo(repo string, paths []string) []string {
	var out []string
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			out = append(out, filepath.ToSlash(p))
			continue
		}
		rel, err := filepath.Rel(repo, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // outside this repo — not ours to police
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

const claudeHookCommand = "dibs hook claude"

// InstallClaude adds the dibs PreToolUse hook to a Claude Code settings
// file (default: .claude/settings.json in the repo). The merge is additive
// and idempotent: existing settings and hooks are preserved, and a second
// install is a no-op.
func InstallClaude(settingsPath string) (changed bool, err error) {
	settings := map[string]any{}
	if raw, err := os.ReadFile(settingsPath); err == nil {
		if json.Unmarshal(raw, &settings) != nil {
			return false, fmt.Errorf("%s exists but is not valid JSON — fix it first, dibs will not overwrite it", settingsPath)
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	pre, _ := hooks["PreToolUse"].([]any)
	for _, entry := range pre {
		b, _ := json.Marshal(entry)
		if strings.Contains(string(b), claudeHookCommand) {
			return false, nil // already installed
		}
	}
	pre = append(pre, map[string]any{
		"matcher": "Write|Edit|MultiEdit|NotebookEdit",
		"hooks": []any{
			map[string]any{"type": "command", "command": claudeHookCommand},
		},
	})
	hooks["PreToolUse"] = pre
	settings["hooks"] = hooks

	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	return true, store.WriteAtomic(settingsPath, append(b, '\n'))
}

// UninstallClaude removes the dibs hook entry, leaving everything else as
// it was.
func UninstallClaude(settingsPath string) (changed bool, err error) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	settings := map[string]any{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return false, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	var kept []any
	for _, entry := range pre {
		b, _ := json.Marshal(entry)
		if strings.Contains(string(b), claudeHookCommand) {
			changed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !changed {
		return false, nil
	}
	if len(kept) > 0 {
		hooks["PreToolUse"] = kept
	} else {
		delete(hooks, "PreToolUse")
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	return true, store.WriteAtomic(settingsPath, append(b, '\n'))
}

// InstallPreCommit writes a git pre-commit hook that runs dibs. If a
// pre-commit hook already exists and isn't ours, it is left untouched and
// instructions are returned in the error.
func InstallPreCommit(st *store.Store) (string, error) {
	out, err := gitPath(st.Worktree)
	if err != nil {
		return "", err
	}
	path := filepath.Join(out, "pre-commit")
	if raw, err := os.ReadFile(path); err == nil {
		if strings.Contains(string(raw), "dibs hook pre-commit") {
			return path, nil // already installed
		}
		return "", fmt.Errorf("a pre-commit hook already exists at %s — add this line to it yourself:\n\n  dibs hook pre-commit || exit 1", path)
	}
	script := "#!/bin/sh\n# installed by dibs — blocks commits touching files other agents have claimed\nexec dibs hook pre-commit\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func gitPath(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-path", "hooks")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p, os.MkdirAll(p, 0o755)
}
