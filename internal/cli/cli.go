// Package cli implements the dibs command line.
package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/polymatx/dibs/internal/coord"
	"github.com/polymatx/dibs/internal/store"
)

// Exit codes: 0 ok/free, 1 error, 2 denied/held — scripts and hooks branch
// on 2 to mean "someone else has it".
const (
	ExitOK     = 0
	ExitErr    = 1
	ExitDenied = 2
)

// Run dispatches a subcommand and returns the process exit code.
func Run(args []string, usage string) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return ExitOK
	}
	cmd, rest := args[0], args[1:]
	var err error
	code := ExitOK
	switch cmd {
	case "init":
		err = cmdInit(rest)
	case "claim":
		code, err = cmdClaim(rest)
	case "release":
		err = cmdRelease(rest)
	case "renew":
		err = cmdRenew(rest)
	case "check":
		code, err = cmdCheck(rest)
	case "status":
		err = cmdStatus(rest)
	case "note":
		err = cmdNote(rest)
	case "notes":
		err = cmdNotes(rest)
	case "log":
		err = cmdLog(rest)
	case "lesson":
		err = cmdLesson(rest)
	case "hook":
		code, err = cmdHook(rest)
	case "mcp", "serve":
		err = cmdMCP(rest)
	case "whoami":
		err = cmdWhoami(rest)
	case "version", "--version", "-v":
		cmdVersion()
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "dibs: unknown command %q\n\n%s", cmd, usage)
		return ExitErr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dibs: "+err.Error())
		return ExitErr
	}
	return code
}

// newFlagSet builds a flag set with the shared --agent flag.
func newFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	agent := fs.String("agent", "", "act as this agent name (default: $DIBS_AGENT or a stable per-worktree name)")
	return fs, agent
}

// parseMixed lets flags and positional arguments appear in any order
// (`dibs claim src/auth --reason x` and `dibs claim --reason x src/auth`
// both work), which the stdlib flag package doesn't do on its own.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	isBool := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			isBool[f.Name] = true
		}
	})
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if !strings.Contains(name, "=") && !isBool[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return pos, nil
}

// manager opens the store for the current directory and binds an identity.
func manager(agent string) (*coord.Manager, error) {
	st, err := store.Open(".")
	if err != nil {
		return nil, err
	}
	if err := st.EnsureState(); err != nil {
		return nil, err
	}
	return coord.NewManager(st, agent), nil
}

func cmdClaim(args []string) (int, error) {
	fs, agent := newFlagSet("claim")
	reason := fs.String("reason", "", "what you're doing, shown to other agents")
	ttl := fs.Duration("ttl", coord.DefaultTTL, "lease duration (auto-expires)")
	jsonOut := fs.Bool("json", false, "JSON output")
	patterns, err := parseMixed(fs, args)
	if err != nil {
		return ExitErr, err
	}
	if len(patterns) == 0 {
		return ExitErr, fmt.Errorf("usage: dibs claim <pattern>... [--reason ...] [--ttl 30m]")
	}
	m, err := manager(*agent)
	if err != nil {
		return ExitErr, err
	}
	lease, conflicts, err := m.Claim(patterns, *reason, *ttl)
	if err != nil {
		return ExitErr, err
	}
	if len(conflicts) > 0 {
		if *jsonOut {
			emitJSON(os.Stdout, map[string]any{"granted": false, "conflicts": conflicts})
			return ExitDenied, nil
		}
		fmt.Println(red("✗ denied"))
		for _, c := range conflicts {
			fmt.Printf("  %s %s\n", pad(c.Pattern, 36), holderLine(c, m.Now()))
		}
		fmt.Println(dim("  wait, work elsewhere, or coordinate: dibs note \"...\""))
		return ExitDenied, nil
	}
	if *jsonOut {
		emitJSON(os.Stdout, map[string]any{"granted": true, "lease": lease})
		return ExitOK, nil
	}
	fmt.Printf("%s %s — lease %s as %s, expires in %s\n",
		green("✓ claimed"), bold(strings.Join(lease.Patterns, ", ")), lease.ID, bold(m.Agent), human(lease.ExpiresIn(m.Now())))
	return ExitOK, nil
}

