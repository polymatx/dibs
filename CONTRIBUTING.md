# Contributing to dibs

Thanks for helping keep parallel agents out of each other's lanes.

## Ground rules

dibs stays small on purpose. The bar for new code:

- `go test ./... -race` passes, `gofmt -l .` is empty, `go vet ./...` is clean.
- **No new dependencies without a very good reason.** The whole tool is
  stdlib + doublestar + the MCP SDK + yaml; that's a feature.
- No daemons, no databases, no background processes. If a feature needs one,
  it probably belongs in a different tool.
- Hooks must **fail open**. dibs coaching an agent is great; dibs bricking an
  editor is a bug, always.
- User-visible messages should tell the reader what to do next, not just
  what went wrong.

## Getting started

```bash
git clone https://github.com/polymatx/dibs
cd dibs
go test ./... -race
go build ./cmd/dibs
```

Try your build against a scratch repo:

```bash
mkdir /tmp/demo && cd /tmp/demo && git init
dibs init
dibs claim src --reason "trying dibs" --agent alice
dibs claim src --agent bob   # should be denied
```

## Reporting bugs

Open an issue with the output of `dibs version`, your OS, and — if it's a
coordination bug — the contents of `.git/dibs/` (it's all plain JSON).

## Feature requests

Check the roadmap in the README first. "Small, sharp, zero-infra" beats
"complete": features that add a server, a database, or a config file need an
exceptional case.
