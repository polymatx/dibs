// Package mcpserver exposes dibs over the Model Context Protocol so agents
// coordinate through first-class tools instead of shelling out. Tool
// descriptions and server instructions double as the protocol: they teach
// any MCP client the claim → work → release → learn loop.
package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/polymatx/dibs/internal/coord"
	"github.com/polymatx/dibs/internal/lessons"
	"github.com/polymatx/dibs/internal/version"
)

const instructions = `dibs coordinates multiple coding agents working on the same repository.

The loop:
1. Before editing files, call dibs_claim with the paths/globs you will touch and a short reason. If DENIED, the response tells you who holds those files and until when — work on something else, wait, or send them a note; do not edit claimed files.
2. Call dibs_check when unsure whether specific files are free.
3. When you finish (or stop working on) an area, call dibs_release.
4. Leave a note with dibs_note when your change affects others ("regenerated API types — pull before editing api/"). Check dibs_notes when you start.
5. When you learn something future agents should know (a gotcha, a convention, a hard-won fix), save it with dibs_lesson_add and search prior knowledge with dibs_lesson_search before diving into unfamiliar code.

Call dibs_status at the start of a session to see active agents, their claims, and unread notes.`

// Serve runs the stdio MCP server until ctx is cancelled.
func Serve(ctx context.Context, m *coord.Manager) error {
	s := New(m)
	return s.Run(ctx, &mcp.StdioTransport{})
}

