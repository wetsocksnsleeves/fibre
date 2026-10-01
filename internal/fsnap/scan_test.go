package fsnap

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "a")
	write(t, filepath.Join(dir, "sub", "b.txt"), "b")
	write(t, filepath.Join(dir, "skipped", "c.txt"), "c")
	if err := os.Symlink("/target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	tree, err := Scan(dir, func(rel string) bool { return rel == "skipped" })
	if err != nil {
		t.Fatal(err)
	}
	want := New().File("a.txt", "a").File("sub/b.txt", "b").Symlink("link", "/target").Tree()
	if len(tree) != len(want) {
		t.Errorf("Scan found %d paths, want %d: %v", len(tree), len(want), tree)
	}
	for rel, e := range want {
		if tree[rel] != e {
			t.Errorf("%s: got %+v, want %+v", rel, tree[rel], e)
		}
	}
}

func TestLookup(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "file"), "x")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	tree, err := Lookup(dir, []string{"file", "sub", "link", "missing", "file/below"})
	if err != nil {
		t.Fatal(err)
	}
	want := New().File("file", "x").Dir("sub").Symlink("link", "/target").Tree()
	if len(tree) != len(want) {
		t.Errorf("Lookup found %d paths, want %d: %v", len(tree), len(want), tree)
	}
	for rel, e := range want {
		if tree[rel] != e {
			t.Errorf("%s: got %+v, want %+v", rel, tree[rel], e)
		}
	}
}

func TestLookupMissingDir(t *testing.T) {
	tree, err := Lookup(filepath.Join(t.TempDir(), "nope"), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 0 {
		t.Errorf("Lookup = %v, want empty", tree)
	}
}
