# Changelog

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
