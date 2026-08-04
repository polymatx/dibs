# The dibs coordination protocol

This is the contract agents follow when several of them share one
repository. Teach it to your agents either by appending it to `AGENTS.md`
(`dibs init --agents-md`) or by wiring the MCP server (`claude mcp add dibs
-- dibs mcp`), whose tool descriptions carry the same rules.

## The loop

1. **Orient.** Start a session with `dibs status` (who is active, what is
   claimed) and `dibs notes` (anything handed off to you).
2. **Claim before editing.** `dibs claim <paths/globs> --reason "what and
   why" [--ttl 30m]`. Claim what you will actually touch — a directory for
   a refactor, single files for a surgical fix. Precise claims mean fewer
   false conflicts.
3. **Respect denials.** A denial names the holder, their reason, and the
   expiry. Choose: work elsewhere, wait it out, or coordinate with
   `dibs note`. Never edit files another agent has claimed — with the hooks
   installed you can't anyway.
4. **Renew or release.** Long task? `dibs renew`. Done with an area?
   `dibs release` — don't squat on files you've finished with. Crashed
   agents cost nothing: leases expire on their own.
5. **Hand off.** When your change affects others — schema changes, renamed
   exports, regenerated code — broadcast it: `dibs note "..."`.
6. **Leave the campsite better.** Non-obvious discoveries become lessons:
   `dibs lesson add "title" --body "the gotcha and the fix"`. Before
   working in unfamiliar territory: `dibs lesson search <topic>`.

## The AGENTS.md snippet

`dibs init --agents-md` appends exactly this:

```markdown
## Coordinating with other agents (dibs)

Multiple agents may be working on this repository in parallel. Coordinate
through dibs (CLI or the "dibs" MCP tools):

- Before editing files, claim them: `dibs claim <paths/globs> --reason "what you're doing"`.
  If the claim is denied, someone else is working there — pick another task,
  wait, or coordinate. Do not edit files another agent has claimed.
- Release your claim when done: `dibs release`. Claims auto-expire, renew
  long work with `dibs renew`.
- Check before touching a hot path: `dibs check <path>`.
- Start sessions with `dibs status` and `dibs notes`; leave a note for
  others when your change affects them: `dibs note "..."`.
- Record non-obvious discoveries for future agents:
  `dibs lesson add "title" --body "..."`; search prior knowledge with
  `dibs lesson search <query>`.
```

## Identity

Agents are named. Resolution order:

1. `--agent NAME` flag
2. `DIBS_AGENT` environment variable (set it per session/worktree when you
   want stable, meaningful names)
3. A deterministic name derived from the worktree path (`brisk-otter`) —
   zero configuration, stable across sessions in the same worktree.

Two sessions sharing one worktree share one identity — which is usually
what you want (they'd collide at the filesystem level anyway).

## Exit codes (for scripts and hooks)

- `0` — ok / free
- `1` — error
- `2` — denied / held by another agent
