package root

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func mkRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Marker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFindAtRoot(t *testing.T) {
	dir := mkRoot(t)
	got, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("Find = %q, want %q", got, dir)
	}
}

func TestFindFromNestedDirectory(t *testing.T) {
	dir := mkRoot(t)
	nested := filepath.Join(dir, "claude", "agents", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Find(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("Find = %q, want %q", got, dir)
	}
}

func TestFindPicksNearestRoot(t *testing.T) {
	outer := mkRoot(t)
	inner := filepath.Join(outer, "inner")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, Marker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Find(inner)
	if err != nil {
		t.Fatal(err)
	}
	if got != inner {
		t.Errorf("Find = %q, want %q", got, inner)
	}
}

func TestFindOutsideRoot(t *testing.T) {
	_, err := Find(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Find err = %v, want ErrNotFound", err)
	}
}

func TestFindRejectsMarkerDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, Marker), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Find(dir)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Find err = %v, want a not-a-regular-file error", err)
	}
}

func TestInitCreatesMarker(t *testing.T) {
	dir := t.TempDir()
	created, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("Init created = false, want true")
	}
	info, err := os.Stat(filepath.Join(dir, Marker))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("marker size = %d, want 0", info.Size())
	}
}

func TestInitExistingRoot(t *testing.T) {
	dir := mkRoot(t)
	created, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("Init created = true on an existing root, want false")
	}
}