// New builds the dibs MCP server bound to one agent identity.
func New(m *coord.Manager) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "dibs",
		Title:   "dibs — coordination for parallel coding agents",
		Version: version.Version,
	}, &mcp.ServerOptions{Instructions: instructions})

	text := func(format string, args ...any) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}}}, nil, nil
	}

	type claimArgs struct {
		Patterns []string `json:"patterns" jsonschema:"files or globs to claim, relative to the repo root, e.g. src/auth/** or cmd/main.go"`
		Reason   string   `json:"reason,omitempty" jsonschema:"short human-readable description of what you are doing, shown to other agents"`
		TTL      string   `json:"ttl,omitempty" jsonschema:"how long you need the claim, as a Go duration like 30m or 2h (default 30m); it auto-expires"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_claim",
		Description: "Claim files or glob patterns before editing them, so parallel agents don't collide. Returns GRANTED with an expiry, or DENIED with who holds the files, why, and until when. Re-claiming the same patterns renews your lease.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in claimArgs) (*mcp.CallToolResult, any, error) {
		ttl := coord.DefaultTTL
		if in.TTL != "" {
			d, err := time.ParseDuration(in.TTL)
			if err != nil {
				return text("invalid ttl %q: use a Go duration like 30m, 1h30m", in.TTL)
			}
			ttl = d
		}
		lease, conflicts, err := m.Claim(in.Patterns, in.Reason, ttl)
		if err != nil {
			return nil, nil, err
		}
		if len(conflicts) > 0 {
			return text("DENIED — %s", describeConflicts(conflicts, m.Now()))
		}
		return text("GRANTED lease %s on %s — expires in %s. Release it with dibs_release when done.",
			lease.ID, strings.Join(lease.Patterns, ", "), lease.ExpiresIn(m.Now()).Round(time.Second))
	})

	type checkArgs struct {
		Paths []string `json:"paths" jsonschema:"repo-relative file paths to check against other agents' active claims"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_check",
		Description: "Check whether files are claimed by another agent. FREE means safe to edit; HELD tells you who has them and until when.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in checkArgs) (*mcp.CallToolResult, any, error) {
		conflicts, err := m.Check(in.Paths)
		if err != nil {
			return nil, nil, err
		}
		if len(conflicts) == 0 {
			return text("FREE — no other agent has claimed these paths.")
		}
		return text("HELD — %s", describeConflicts(conflicts, m.Now()))
	})

	type releaseArgs struct {
		Patterns []string `json:"patterns,omitempty" jsonschema:"patterns to release (as originally claimed); omit and set all=true to release everything you hold"`
		All      bool     `json:"all,omitempty" jsonschema:"release every lease you hold"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_release",
		Description: "Release your claims when you finish working on an area, so other agents can proceed. Leases also expire on their own.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in releaseArgs) (*mcp.CallToolResult, any, error) {
		if !in.All && len(in.Patterns) == 0 {
			in.All = true
		}
		released, err := m.Release(nil, in.Patterns, in.All)
		if err != nil {
			return nil, nil, err
		}
		if len(released) == 0 {
			return text("nothing to release — you hold no matching leases.")
		}
		var parts []string
		for _, l := range released {
			parts = append(parts, strings.Join(l.Patterns, ", "))
		}
		return text("released: %s", strings.Join(parts, "; "))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_status",
		Description: "See the whole picture: active agents on this repo, every live claim with expiry, and your unread note count. Call this at the start of a session.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		leases, err := m.ActiveLeases()
		if err != nil {
			return nil, nil, err
		}
		agents, err := m.Agents()
		if err != nil {
			return nil, nil, err
		}
		unread, err := m.UnreadNotes()
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "you are: %s\n\nagents (%d):\n", m.Agent, len(agents))
		for _, a := range agents {
			fmt.Fprintf(&b, "  %-18s last active %s ago", a.Name, m.Now().Sub(a.LastSeen).Round(time.Second))
			if a.LastReason != "" {
				fmt.Fprintf(&b, " — %s", a.LastReason)
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "\nactive claims (%d):\n", len(leases))
		if len(leases) == 0 {
			b.WriteString("  none — everything is free\n")
		}
		for _, l := range leases {
			owner := l.Agent
			if l.Agent == m.Agent {
				owner += " (you)"
			}
			fmt.Fprintf(&b, "  %-40s %s%s, expires in %s\n",
				strings.Join(l.Patterns, ", "), owner, reasonSuffix(l.Reason), l.ExpiresIn(m.Now()).Round(time.Second))
		}
		if len(unread) > 0 {
			fmt.Fprintf(&b, "\nyou have %d unread note(s) — read them with dibs_notes\n", len(unread))
		}
		return text("%s", b.String())
	})

	type noteArgs struct {
		Message string `json:"message" jsonschema:"short broadcast message for other agents, e.g. 'renamed User.ID to User.UUID — regenerate mocks'"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_note",
		Description: "Broadcast a short note to every agent working on this repo. Use it for handoffs and heads-ups that affect others.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in noteArgs) (*mcp.CallToolResult, any, error) {
		if _, err := m.PostNote(in.Message); err != nil {
			return nil, nil, err
		}
		return text("note posted.")
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_notes",
		Description: "Read notes from other agents (marks them read). Check at the start of a session and before starting new work.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		unread, err := m.UnreadNotes()
		if err != nil {
			return nil, nil, err
		}
		all, err := m.Notes()
		if err != nil {
			return nil, nil, err
		}
		if err := m.MarkNotesRead(); err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d unread, %d total (last 7 days):\n", len(unread), len(all))
		shown := all
		if len(shown) > 20 {
			shown = shown[len(shown)-20:]
		}
		for _, n := range shown {
			marker := " "
			for _, u := range unread {
				if u.ID == n.ID {
					marker = "*"
				}
			}
			fmt.Fprintf(&b, "%s [%s] %s: %s\n", marker, n.CreatedAt.Format("Jan 02 15:04"), n.From, n.Message)
		}
		return text("%s", b.String())
	})

	type lessonAddArgs struct {
		Title string   `json:"title" jsonschema:"one-line summary of the lesson, e.g. 'rate-limit middleware must register after auth'"`
		Body  string   `json:"body" jsonschema:"the lesson itself: the gotcha, the fix, the context a future agent needs"`
		Tags  []string `json:"tags,omitempty" jsonschema:"optional topic tags like auth, migrations, ci"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_lesson_add",
		Description: "Save a lesson for future agents as a markdown file under .dibs/lessons/ (committed with the repo, shared via git). Record gotchas, conventions, and hard-won fixes — not routine work logs.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in lessonAddArgs) (*mcp.CallToolResult, any, error) {
		slug, err := lessons.Add(m.St.Shared+"/lessons", lessons.Lesson{Title: in.Title, Body: in.Body, Tags: in.Tags, Agent: m.Agent})
		if err != nil {
			return nil, nil, err
		}
		return text("lesson saved as .dibs/lessons/%s.md — commit it so your team and CI agents get it too.", slug)
	})

	type lessonSearchArgs struct {
		Query string `json:"query" jsonschema:"what you want to know, e.g. 'webhook retry handling'"`
		Limit int    `json:"limit,omitempty" jsonschema:"max results (default 5)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "dibs_lesson_search",
		Description: "Search lessons recorded by previous agents before working in unfamiliar territory. Returns ranked matches with snippets.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in lessonSearchArgs) (*mcp.CallToolResult, any, error) {
		hits, err := lessons.Search(m.St.Shared+"/lessons", in.Query, in.Limit)
		if err != nil {
			return nil, nil, err
		}
		if len(hits) == 0 {
			return text("no matching lessons. (Lessons live in .dibs/lessons/ — add what you learn with dibs_lesson_add.)")
		}
		var b strings.Builder
		for _, h := range hits {
			fmt.Fprintf(&b, "%.2f  %s [%s]", h.Score, h.Lesson.Title, h.Lesson.Slug)
			if len(h.Lesson.Tags) > 0 {
				fmt.Fprintf(&b, " tags: %s", strings.Join(h.Lesson.Tags, ","))
			}
			if h.Snippet != "" {
				fmt.Fprintf(&b, "\n      %s", h.Snippet)
			}
			b.WriteString("\n")
		}
		b.WriteString("\nRead a full lesson at .dibs/lessons/<slug>.md")
		return text("%s", b.String())
	})

	return s
}

func describeConflicts(conflicts []coord.Conflict, now time.Time) string {
	var parts []string
	seen := map[string]bool{}
	for _, c := range conflicts {
		s := fmt.Sprintf("%s is held by %s%s, expires in %s",
			c.Pattern, c.Holder.Agent, reasonSuffix(c.Holder.Reason), c.Holder.ExpiresIn(now).Round(time.Second))
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "; ") + ". Work elsewhere, wait, or coordinate via dibs_note."
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", reason)
}
