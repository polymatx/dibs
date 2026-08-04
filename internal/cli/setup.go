package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/polymatx/dibs/internal/hook"
	"github.com/polymatx/dibs/internal/mcpserver"
	"github.com/polymatx/dibs/internal/store"
	"github.com/polymatx/dibs/internal/version"
)

// agentsSnippet is what init offers to put in AGENTS.md / CLAUDE.md so
// agents actually follow the protocol.
const agentsSnippet = `## Coordinating with other agents (dibs)

Multiple agents may be working on this repository in parallel. Coordinate
through dibs (CLI or the "dibs" MCP tools):

- Before editing files, claim them: ` + "`dibs claim <paths/globs> --reason \"what you're doing\"`" + `.
  If the claim is denied, someone else is working there — pick another task,
  wait, or coordinate. Do not edit files another agent has claimed.
- Release your claim when done: ` + "`dibs release`" + `. Claims auto-expire, renew
  long work with ` + "`dibs renew`" + `.
- Check before touching a hot path: ` + "`dibs check <path>`" + `.
- Start sessions with ` + "`dibs status`" + ` and ` + "`dibs notes`" + `; leave a note for
  others when your change affects them: ` + "`dibs note \"...\"`" + `.
- Record non-obvious discoveries for future agents:
  ` + "`dibs lesson add \"title\" --body \"...\"`" + `; search prior knowledge with
  ` + "`dibs lesson search <query>`" + `.
`

const dibsDirReadme = `# .dibs

Durable, git-shared knowledge for the coding agents working on this repo,
managed by [dibs](https://github.com/polymatx/dibs).

- ` + "`lessons/`" + ` — what agents learned here: gotchas, conventions, fixes.
  Plain markdown; committed, reviewed, and shared through git like any code.

Coordination state (claims, presence, notes) is NOT here — it lives under
` + "`.git/dibs/`" + `, machine-local and invisible to git, shared instantly across
every worktree of this repository.
`

func cmdInit(args []string) error {
	fs, agent := newFlagSet("init")
	withClaudeHook := fs.Bool("claude-hook", false, "also install the Claude Code enforcement hook")
	withPreCommit := fs.Bool("pre-commit", false, "also install the git pre-commit enforcement hook")
	writeAgentsMD := fs.Bool("agents-md", false, "append the coordination snippet to AGENTS.md")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	lessonsDir := filepath.Join(m.St.Shared, "lessons")
	if err := os.MkdirAll(lessonsDir, 0o755); err != nil {
		return err
	}
	readme := filepath.Join(m.St.Shared, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		if err := os.WriteFile(readme, []byte(dibsDirReadme), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("%s .dibs/ ready, coordination state in %s\n", green("✓"), dim(m.St.State))
	fmt.Printf("%s you are %s\n", green("✓"), bold(m.Agent))

	if *writeAgentsMD {
		path := filepath.Join(m.St.Repo, "AGENTS.md")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		fmt.Fprintf(f, "\n%s", agentsSnippet)
		f.Close()
		fmt.Printf("%s appended coordination protocol to AGENTS.md\n", green("✓"))
	}
	if *withClaudeHook {
		if err := installClaudeHook(m.St); err != nil {
			return err
		}
	}
	if *withPreCommit {
		path, err := hook.InstallPreCommit(m.St)
		if err != nil {
			return err
		}
		fmt.Printf("%s git pre-commit hook installed at %s\n", green("✓"), dim(path))
	}
	if !*writeAgentsMD && !*withClaudeHook {
		fmt.Println("\nnext steps:")
		fmt.Println("  dibs init --agents-md      teach agents the protocol via AGENTS.md")
		fmt.Println("  dibs hook install claude   make claims enforced, not advisory (Claude Code)")
		fmt.Println("  dibs hook install pre-commit   enforce at commit time (any agent)")
		fmt.Println("  claude mcp add dibs -- dibs mcp    let agents coordinate via MCP tools")
	}
	return nil
}

func cmdHook(args []string) (int, error) {
	if len(args) == 0 {
		return ExitErr, fmt.Errorf("usage: dibs hook claude|pre-commit | dibs hook install claude|pre-commit | dibs hook uninstall claude")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "claude":
		fs, agent := newFlagSet("hook claude")
		if _, err := parseMixed(fs, rest); err != nil {
			return ExitErr, err
		}
		m, err := manager(*agent)
		if err != nil {
			return ExitOK, nil // fail open: never brick edits because dibs can't load
		}
		return hook.RunClaude(m, os.Stdin, os.Stderr), nil
	case "pre-commit":
		fs, agent := newFlagSet("hook pre-commit")
		if _, err := parseMixed(fs, rest); err != nil {
			return ExitErr, err
		}
		m, err := manager(*agent)
		if err != nil {
			return ExitOK, nil
		}
		return hook.RunPreCommit(m, os.Stderr), nil
	case "install":
		if len(rest) == 0 {
			return ExitErr, fmt.Errorf("usage: dibs hook install claude|pre-commit")
		}
		st, err := store.Open(".")
		if err != nil {
			return ExitErr, err
		}
		switch rest[0] {
		case "claude":
			return ExitOK, installClaudeHook(st)
		case "pre-commit":
			path, err := hook.InstallPreCommit(st)
			if err != nil {
				return ExitErr, err
			}
			fmt.Printf("%s git pre-commit hook installed at %s\n", green("✓"), dim(path))
			return ExitOK, nil
		}
		return ExitErr, fmt.Errorf("unknown hook %q", rest[0])
	case "uninstall":
		st, err := store.Open(".")
		if err != nil {
			return ExitErr, err
		}
		settings := filepath.Join(st.Repo, ".claude", "settings.json")
		changed, err := hook.UninstallClaude(settings)
		if err != nil {
			return ExitErr, err
		}
		if changed {
			fmt.Printf("%s removed dibs hook from %s\n", green("✓"), settings)
		} else {
			fmt.Println(dim("dibs hook was not installed"))
		}
		return ExitOK, nil
	default:
		return ExitErr, fmt.Errorf("unknown hook subcommand %q", sub)
	}
}

func installClaudeHook(st *store.Store) error {
	settings := filepath.Join(st.Repo, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		return err
	}
	changed, err := hook.InstallClaude(settings)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("%s Claude Code hook installed in %s — edits to claimed files are now blocked\n", green("✓"), dim(settings))
	} else {
		fmt.Println(dim("Claude Code hook already installed"))
	}
	return nil
}

func cmdMCP(args []string) error {
	fs, agent := newFlagSet("mcp")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	m.Touch()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// Banner on stderr: stdout is the JSON-RPC channel.
	fmt.Fprintf(os.Stderr, "dibs %s — MCP server on stdio, agent %s, repo %s\n", version.Version, m.Agent, m.St.Repo)
	return mcpserver.Serve(ctx, m)
}

func cmdWhoami(args []string) error {
	fs, agent := newFlagSet("whoami")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	fmt.Println(m.Agent)
	return nil
}

func cmdVersion() {
	fmt.Printf("dibs %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
}

// PrintAgentsSnippet is used by docs generation and tests.
func PrintAgentsSnippet() string { return agentsSnippet }
