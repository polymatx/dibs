package coord

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src", "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "auth", "token.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "src/auth", want: "src/auth/**"},                 // existing dir → subtree
		{in: "./src/auth/", want: "src/auth/**"},              // trailing slash → subtree
		{in: "src/auth/token.go", want: "src/auth/token.go"},  // existing file stays literal
		{in: "migrations", want: "migrations/**"},             // nonexistent → conservative subtree
		{in: "docs/PLAN.md", want: "docs/PLAN.md/**"},         // nonexistent file-looking path too
		{in: "**/*.sql", want: "**/*.sql"},
		{in: ".", want: "**"},                                 // whole repo
		{in: "src/../src/auth", want: "src/auth/**"},          // cleaned
		{in: filepath.Join(repo, "src/auth/token.go"), want: "src/auth/token.go"}, // absolute inside repo
		{in: "/somewhere/else/entirely", wantErr: true},       // absolute outside repo
		{in: "../outside", wantErr: true},                     // escapes repo
		{in: "", wantErr: true},
		{in: "src/[", wantErr: true}, // invalid glob
	}
	for _, c := range cases {
		got, err := Normalize(c.in, repo)
		if c.wantErr {
			if err == nil {
				t.Errorf("Normalize(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Normalize(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIntersect(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		// literal vs literal
		{"src/a.go", "src/a.go", true},
		{"src/a.go", "src/b.go", false},
		{"src", "src/a.go", true}, // literal parent covers child

		// literal vs glob
		{"src/auth/token.go", "src/auth/**", true},
		{"src/api/routes.go", "src/auth/**", false},
		{"src/auth/token.go", "src/**", true},
		{"README.md", "src/**", false},
		{"db/schema.sql", "**/*.sql", true},
		{"db/schema.go", "**/*.sql", false},
		{"src", "src/auth/**", true},           // literal dir containing the glob's subtree
		{"src/auth/deep/x.go", "src/*", false}, // single star doesn't cross /
		{"src/auth", "src/*", true},

		// glob vs glob (conservative static-prefix rule)
		{"src/auth/**", "src/auth/**", true},
		{"src/auth/**", "src/**", true},      // nested prefixes
		{"src/auth/**", "src/api/**", false}, // disjoint prefixes
		{"**/*.sql", "src/auth/**", true},    // rootless glob conflicts with all
		{"src/**/*.go", "src/**/*.md", true}, // same prefix: conservative conflict
		{"docs/**", "src/**", false},
	}
	for _, c := range cases {
		if got := Intersect(c.a, c.b); got != c.want {
			t.Errorf("Intersect(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		// Intersection must be symmetric.
		if got := Intersect(c.b, c.a); got != c.want {
			t.Errorf("Intersect(%q, %q) = %v, want %v (asymmetry)", c.b, c.a, got, c.want)
		}
	}
}

// TestUnexpandedLiteralCannotDodgeGlob is the review regression: a claim
// on a directory that does not exist in the claimer's worktree must still
// conflict with a glob that can cover the same files. Both being granted
// violates the core invariant.
func TestUnexpandedLiteralCannotDodgeGlob(t *testing.T) {
	repo := t.TempDir() // "migrations" does not exist here
	a, err := Normalize("migrations", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !Intersect(a, "**/*.sql") {
		t.Fatalf("Normalize(%q)=%q must intersect **/*.sql", "migrations", a)
	}
	b, err := Normalize("src/auth", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !Intersect(b, "src/**/*.go") {
		t.Fatalf("Normalize(%q)=%q must intersect src/**/*.go", "src/auth", b)
	}
}

func TestPathCoveredBy(t *testing.T) {
	cases := []struct {
		path, pattern string
		want          bool
	}{
		{"src/auth/token.go", "src/auth/**", true},
		{"src/api/routes.go", "src/auth/**", false},
		{"docs/[draft].md", "docs/d.md", false},   // brackets in a FILE NAME are literal
		{"docs/[draft].md", "docs/**", true},      // but globs still cover it
		{"docs/[x.md", "**", true},                // unclosed bracket: still just a file name
		{"docs/d.md", "docs/d.md", true},
		{"src", "src/auth/**", true},              // touching a dir touches its claimed subtree
		{"README.md", "src/**/*.md", false},       // glob prefix not an ancestor
		{"src/main.go", "src/**/*.md", false},     // same prefix but glob cannot match the file
	}
	for _, c := range cases {
		if got := PathCoveredBy(c.path, c.pattern); got != c.want {
			t.Errorf("PathCoveredBy(%q, %q) = %v, want %v", c.path, c.pattern, got, c.want)
		}
	}
}

func TestStaticPrefix(t *testing.T) {
	cases := map[string]string{
		"src/auth/**":   "src/auth",
		"src/**/*.go":   "src",
		"**/*.sql":      "",
		"cmd/dibs/x.go": "cmd/dibs/x.go",
		"a/{b,c}/d":     "a",
	}
	for in, want := range cases {
		if got := staticPrefix(in); got != want {
			t.Errorf("staticPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
