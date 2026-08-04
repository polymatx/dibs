<div align="center">

# dibs

**Call dibs on files. Run parallel coding agents without collisions.**

The missing coordination layer for AI coding agents — file claims, presence,
handoffs, and shared lessons. One binary. No server, no database, no
embeddings. State lives inside your `.git` dir.

[![CI](https://github.com/polymatx/dibs/actions/workflows/ci.yml/badge.svg)](https://github.com/polymatx/dibs/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/polymatx/dibs)](https://goreportcard.com/report/github.com/polymatx/dibs)
[![Go Reference](https://pkg.go.dev/badge/github.com/polymatx/dibs.svg)](https://pkg.go.dev/github.com/polymatx/dibs)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-server-6E56CF)](https://modelcontextprotocol.io)

[Why](#why-dibs-exists) ·
[60-second demo](#the-60-second-demo) ·
[How it works](#how-it-works) ·
[Quickstart](#quickstart) ·
[Enforcement](#enforcement-claims-that-actually-block) ·
[Lessons](#lessons-memory-that-travels-with-the-repo) ·
[vs. alternatives](#how-dibs-compares) ·
[FAQ](#faq)

![dibs demo — two agents claim, collide politely, and coordinate](docs/demo.gif)

</div>

---

Running two or three Claude Code / Codex sessions in parallel is the new
normal. So is this:

> Agent A refactors `src/auth`. Agent B "fixes a quick bug" in the same
> files. You get a merge disaster, or worse — silent overwrites.

Existing fixes want you to run an orchestration **server** with a database
and an embedding model. dibs is the opposite bet: coordination should be
**one tiny binary and a handful of files in your repo**.

```
you                          agent "alice"                agent "bob"
 │                            │                            │
 │                            │ dibs claim src/auth/**     │
 │                            │ ✓ granted (30m)            │
 │                            │                            │ dibs claim src/auth/token.go
 │                            │                            │ ✗ denied — alice holds it
 │                            │                            │   (refactor auth, 29m left)
 │                            │                            │ → works on src/api instead
 │        dibs status         │                            │
 │  every claim, every agent  │ dibs release               │ dibs note "api types changed"
```

## Why dibs exists

| Without dibs | With dibs |
|---|---|
| Two agents edit the same file; last write wins. | Claims are checked **before** editing — and can hard-block colliding edits. |
| "What is the other session doing right now?" | `dibs status`: every agent, claim, reason, and expiry at a glance. |
| A crashed agent holds imaginary locks forever. | Claims are **leases** — they expire on their own. No deadlocks, no daemon. |
| Handoffs happen in your head. | `dibs note "renamed User.ID — regenerate mocks"` reaches every agent. |
| Every session relearns the same gotchas. | Lessons live in `.dibs/lessons/*.md` — committed, searched, shared via git. |

## The 60-second demo

Real output, three agents, one repo:

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

And the part that makes it *enforcement* rather than etiquette — with the
Claude Code hook installed, an agent that ignores the protocol gets stopped
mid-edit and told why:

```console
$ echo '{"tool_name":"Edit","tool_input":{"file_path":"src/auth/token.go"}}' | dibs hook claude
dibs: src/auth/token.go is claimed by alice (refactor auth) — the claim expires in 29m.
Coordinate instead of colliding: wait for the lease, message them with `dibs note`,
or work on files outside their claim. Run `dibs status` to see all active claims.
$ echo $?
2   # Claude Code blocks the edit and shows the agent this message
```

## How it works

The trick is a property of git that almost nobody uses: **every worktree of
a repository shares one `.git` common directory.**

```
repo/.git/dibs/          ← coordination state (leases, presence, notes, journal)
│                          machine-local, invisible to git status,
│                          visible to EVERY worktree instantly — no commits,
│                          no daemon, no database
│
repo/.dibs/              ← knowledge (lessons/*.md)
                           plain markdown, committed and reviewed like code,
                           shared with your team and CI through git itself
```

- A **claim** is a JSON file with a TTL. Conflict checking happens under an
  advisory lockfile; expiry is evaluated lazily on read. Nothing runs in the
  background.
- Every agent gets a **stable identity** derived from its worktree path
  (`brisk-otter`, `keen-wren`, …) — or set `DIBS_AGENT` / `--agent`.
- Patterns are doublestar globs. Claiming a directory claims its subtree.
  Glob-vs-glob conflict checking is deliberately conservative (nested static
  prefixes collide) — predictable beats clever when the cost is a corrupted
  refactor.

## Quickstart

```bash
go install github.com/polymatx/dibs/cmd/dibs@latest
# or grab a binary from https://github.com/polymatx/dibs/releases
```

```bash
cd your-repo
dibs init                     # .dibs/ + state dir, prints next steps
dibs init --agents-md         # teach agents the protocol via AGENTS.md
dibs hook install claude      # hard-block colliding edits in Claude Code
dibs hook install pre-commit  # enforce at commit time for any agent
```

Give agents first-class tools (recommended — works with anything that
speaks MCP):

```bash
claude mcp add dibs -- dibs mcp
```

```toml
# Codex (~/.codex/config.toml)
[mcp_servers.dibs]
command = "dibs"
args = ["mcp"]
```

The MCP server exposes 8 tools — `dibs_claim`, `dibs_check`,
`dibs_release`, `dibs_status`, `dibs_note`, `dibs_notes`,
`dibs_lesson_add`, `dibs_lesson_search` — with descriptions that teach the
model the protocol: *claim before editing, release when done, leave notes,
record lessons.*

## Enforcement: claims that actually block

Protocols that rely on the model remembering to behave degrade under
context pressure. dibs closes the loop:

- **Claude Code** — `dibs hook install claude` adds a `PreToolUse` hook.
  Edits to files claimed by another agent are **blocked** (exit 2) and the
  model is told who holds them, why, and for how long — so it adapts
  instead of retrying blindly.
- **Any agent** — `dibs hook install pre-commit` refuses commits that touch
  someone else's claim.
- Hooks **fail open**: if dibs breaks, your editor doesn't. A blocked edit
  is a coaching moment; a bricked repo is a bug.

Installation is additive and idempotent — existing hooks and settings in
`.claude/settings.json` are preserved, and `dibs hook uninstall claude`
removes exactly what was added.

## Lessons: memory that travels with the repo

```bash
dibs lesson add "rate-limit middleware must register after auth" \
  --body "The limiter reads ctx.User set by the auth guard. Registering it earlier panics." \
  --tags middleware,auth

dibs lesson search "why does the rate limiter panic"
 1.91 rate-limit middleware must register after auth [rate-limit-middleware-...]
       The limiter reads ctx.User set by the auth guard. Registering it earlier panics...
```

Lessons are markdown files with YAML frontmatter under `.dibs/lessons/`.
That one decision buys a lot:

- **Team-shared by `git pull`** — no per-machine database to sync, no
  server for CI agents to reach.
- **Reviewable** — knowledge lands in PRs like code. Wrong lesson? Comment,
  fix, or `git revert` it.
- **Searchable without ML** — BM25 with light stemming over title/tags/body.
  For the hundreds of lessons a real repo accumulates, lexical search built
  in-memory per query is instant and needs zero infrastructure. If you're
  at the scale where you need embeddings for your lessons file, you need a
  different tool — see below.

## How dibs compares

Different tools do different jobs — this is the honest map:

| | **dibs** | server-based orchestration platforms | beads | claude-squad / vibe-kanban |
|---|---|---|---|---|
| Job | coordination + light memory | memory + orchestration suites | issue tracking as agent memory | running/managing sessions |
| File claims with expiry | ✅ leases | ✅ via a central server | ❌ | ❌ |
| **Blocks colliding edits** | ✅ hooks | ❌ advisory | ❌ | ❌ (isolation via worktrees) |
| Works across git worktrees instantly | ✅ `.git` common dir | only while the server is up | n/a | creates the worktrees |
| Needs a running server | **no** | yes — server + web UI + database | no | no |
| Database | **none** — JSON + markdown | SQLite / vector store | SQLite + JSONL | varies |
| Memory search | BM25, in-repo files | vector embeddings | issue graph | ❌ |
| Install | 1 static binary | docker compose stack | 1 binary | 1 binary / app |

**Use dibs *with* [beads](https://github.com/gastownhall/beads)**: beads
tracks *what to do*; dibs keeps agents from colliding *while doing it*.
A natural pairing: `dibs claim src/auth --reason "bd-142: refactor auth"`.

## The protocol agents follow

`dibs init --agents-md` appends this to your `AGENTS.md` (also in
[docs/protocol.md](docs/protocol.md)):

1. **Claim before editing** — `dibs claim <paths> --reason "..."`. Denied?
   Work elsewhere, wait, or coordinate. Never edit claimed files.
2. **Release when done** — `dibs release`. Renew long work with `dibs renew`.
3. **Start sessions with** `dibs status` and `dibs notes`.
4. **Hand off with notes** — `dibs note "schema changed, regenerate types"`.
5. **Record lessons** — `dibs lesson add` for gotchas future agents need;
   `dibs lesson search` before spelunking unfamiliar code.

## CLI reference

| Command | What it does |
|---|---|
| `dibs claim <pattern>... [--reason] [--ttl 30m]` | Lease files/globs (exit 2 if denied) |
| `dibs release [pattern]... [--all]` | Drop your leases (bare = all) |
| `dibs renew [--ttl]` | Extend all your leases |
| `dibs check <path>...` | Free or held? (exit 2 = held) |
| `dibs status [--json]` | Agents, claims, unread notes |
| `dibs note <msg>` / `dibs notes` | Broadcast / read handoffs |
| `dibs log [-n 20]` | Coordination journal (who did what) |
| `dibs lesson add\|list\|show\|search` | Durable knowledge in `.dibs/lessons/` |
| `dibs mcp` | MCP server on stdio |
| `dibs hook install claude\|pre-commit` | Turn on enforcement |
| `dibs whoami` / `dibs version` | Identity / build info |

Every state-changing action is recorded in an append-only journal:
`dibs log` answers "what have the agents been doing?".

## FAQ

**What if an agent never runs dibs at all?**
Install the hooks. The Claude Code hook checks every `Edit`/`Write` no
matter what the model forgot; the pre-commit hook catches everything else
at commit time. Agents that *do* speak the protocol get the richer
experience (denials with context, notes, lessons).

**What happens when an agent crashes holding a claim?**
Nothing — that's the point. Claims are leases with TTLs (default 30m, max
24h). They expire; the journal records it; life goes on.

**Is this a task tracker / orchestrator / session manager?**
No. beads tracks tasks; claude-squad and vibe-kanban run sessions; dibs
coordinates file access and shares knowledge between whatever you already
run. It composes with all of them.

**Multiple machines?**
Lessons already travel with git. Live coordination is per-machine today —
by design, since parallel agents overwhelmingly share one box. A sync
backend for distributed teams is on the roadmap.

**Why not `git lfs locks` or lock files in the repo?**
Server-bound (LFS) or commit-noise (files in the tree), and neither
expires, path-globs, coaches the agent, or works uncommitted across
worktrees.

**Single agent — still useful?**
Yes: lessons (persistent memory), the journal (audit trail), and notes
(handoff to *future you* or your teammate's agent).

## Roadmap

- `dibs tui` — live dashboard of agents/claims (watch mode)
- `dibs claim --wait` — block until a lease frees up
- Cross-machine coordination backend (opt-in, still zero-config)
- Cursor / OpenCode enforcement recipes
- beads integration sugar (`--reason bd-142` auto-links)

## Contributing

Small tool, small rules: `go test ./... -race` must pass, `gofmt` clean,
no new dependencies without a very good reason. See
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) — © 2026 [Farid Vosoughi (polymatx)](https://github.com/polymatx)

---

<div align="center">

*If your agents stopped stepping on each other today, consider a ⭐ — it
helps other multi-agent developers find dibs.*

</div>
