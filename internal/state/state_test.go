package state

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestLoadMissing(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != "" || len(s.Linked) != 0 {
		t.Errorf("Load = %+v, want empty", s)
	}
	if s.Linked == nil {
		t.Error("Linked is nil; callers should be able to assign into it")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fibre") // Save creates it
	at := time.Date(2026, 9, 30, 10, 12, 0, 0, time.UTC)
	in := &State{
		Root: "/Users/me/.dotfiles",
		Linked: map[string]*Set{
			"claude": {Dest: "/Users/me/.claude", LinkedAt: at, Links: []string{"agents/reviewer.md", "settings.json"}},
		},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := out.Linked["claude"]
	if out.Root != in.Root || got == nil || got.Dest != "/Users/me/.claude" || !got.LinkedAt.Equal(at) ||
		!slices.Equal(got.Links, []string{"agents/reviewer.md", "settings.json"}) {
		t.Errorf("round trip: got %+v / %+v", out, got)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		if err := Save(dir, &State{Root: "/r"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.yaml" {
		t.Errorf("dir contents = %v, want only state.yaml", entries)
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.yaml"), []byte("linked: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("Load succeeded on invalid YAML")
	}
}

func TestDefaultDirHonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/xdg/state/fibre" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func TestDefaultDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/Users/me")
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/Users/me/.local/state/fibre" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	l, err := TryAcquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryAcquire(dir); !errors.Is(err, ErrLocked) {
		t.Errorf("second TryAcquire err = %v, want ErrLocked", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := TryAcquire(dir)
	if err != nil {
		t.Fatalf("TryAcquire after Release: %v", err)
	}
	l2.Release()
}

func TestAcquireWaitsForRelease(t *testing.T) {
	dir := t.TempDir()
	l, err := TryAcquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		l.Release()
	}()
	l2, err := Acquire(dir, 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	l2.Release()
}

func TestAcquireGivesUp(t *testing.T) {
	dir := t.TempDir()
	l, err := TryAcquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := Acquire(dir, 100*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Errorf("err = %v, want ErrLocked", err)
	}
}

func TestWatcherLock(t *testing.T) {
	dir := t.TempDir()
	if _, running, err := WatcherPID(dir); err != nil || running {
		t.Fatalf("WatcherPID before start = %v, %v", running, err)
	}
	l, err := AcquireWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	pid, running, err := WatcherPID(dir)
	if err != nil || !running || pid != os.Getpid() {
		t.Errorf("WatcherPID = %d, %v, %v; want %d, true", pid, running, err, os.Getpid())
	}
	if _, err := AcquireWatcher(dir); !errors.Is(err, ErrWatcherRunning) {
		t.Errorf("second AcquireWatcher err = %v", err)
	}
	l.Release()
	if _, running, _ := WatcherPID(dir); running {
		t.Error("WatcherPID reports running after release")
	}
}
