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

	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "src/auth", want: "src/auth/**"},     // existing dir → subtree
		{in: "./src/auth/", want: "src/auth/**"},  // trailing slash → subtree
		{in: "/src/auth/**", want: "src/auth/**"}, // leading slash stripped
		{in: "src/auth/token.go", want: "src/auth/token.go"},
		{in: "**/*.sql", want: "**/*.sql"},
		{in: ".", want: "**"},                        // whole repo
		{in: "src/../src/auth", want: "src/auth/**"}, // cleaned
		{in: "../outside", wantErr: true},            // escapes repo
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
