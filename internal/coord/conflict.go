package coord

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Pattern semantics
//
// Patterns are doublestar globs relative to the repo root ("src/auth/**",
// "cmd/dibs/main.go", "**/*.sql"). A pattern that names an existing
// directory is expanded to cover its whole subtree ("src/auth" becomes
// "src/auth/**") — claiming a directory means claiming what's inside it.
//
// Two patterns conflict when they can plausibly cover the same file. dibs
// deliberately errs on the conservative side for glob-vs-glob checks: it
// compares the globs' static prefixes (the leading segments before any
// wildcard), and nested prefixes count as a conflict. "src/**" vs
// "src/auth/**" conflicts; "src/auth/**" vs "src/api/**" does not. A glob
// with no static prefix ("**/*.sql") conflicts with everything — which is
// the honest reading of "I'm touching SQL files anywhere".

// Normalize canonicalizes a claim pattern: slashes, no leading "./" or "/",
// no escaping the repo, and directory literals expanded to "dir/**".
// repoRoot may be "" in contexts (like tests) with no filesystem to consult.
func Normalize(p, repoRoot string) (string, error) {
	p = strings.TrimSpace(p)
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", fmt.Errorf("empty pattern")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("pattern %q escapes the repository", p)
	}
	if clean == "." {
		clean = "**"
	}
	if !doublestar.ValidatePattern(clean) {
		return "", fmt.Errorf("invalid glob pattern %q", p)
	}
	if !hasMeta(clean) && repoRoot != "" {
		if st, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(clean))); err == nil && st.IsDir() {
			clean += "/**"
		}
	}
	if strings.HasSuffix(p, "/") && !hasMeta(clean) { // trailing slash: caller means a directory
		clean += "/**"
	}
	return clean, nil
}

// Intersect reports whether two normalized patterns can cover the same file.
func Intersect(a, b string) bool {
	if a == b {
		return true
	}
	am, bm := hasMeta(a), hasMeta(b)
	switch {
	case !am && !bm:
		// Two literals: same file, or one names a parent of the other.
		return isPathPrefix(a, b) || isPathPrefix(b, a)
	case am && !bm:
		return literalHitsGlob(b, a)
	case !am && bm:
		return literalHitsGlob(a, b)
	default:
		// Glob vs glob: conservative static-prefix containment.
		pa, pb := staticPrefix(a), staticPrefix(b)
		return isPathPrefix(pa, pb) || isPathPrefix(pb, pa)
	}
}

// literalHitsGlob reports whether a literal path intersects a glob: either
// the glob matches the path, or the path names a directory that contains
// the glob's subtree (claiming literal "src" intersects "src/x/**").
func literalHitsGlob(literal, glob string) bool {
	if ok, _ := doublestar.Match(glob, literal); ok {
		return true
	}
	return isPathPrefix(literal, staticPrefix(glob))
}

// isPathPrefix reports whether a equals b or is a path-segment prefix of b.
// The empty path is the repo root and prefixes everything.
func isPathPrefix(a, b string) bool {
	if a == "" {
		return true
	}
	return a == b || strings.HasPrefix(b, a+"/")
}

// staticPrefix returns the leading wildcard-free segments of a pattern.
func staticPrefix(p string) string {
	segs := strings.Split(p, "/")
	var out []string
	for _, s := range segs {
		if hasMeta(s) {
			break
		}
		out = append(out, s)
	}
	return strings.Join(out, "/")
}

func hasMeta(s string) bool {
	return strings.ContainsAny(s, "*?[{")
}