func cmdRelease(args []string) error {
	fs, agent := newFlagSet("release")
	all := fs.Bool("all", false, "release everything you hold")
	patterns, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if !*all && len(patterns) == 0 {
		*all = true // bare `dibs release` means "I'm done"
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	released, err := m.Release(nil, patterns, *all)
	if err != nil {
		return err
	}
	if len(released) == 0 {
		fmt.Println(dim("nothing to release"))
		return nil
	}
	for _, l := range released {
		fmt.Printf("%s %s\n", green("✓ released"), strings.Join(l.Patterns, ", "))
	}
	return nil
}

func cmdRenew(args []string) error {
	fs, agent := newFlagSet("renew")
	ttl := fs.Duration("ttl", coord.DefaultTTL, "new lease duration from now")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	renewed, err := m.Renew(*ttl)
	if err != nil {
		return err
	}
	if len(renewed) == 0 {
		fmt.Println(dim("no active leases to renew"))
		return nil
	}
	for _, l := range renewed {
		fmt.Printf("%s %s — now expires in %s\n", green("✓ renewed"), strings.Join(l.Patterns, ", "), human(l.ExpiresIn(m.Now())))
	}
	return nil
}

func cmdCheck(args []string) (int, error) {
	fs, agent := newFlagSet("check")
	jsonOut := fs.Bool("json", false, "JSON output")
	paths, err := parseMixed(fs, args)
	if err != nil {
		return ExitErr, err
	}
	if len(paths) == 0 {
		return ExitErr, fmt.Errorf("usage: dibs check <path>...")
	}
	m, err := manager(*agent)
	if err != nil {
		return ExitErr, err
	}
	conflicts, err := m.Check(paths)
	if err != nil {
		return ExitErr, err
	}
	if *jsonOut {
		emitJSON(os.Stdout, map[string]any{"free": len(conflicts) == 0, "conflicts": conflicts})
		if len(conflicts) > 0 {
			return ExitDenied, nil
		}
		return ExitOK, nil
	}
	if len(conflicts) == 0 {
		fmt.Println(green("✓ free"))
		return ExitOK, nil
	}
	for _, c := range conflicts {
		fmt.Printf("%s %s %s\n", yellow("✋ held"), pad(c.Pattern, 36), holderLine(c, m.Now()))
	}
	return ExitDenied, nil
}

func cmdStatus(args []string) error {
	fs, agent := newFlagSet("status")
	jsonOut := fs.Bool("json", false, "JSON output")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	m, err := manager(*agent)
	if err != nil {
		return err
	}
	m.Touch()
	leases, err := m.ActiveLeases()
	if err != nil {
		return err
	}
	agents, err := m.Agents()
	if err != nil {
		return err
	}
	unread, err := m.UnreadNotes()
	if err != nil {
		return err
	}
	if *jsonOut {
		return emitJSON(os.Stdout, map[string]any{
			"you": m.Agent, "agents": agents, "leases": leases, "unread_notes": len(unread),
		})
	}
	fmt.Printf("%s %s\n\n", dim("you are"), bold(m.Agent))
	fmt.Println(bold("agents"))
	if len(agents) == 0 {
		fmt.Println(dim("  none seen this week"))
	}
	for _, a := range agents {
		name := a.Name
		if a.Name == m.Agent {
			name += " (you)"
		}
		line := fmt.Sprintf("  %s active %s ago", pad(name, 24), human(m.Now().Sub(a.LastSeen)))
		if a.Branch != "" {
			line += dim(" on " + a.Branch)
		}
		if a.LastReason != "" {
			line += dim(" — " + a.LastReason)
		}
		fmt.Println(line)
	}
	fmt.Println()
	fmt.Println(bold("claims"))
	if len(leases) == 0 {
		fmt.Println(dim("  none — everything is free"))
	}
	for _, l := range leases {
		owner := l.Agent
		mark := yellow("●")
		if l.Agent == m.Agent {
			owner += " (you)"
			mark = green("●")
		}
		fmt.Printf("  %s %s %s%s %s\n", mark, pad(strings.Join(l.Patterns, ", "), 38), owner,
			dim(reasonSuffix(l.Reason)), dim("expires in "+human(l.ExpiresIn(m.Now()))))
	}
	if len(unread) > 0 {
		fmt.Printf("\n%s %d unread note(s) — %s\n", yellow("✉"), len(unread), dim("dibs notes"))
	}
	return nil
}

func holderLine(c coord.Conflict, now time.Time) string {
	return fmt.Sprintf("held by %s%s, expires in %s",
		bold(c.Holder.Agent), dim(reasonSuffix(c.Holder.Reason)), human(c.Holder.ExpiresIn(now)))
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}
