package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWriteReadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	in := map[string]string{"hello": "world"}
	if err := WriteJSON(path, in); err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := ReadJSON(path, &out); err != nil {
		t.Fatal(err)
	}
	if out["hello"] != "world" {
		t.Fatalf("roundtrip lost data: %v", out)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("stray files after atomic write: %v", entries)
	}
}

func TestListJSONSkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	WriteJSON(filepath.Join(dir, "good.json"), map[string]int{"n": 1})
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{nope"), 0o644)
	os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644)

	got, err := ListJSON[map[string]int](dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["n"] != 1 {
		t.Fatalf("want the one good file, got %v", got)
	}
}

func TestListJSONMissingDir(t *testing.T) {
	got, err := ListJSON[int](filepath.Join(t.TempDir(), "nope"))
	if err != nil || got != nil {
		t.Fatalf("missing dir should be empty, got %v %v", got, err)
	}
}

func TestWithLockMutualExclusion(t *testing.T) {
	s := At(t.TempDir(), t.TempDir())
	var inside, max int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.WithLock(func() error {
				mu.Lock()
				inside++
				if inside > max {
					max = inside
				}
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithLock: %v", err)
			}
		}()
	}
	wg.Wait()
	if max != 1 {
		t.Fatalf("critical section overlap: max concurrency %d", max)
	}
}

// A lock file left on disk by a crashed process holds no kernel lock, so
// it must not block anyone. This is the property that lets dibs skip
// stale-lock detection entirely.
func TestWithLockIgnoresLeftoverLockFile(t *testing.T) {
	s := At(t.TempDir(), t.TempDir())
	if err := os.MkdirAll(s.State, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(s.State, "lock")
	if err := os.WriteFile(lock, []byte("crashed writer left me behind"), 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- s.WithLock(func() error { return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("leftover lock file must not block: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("WithLock hung on a leftover lock file")
	}
}

func TestOpenOutsideRepo(t *testing.T) {
	dir := t.TempDir() // no .git here
	if _, err := Open(dir); err == nil {
		t.Fatal("Open outside a git repo must fail with guidance")
	}
}
