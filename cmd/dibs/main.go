// Command dibs is coordination for parallel coding agents: call dibs on
// files before you edit them, and agents stop colliding.
package main

import (
	"os"

	"github.com/polymatx/dibs/internal/cli"
)

const usage = `dibs — call dibs on files. Coordination for parallel coding agents.

Usage:
  dibs init                                       Set up .dibs/ and show next steps
  dibs claim <pattern>... [--reason ...] [--ttl 30m]
                                                  Claim files/globs before editing
  dibs release [pattern]... [--all]               Release your claims (bare = all)
  dibs renew [--ttl 30m]                          Extend all your leases
  dibs check <path>...                            Are these files free? (exit 2 = held)
  dibs status [--json]                            Agents, claims and notes at a glance
  dibs note <message>                             Broadcast a note to other agents
  dibs notes                                      Read notes (marks them read)
  dibs log [-n 20]                                Recent coordination events
  dibs lesson add|list|show|search ...            Durable lessons in .dibs/lessons/
  dibs mcp                                        Run the MCP server (stdio)
  dibs hook install claude|pre-commit             Enforce claims (block colliding edits)
  dibs hook uninstall claude                      Remove the Claude Code hook
  dibs whoami                                     Print this agent's name
  dibs version                                    Version info

Common flags (any position after the subcommand):
  --agent NAME    Act as NAME (default: $DIBS_AGENT, else a stable per-worktree name)
  --json          Machine-readable output where supported

Exit codes: 0 ok/free · 1 error · 2 claim denied / path held by another agent
`

func main() {
	os.Exit(cli.Run(os.Args[1:], usage))
}
