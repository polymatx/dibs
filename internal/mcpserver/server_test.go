package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/polymatx/dibs/internal/coord"
	"github.com/polymatx/dibs/internal/store"
)

func connect(t *testing.T, m *coord.Manager) *mcp.ClientSession {
	t.Helper()
	srv := New(m)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func managers(t *testing.T) (*coord.Manager, *coord.Manager) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, "src", "auth"), 0o755)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	a := &coord.Manager{St: store.At(state, repo), Agent: "alice", Now: func() time.Time { return now }}
	b := &coord.Manager{St: store.At(state, repo), Agent: "bob", Now: func() time.Time { return now }}
	a.St.EnsureState()
	return a, b
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var out strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out.WriteString(tc.Text)
		}
	}
	return out.String()
}

func TestToolsAreRegistered(t *testing.T) {
	alice, _ := managers(t)
	cs := connect(t, alice)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"dibs_claim": false, "dibs_check": false, "dibs_release": false, "dibs_status": false,
		"dibs_note": false, "dibs_notes": false, "dibs_lesson_add": false, "dibs_lesson_search": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestClaimCheckAcrossAgents(t *testing.T) {
	alice, bob := managers(t)
	aliceSess := connect(t, alice)
	bobSess := connect(t, bob)

	res, err := aliceSess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "dibs_claim",
		Arguments: map[string]any{"patterns": []string{"src/auth"}, "reason": "refactor auth", "ttl": "45m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if txt := textOf(t, res); !strings.Contains(txt, "GRANTED") {
		t.Fatalf("alice claim: %q", txt)
	}

	res, err = bobSess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "dibs_claim",
		Arguments: map[string]any{"patterns": []string{"src/auth/token.go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt := textOf(t, res)
	if !strings.Contains(txt, "DENIED") || !strings.Contains(txt, "alice") || !strings.Contains(txt, "refactor auth") {
		t.Fatalf("bob should be denied with context: %q", txt)
	}

	res, _ = bobSess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "dibs_check",
		Arguments: map[string]any{"paths": []string{"README.md"}},
	})
	if txt := textOf(t, res); !strings.Contains(txt, "FREE") {
		t.Fatalf("free path: %q", txt)
	}
}

func TestLessonToolsRoundtrip(t *testing.T) {
	alice, bob := managers(t)
	aliceSess := connect(t, alice)
	bobSess := connect(t, bob)

	if _, err := aliceSess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "dibs_lesson_add",
		Arguments: map[string]any{
			"title": "webhooks retry with exponential backoff",
			"body":  "The queue redelivers on 5xx; handlers must be idempotent.",
			"tags":  []string{"webhooks"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := bobSess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "dibs_lesson_search",
		Arguments: map[string]any{"query": "how do webhook retries work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if txt := textOf(t, res); !strings.Contains(txt, "webhooks retry") {
		t.Fatalf("search missed the lesson: %q", txt)
	}
}
