// Package store resolves where dibs keeps its files and provides small,
// safe filesystem primitives (atomic writes, an advisory lock, JSON helpers).
//
// dibs state lives in two places, on purpose:
//
//   - State: <git-common-dir>/dibs — coordination state (leases, presence,
//     notes, journal). Every git worktree of a repository shares the same
//     common dir, so leases taken in one worktree are visible in all others
//     instantly, without commits, daemons, or databases. It never shows up
//     in `git status`.
//
//   - Shared: <repo-root>/.dibs — durable knowledge (lessons). These are
//     plain files meant to be committed, reviewed in PRs, and shared with
//     your team and CI through git itself.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Store locates the dibs directories for one repository.
type Store struct {
	State    string // <git-common-dir>/dibs — machine-local coordination state
	Shared   string // <repo-root>/.dibs — committed knowledge (lessons)
	Repo     string // repo root of the current worktree
	Worktree string // same as Repo; named for clarity at call sites
}

// ErrNotRepo is returned when the current directory is not inside a git repo.
var ErrNotRepo = errors.New("not inside a git repository (dibs coordinates agents through the repo's .git dir)")

// Open resolves the store for the repository containing dir (usually ".").
// DIBS_STATE_DIR overrides the coordination dir, mainly for tests and
// unusual setups.
func Open(dir string) (*Store, error) {
	top, err := gitOut(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotRepo
	}
	common, err := gitOut(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, ErrNotRepo
	}
	// Relative output (e.g. ".git") is relative to the directory git ran in.
	if !filepath.IsAbs(common) {
		abs, err := filepath.Abs(filepath.Join(dir, common))
		if err != nil {
			return nil, err
		}
		common = abs
	}
	state := filepath.Join(common, "dibs")
	if env := os.Getenv("DIBS_STATE_DIR"); env != "" {
		state = env
	}
	return &Store{
		State:    state,
		Shared:   filepath.Join(top, ".dibs"),
		Repo:     top,
		Worktree: top,
	}, nil
}

// At builds a store from explicit paths. Tests use this.
func At(state, repo string) *Store {
	return &Store{State: state, Shared: filepath.Join(repo, ".dibs"), Repo: repo, Worktree: repo}
}

// EnsureState creates the coordination directories.
func (s *Store) EnsureState() error {
	for _, d := range []string{s.State, filepath.Join(s.State, "leases"), filepath.Join(s.State, "agents"), filepath.Join(s.State, "notes")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Branch reports the current branch of the worktree ("HEAD" when detached,
// "" outside git).
func (s *Store) Branch() string {
	out, err := gitOut(s.Worktree, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// WriteJSON writes v as indented JSON atomically (temp file + rename).
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, append(b, '\n'))
}

// ReadJSON decodes the JSON file at path into v.
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// WriteAtomic writes data to path via a temp file in the same directory,
// so readers never observe a partial file.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// AppendLine appends one line to path (creating it if needed).
func AppendLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ListJSON decodes every *.json file in dir into T, skipping files that
// fail to parse (a crashed writer must never brick coordination).
func ListJSON[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []T
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var v T
		if err := ReadJSON(filepath.Join(dir, e.Name()), &v); err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// WithLock runs fn while holding an exclusive lock on the store. The lock
// is a kernel file lock (flock on Unix, LockFileEx on Windows), so it is
// released automatically when the holding process exits or crashes — there
// are no stale lockfiles to detect and no stealing heuristics to get
// wrong. Critical sections in dibs are a few milliseconds, so contention
// is short-lived.
func (s *Store) WithLock(fn func() error) error {
	const (
		timeout = 5 * time.Second
		poll    = 25 * time.Millisecond
	)
	if err := os.MkdirAll(s.State, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.State, "lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := tryLock(f)
		if err != nil {
			return fmt.Errorf("locking %s: %w", path, err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the dibs lock at %s", path)
		}
		time.Sleep(poll)
	}
	defer unlock(f)
	return fn()
}
