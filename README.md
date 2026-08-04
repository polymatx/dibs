<div align="center">

# dibs

**Call dibs on files. Run parallel coding agents without collisions.**

dibs is a coordination layer for AI coding agents that share a repository:
file claims with expiry, enforcement hooks, agent presence, handoff notes,
and a git-native knowledge base. One static binary — no server, no
database, no background processes.

[![CI](https://github.com/polymatx/dibs/actions/workflows/ci.yml/badge.svg)](https://github.com/polymatx/dibs/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/polymatx/dibs)](https://goreportcard.com/report/github.com/polymatx/dibs)
[![Go Reference](https://pkg.go.dev/badge/github.com/polymatx/dibs.svg)](https://pkg.go.dev/github.com/polymatx/dibs)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-server-6E56CF)](https://modelcontextprotocol.io)

[Overview](#overview) ·
[Demo](#demo) ·
[How it works](#how-it-works) ·
[Installation](#installation) ·
[Usage](#usage) ·
[Enforcement](#enforcement) ·
[Lessons](#lessons) ·
[MCP server](#mcp-server) ·
[Comparison](#comparison) ·
[FAQ](#faq)

![dibs demo](docs/demo.gif)

</div>

## Overview

Running several coding agents — Claude Code, Codex, Cline, Cursor — against
one repository in parallel is now a common workflow, typically with one git
worktree per agent. The sessions share no state: an agent cannot see what
its peers are doing, and two agents that touch the same files produce
silent overwrites and unmergeable diffs.

dibs provides the missing coordination primitives:

| Problem | What dibs does |
|---|---|
| Two agents edit the same file; last write wins. | Claims are checked before editing, and hooks can block colliding edits outright. |
| No visibility into other sessions. | `dibs status` lists every agent, claim, reason, and expiry. |
| A crashed agent leaves stale locks. | Claims are leases with a TTL. They expire on their own; there is nothing to clean up and no way to deadlock. |
| Handoffs between agents are ad hoc. | `dibs note` broadcasts a message to every agent on the repository, with per-agent read tracking. |
| Knowledge is lost between sessions. | Lessons are markdown files under `.dibs/lessons/`, committed with the repository and searched with BM25. |

## Demo

Two agents, one repository:

```console
$ dibs claim src/auth --reason "refactor auth" --agent alice
✓ claimed src/auth/** — lease 82fead6e as alice, expires in 29m

$ dibs claim src/auth/token.go --agent bob --reason "fix token bug"
✗ denied
  src/auth/token.go                    held by alice (refactor auth), expires in 29m
  wait, work elsewhere, or coordinate: dibs note "..."
$ echo $?
2

$ dibs claim src/api --agent bob --reason "new endpoints"
✓ claimed src/api/** — lease b7d31a5b as bob, expires in 29m

$ dibs status
agents
  bob (you)                active 0s ago on main — new endpoints
  alice                    active 0s ago on main — refactor auth

claims
  ● src/auth/**            alice (refactor auth)  expires in 29m
  ● src/api/**             bob (you) (new endpoints)  expires in 29m
```

With the Claude Code hook installed, an edit that violates a claim is
blocked before it happens, and the model is told why:

```console
$ echo '{"tool_name":"Edit","tool_input":{"file_path":"src/auth/token.go"}}' | dibs hook claude
dibs: src/auth/token.go is claimed by alice (refactor auth) — the claim expires in 29m.
Coordinate instead of colliding: wait for the lease, message them with `dibs note`,
or work on files outside their claim. Run `dibs status` to see all active claims.
$ echo $?
2
```

## How it works

dibs relies on a property of git worktrees: every worktree of a repository
shares a single common directory (`git rev-parse --git-common-dir`). State
written there is visible to all worktrees immediately, without commits, and
never appears in `git status`.

```
repo/.git/dibs/     coordination state — leases, presence, notes, journal
                    machine-local, shared by every worktree of the repo,
                    invisible to git

repo/.dibs/         knowledge — lessons/*.md
                    committed and reviewed like any other file, shared
                    with the team and CI through git itself
```

- A **claim** is a JSON file containing an agent name, a set of patterns, a
  reason, and an expiry. Conflict checks run under an advisory lock; expiry
  is evaluated lazily at read time. No daemon is required.
- **Identity** is resolved from `--agent`, then `DIBS_AGENT`, then a stable
  name derived from the worktree path — so each worktree has a consistent
  identity with zero configuration.
- **Patterns** are [doublestar](https://github.com/bmatcuk/doublestar)
  globs relative to the repository root. Claiming a directory claims its
  subtree. Glob-to-glob conflict detection is conservative: two globs whose
  static prefixes are nested are treated as conflicting. Precise claims
  produce precise conflicts.
- Every claim, denial, release, expiry, and note is appended to a JSONL
  journal (`dibs log`).

## Installation

```bash
go install github.com/polymatx/dibs/cmd/dibs@latest
```

Prebuilt binaries for Linux, macOS, and Windows (amd64/arm64) are on the
[releases page](https://github.com/polymatx/dibs/releases). Building from
source requires Go 1.25+; runtime requires git.

## Usage

```bash
cd your-repo
dibs init                     # create .dibs/, print next steps
dibs init --agents-md         # append the coordination protocol to AGENTS.md
dibs hook install claude      # enforce claims in Claude Code
dibs hook install pre-commit  # enforce claims at commit time (any agent)
```

| Command | Description |
|---|---|
| `dibs claim <pattern>... [--reason ...] [--ttl 30m]` | Lease files or globs. Default TTL 30m, maximum 24h. |
| `dibs release [pattern]... [--all]` | Release leases. Bare `dibs release` releases everything you hold. |
| `dibs renew [--ttl 30m]` | Extend all of your leases. |
| `dibs check <path>...` | Report whether paths are covered by another agent's lease. |
| `dibs status [--json]` | Agents, claims, and unread note count. |
| `dibs note <message>` | Broadcast a note to all agents on the repository. |
| `dibs notes` | Read notes and mark them read. |
| `dibs log [-n 20]` | Show recent journal events. |
| `dibs lesson add\|list\|show\|search` | Manage the lessons knowledge base. |
| `dibs mcp` | Run the MCP server on stdio. |
| `dibs hook install\|uninstall` | Manage enforcement hooks. |
| `dibs whoami`, `dibs version` | Identity and build information. |

Exit codes: `0` ok/free · `1` error · `2` denied or held by another agent.
All state-reading commands accept `--json` for scripting.

## Enforcement

Protocol adherence that depends on a model remembering instructions
degrades under context pressure. dibs therefore supports enforcement at two
levels, both opt-in:

- **Claude Code** — `dibs hook install claude` registers a `PreToolUse`
  hook. When an agent attempts to edit a file covered by another agent's
  claim, the tool call is blocked (exit code 2) and the model receives a
  message naming the holder, their reason, and the expiry. Agents
  consistently adjust course when given this context.
- **Any agent** — `dibs hook install pre-commit` registers a git hook that
  rejects commits touching files claimed by another agent.

Hook installation is additive and idempotent: existing entries in
`.claude/settings.json` and existing git hooks are preserved, and
`dibs hook uninstall claude` removes exactly what was added. Both hooks
fail open — if dibs cannot run, editing and committing proceed normally.

## Lessons

Lessons capture what an agent learned so the next session does not
rediscover it:

```bash
dibs lesson add "rate-limit middleware must register after auth" \
  --body "The limiter reads ctx.User set by the auth guard. Registering it earlier panics." \
  --tags middleware,auth

dibs lesson search "why does the rate limiter panic"
 1.91 rate-limit middleware must register after auth [rate-limit-middleware-...]
       The limiter reads ctx.User set by the auth guard. Registering it earlier panics...
```

Lessons are markdown files with YAML frontmatter under `.dibs/lessons/`:

- **Shared through git** — the whole team and CI receive them via
  `git pull`; there is no per-machine database to synchronize.
- **Reviewable** — knowledge changes go through the same pull-request
  review as code, and can be corrected or reverted like code.
- **Searchable without infrastructure** — BM25 ranking with light stemming
  over title, tags, and body, computed in memory per query. At the scale of
  a repository's accumulated lessons, lexical search is instant and
  requires no embedding model or vector store.

## MCP server

`dibs mcp` runs a stdio MCP server, so agents coordinate through typed
tools rather than shell commands. The server instructions and tool
descriptions encode the protocol (claim before editing, release when done,
leave notes, record lessons):

| Tool | Purpose |
|---|---|
| `dibs_claim` | Claim patterns with a reason and TTL; returns GRANTED or DENIED with holder details. |
| `dibs_check` | Report whether paths are free or held. |
| `dibs_release` | Release claims. |
| `dibs_status` | Agents, claims, and unread notes. |
| `dibs_note` / `dibs_notes` | Broadcast and read handoff notes. |
| `dibs_lesson_add` / `dibs_lesson_search` | Write and search the knowledge base. |

Client configuration:

```bash
# Claude Code
claude mcp add dibs -- dibs mcp
```

```toml
# Codex (~/.codex/config.toml)
[mcp_servers.dibs]
command = "dibs"
args = ["mcp"]
```

Any MCP client with stdio transport is supported.

## Comparison

Adjacent tools solve different problems; the table shows where dibs fits.

| | **dibs** | Server-based orchestration platforms | beads | claude-squad / vibe-kanban |
|---|---|---|---|---|
| Primary job | coordination + shared lessons | memory + orchestration suites | issue tracking as agent memory | running and managing sessions |
| File claims with expiry | ✅ | ✅ via a central server | ❌ | ❌ |
| Blocks colliding edits | ✅ hooks | ❌ advisory | ❌ | ❌ (isolation via worktrees) |
| Cross-worktree visibility | ✅ instant, via `.git` common dir | while the server is running | n/a | creates the worktrees |
| Runtime dependencies | none | server + web UI + database | none | varies |
| Memory search | BM25 over in-repo files | vector embeddings | issue graph | ❌ |
| Installation | single static binary | docker compose stack | single binary | binary / app |

dibs composes with these tools rather than replacing them. It pairs
naturally with [beads](https://github.com/gastownhall/beads) for task
tracking (`dibs claim src/auth --reason "bd-142: refactor auth"`) and with
any session manager, since coordination is independent of how sessions are
launched.

## Design principles and limitations

- **No infrastructure.** No daemon, no server, no database, no telemetry.
  All state is plain JSON and markdown on disk. If dibs is removed, a
  repository is left exactly as it was, minus one directory.
- **Leases, not locks.** Every claim expires. A crashed or abandoned agent
  cannot block a repository.
- **Fail-open enforcement.** A malfunctioning hook must never prevent
  legitimate work; enforcement errs on the side of allowing edits.
- **Cooperative trust model.** dibs coordinates well-behaved agents and
  enforces claims inside Claude Code and at commit time. It is not a
  security boundary against a process that deliberately bypasses it.
- **Machine-local coordination.** Live claims are per machine, which
  matches the dominant workflow of parallel agents on one workstation.
  Lessons travel across machines through git. Cross-machine live
  coordination is on the roadmap.
- **Conservative conflict detection.** Overlapping glob prefixes are
  treated as conflicts even when the globs could be disjoint. False
  positives are cheap; silent collisions are not.

## FAQ

**What if an agent never calls dibs at all?**
Install the hooks. The Claude Code hook checks every file-modifying tool
call regardless of what the model remembers; the pre-commit hook catches
everything else at commit time.

**What happens when an agent crashes while holding a claim?**
The claim expires after its TTL (default 30 minutes). The expiry is
recorded in the journal.

**Is dibs useful with a single agent?**
Yes, in a reduced role: lessons provide persistent knowledge across
sessions, and the journal provides an audit trail of what was claimed and
when.

**Why not `git lfs locks` or lock files committed to the repository?**
LFS locks require a server and do not expire; committed lock files create
commit noise and are invisible to uncommitted worktrees. Neither supports
glob patterns or communicates context to the blocked agent.

## Roadmap

- `dibs tui` — live terminal dashboard of agents and claims
- `dibs claim --wait` — block until a lease becomes free
- npm wrapper package for `npx dibs` and MCP registry listing
- Cross-machine coordination backend (opt-in)
- Cursor and OpenCode enforcement recipes

## Contributing

Contributions are welcome. The project intentionally stays small: stdlib
plus three dependencies, no daemons, no databases. See
[CONTRIBUTING.md](CONTRIBUTING.md) for guidelines and
[docs/protocol.md](docs/protocol.md) for the full coordination protocol.

## License

[MIT](LICENSE) © 2026 [polymatx](https://github.com/polymatx)
