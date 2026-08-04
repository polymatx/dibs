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

// Normalize canonicalizes a claim pattern: slashes, absolute paths mapped
// into the repo, no escaping the repo, and directory-like literals
// expanded to "dir/**". A meta-free pattern stays literal only when it
// names an existing regular file; a directory — or a path that does not
// exist (yet, or in this worktree) — is claimed as its whole subtree,
// which errs on the conservative side. Note that "dir/**" also matches
// "dir" itself in doublestar semantics.
// repoRoot may be "" in contexts (like tests) with no filesystem to consult.
func Normalize(p, repoRoot string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty pattern")
	}
	if filepath.IsAbs(p) {
		if repoRoot == "" {
			return "", fmt.Errorf("absolute pattern %q cannot be resolved without a repository root", p)
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
			return "", fmt.Errorf("path %q is outside the repository", p)
		}
		p = filepath.ToSlash(rel)
	}
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
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
	if !hasMeta(clean) {
		isFile := false
		if repoRoot != "" {
			if st, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(clean))); err == nil && !st.IsDir() {
				isFile = true
			}
		}
		if !isFile || strings.HasSuffix(p, "/") {
			clean += "/**"
		}
	}
	return clean, nil
}

// NormalizePath canonicalizes a concrete filesystem path for conflict
// checking. Unlike Normalize it never reinterprets the path as a glob and
// never expands it — a path is data, not a pattern.
func NormalizePath(p, repoRoot string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		if repoRoot == "" {
			return "", fmt.Errorf("absolute path %q cannot be resolved without a repository root", p)
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
			return "", fmt.Errorf("path %q is outside the repository", p)
		}
		p = rel
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.ToSlash(p)))
	clean = strings.TrimPrefix(clean, "./")
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q escapes the repository", p)
	}
	return clean, nil
}

// PathCoveredBy reports whether a concrete path is covered by a claim
// pattern. The path is matched literally: glob metacharacters in file
// names ("docs/[draft].md") have no special meaning.
func PathCoveredBy(path, pattern string) bool {
	if hasMeta(pattern) {
		if ok, _ := doublestar.Match(pattern, path); ok {
			return true
		}
		// A directory path that contains the pattern's subtree is a
		// conflict: touching "src" touches "src/auth/**".
		return isPathPrefix(path, staticPrefix(pattern))
	}
	return isPathPrefix(pattern, path) || isPathPrefix(path, pattern)
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
