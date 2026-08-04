# Security Policy

## Model

dibs is a local coordination tool. It runs with your user's permissions,
reads/writes files only inside the current repository's `.git` dir and
`.dibs/`, and opens no network connections. The MCP server speaks stdio to a
local client; there is no HTTP listener, no telemetry, and no credential
handling.

Coordination between agents is cooperative by design: leases protect
well-behaved agents (and hooks enforce them inside Claude Code and at commit
time), but nothing in dibs prevents a hostile local process from ignoring
them — the same trust boundary as the repository itself.

## Reporting a vulnerability

If you find something that breaks that model — path traversal out of the
repo, hook-driven command injection, lease bypass that hooks should have
caught — please email **farid.vosoughi.65@gmail.com** rather than opening a
public issue. You'll get a response within a few days, and credit in the
release notes if you want it.
