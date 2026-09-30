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
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrLocked) {
		t.Errorf("second Acquire err = %v, want ErrLocked", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	l2.Release()
}
