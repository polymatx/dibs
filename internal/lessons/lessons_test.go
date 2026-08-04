package lessons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddGetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	slug, err := Add(dir, Lesson{
		Title: "Rate limiter must register after auth!",
		Body:  "The limiter reads ctx.User set by the auth guard.",
		Tags:  []string{"middleware", "auth"},
		Agent: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if slug != "rate-limiter-must-register-after-auth" {
		t.Fatalf("slug = %q", slug)
	}
	got, err := Get(dir, slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Rate limiter must register after auth!" ||
		got.Body != "The limiter reads ctx.User set by the auth guard." ||
		len(got.Tags) != 2 || got.Agent != "alice" || got.CreatedAt.IsZero() {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestAddDuplicateTitlesGetDistinctSlugs(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Add(dir, Lesson{Title: "same", Body: "one"})
	s2, err := Add(dir, Lesson{Title: "same", Body: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if s1 == s2 {
		t.Fatalf("duplicate slugs: %q", s1)
	}
}

func TestListToleratesHandWrittenFiles(t *testing.T) {
	dir := t.TempDir()
	// A file someone dropped in without frontmatter must still be usable.
	os.WriteFile(filepath.Join(dir, "raw-note.md"), []byte("just some text"), 0o644)
	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Title != "raw note" || all[0].Body != "just some text" {
		t.Fatalf("hand-written file mishandled: %+v", all)
	}
}

func TestSearchRanksRelevance(t *testing.T) {
	dir := t.TempDir()
	Add(dir, Lesson{Title: "rate-limit middleware must register after auth",
		Body: "The limiter panics with a nil user when registered before the auth guard.", Tags: []string{"middleware"}})
	Add(dir, Lesson{Title: "migrations need postgres build tag",
		Body: "go run ./cmd/migrate fails silently without -tags postgres."})
	Add(dir, Lesson{Title: "CI cache key includes go.sum",
		Body: "Stale caches broke builds after dependency bumps."})

	hits, err := Search(dir, "why does the rate limiter panic", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Lesson.Slug != "rate-limit-middleware-must-register-after-auth" {
		t.Fatalf("wrong top hit: %+v", hits)
	}
	if hits[0].Snippet == "" || !strings.Contains(strings.ToLower(hits[0].Snippet), "panic") {
		t.Fatalf("snippet should show the matching line, got %q", hits[0].Snippet)
	}
	// Irrelevant queries return nothing rather than noise.
	none, _ := Search(dir, "zzz qqq xyzzy", 5)
	if len(none) != 0 {
		t.Fatalf("nonsense query matched: %+v", none)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello, World!":       "hello-world",
		"  spaces   galore  ": "spaces-galore",
		"ALL CAPS":            "all-caps",
		"چیزی به فارسی":       "lesson", // non-latin collapses; fallback stays usable
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
