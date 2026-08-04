# Changelog

## v0.1.1 — 2026-08-04

Hardening release: every finding from an adversarial review of v0.1.0,
each with a regression test.

**Correctness of the core invariant**

- The store lock is now a kernel file lock (flock / LockFileEx) instead of
  a lockfile with stale-detection — a crashed process can no longer leave
  state that two contenders race to steal, and there is no heuristic to
  get wrong.
- A claim on a path that does not exist in the claimer's worktree is now
  claimed as its subtree (`migrations` → `migrations/**`), closing a hole
  where such a claim and an overlapping glob claim (`**/*.sql`) could both
  be granted.
- Absolute paths are resolved into the repository for both claims and
  checks; previously `dibs check /abs/path/to/held/file` reported FREE and
  an absolute claim granted a useless lease. Paths outside the repository
  are rejected (claims) or ignored (checks).
- `check` and the hooks now match paths literally instead of parsing them
  as globs: file names containing `[`, `{`, `*` are handled correctly in
  both directions (no false blocks, no invisible files).
- Lease IDs are re-drawn on collision instead of silently overwriting a
  live lease.

**Enforcement and identity**

- Hooks no longer block an agent on its own claims when the hook runs
  without the `DIBS_AGENT` its claims were made under: an auto-derived
  identity treats leases from its own worktree as its own.
- Agent names are sanitized (`[A-Za-z0-9._-]`, max 64) — a hostile
  `--agent ../../name` can no longer write outside the state dir.
- `dibs hook pre-commit` exits 2 for "held by another agent", matching the
  documented exit-code contract.
- `dibs hook install claude` no longer panics on a `null` settings file
  and refuses (instead of clobbering) settings with unexpected shapes;
  uninstall removes only the dibs command, preserving user hooks grouped
  in the same entry.

**Robustness**

- Journal detail values are truncated so an oversized note can never make
  the journal unreadable; reads report truncation instead of silently
  dropping the newest events; rotation never rewrites from a partial read.
- Notes: timestamps are assigned under the lock and the read cursor
  advances only to the newest note actually seen — a handoff can no longer
  be marked read without being surfaced.
- `--` ends flag parsing (`dibs note -- "-1 on that approach"` works;
  `dibs release -- --all` no longer releases everything).
- Lesson files: CRLF frontmatter parses correctly, search snippets never
  split a multi-byte rune, and concurrent same-title adds cannot overwrite
  each other (O_EXCL).

## v0.1.0 — 2026-08-04

Initial release.

- **Claims**: `claim` / `release` / `renew` / `check` — TTL leases on files
  and doublestar globs, conservative glob-conflict detection, lazy expiry,
  no daemon.
- **Worktree-native**: coordination state in the git common dir — every
  worktree of the repo shares it instantly, nothing to commit.
- **Enforcement**: Claude Code `PreToolUse` hook blocks edits to files
  claimed by another agent (fail-open); git `pre-commit` hook for everyone
  else. Additive, idempotent installers.
- **Presence & notes**: `status` shows who's active where and why;
  `note`/`notes` broadcast handoffs with per-agent read cursors.
- **Journal**: append-only JSONL audit of claims, denials, releases,
  expiries and notes; `dibs log`.
- **Lessons**: markdown knowledge base in `.dibs/lessons/`, committed and
  shared via git; BM25 search with light stemming.
- **MCP server**: 8 tools over stdio (`dibs mcp`) with protocol-teaching
  descriptions; works with Claude Code, Codex, Cursor, and any MCP client.
