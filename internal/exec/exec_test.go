package exec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
	"github.com/wetsocksnsleeves/rivet/internal/plan"
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

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertLink(t *testing.T, p, target string) {
	t.Helper()
	got, err := os.Readlink(p)
	if err != nil {
		t.Fatalf("%s is not a symlink: %v", p, err)
	}
	if got != target {
		t.Errorf("%s -> %s, want -> %s", p, got, target)
	}
}

func dirs(t *testing.T) (setDir, dest string) {
	t.Helper()
	base := t.TempDir()
	return filepath.Join(base, "set"), filepath.Join(base, "dest")
}

func TestApplyMkDirAndLink(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "sub", "a"), "a")
	_, err := Apply(setDir, dest, []plan.Action{
		{Op: plan.OpMkDir, Path: "."},
		{Op: plan.OpMkDir, Path: "sub"},
		{Op: plan.OpLink, Path: "sub/a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(dest, "sub")); err != nil || !info.IsDir() {
		t.Errorf("dest/sub is not a real directory: %v", err)
	}
	assertLink(t, filepath.Join(dest, "sub", "a"), filepath.Join(setDir, "sub", "a"))
}

func TestApplyReplaceWithLink(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "a"), "same")
	write(t, filepath.Join(dest, "a"), "same")
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpReplaceWithLink, Path: "a"}}); err != nil {
		t.Fatal(err)
	}
	assertLink(t, filepath.Join(dest, "a"), filepath.Join(setDir, "a"))
	if _, err := os.Lstat(filepath.Join(dest, "a.rivet-tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp link left behind: %v", err)
	}
}

func TestApplyAdopt(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "a"), "set version")
	write(t, filepath.Join(dest, "a"), "dest version")
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpAdopt, Path: "a"}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(setDir, "a")); got != "dest version" {
		t.Errorf("set file = %q, want the adopted dest version", got)
	}
	assertLink(t, filepath.Join(dest, "a"), filepath.Join(setDir, "a"))
}

func TestApplyAdoptCreatesSetParents(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(dest, "a", "b", "c"), "c")
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpAdopt, Path: "a/b/c"}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(setDir, "a", "b", "c")); got != "c" {
		t.Errorf("set file = %q", got)
	}
	assertLink(t, filepath.Join(dest, "a", "b", "c"), filepath.Join(setDir, "a", "b", "c"))
}

func TestApplyReplaceWithCopy(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "bin", "tool"), "#!/bin/sh")
	if err := os.Chmod(filepath.Join(setDir, "bin", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dest, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(setDir, "bin", "tool"), filepath.Join(dest, "bin", "tool")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpReplaceWithCopy, Path: "bin/tool"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("dest/bin/tool is a %v, want a regular file", info.Mode().Type())
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	if got := read(t, filepath.Join(dest, "bin", "tool")); got != "#!/bin/sh" {
		t.Errorf("copy = %q", got)
	}
	if got := read(t, filepath.Join(setDir, "bin", "tool")); got != "#!/bin/sh" {
		t.Errorf("set file changed: %q", got)
	}
}

func TestApplyReplaceWithCopyOfSetSymlink(t *testing.T) {
	setDir, dest := dirs(t)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/usr/bin/env", filepath.Join(setDir, "env")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(setDir, "env"), filepath.Join(dest, "env")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpReplaceWithCopy, Path: "env"}}); err != nil {
		t.Fatal(err)
	}
	assertLink(t, filepath.Join(dest, "env"), "/usr/bin/env")
}

func TestApplyRemoveLink(t *testing.T) {
	setDir, dest := dirs(t)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(setDir, "gone"), filepath.Join(dest, "gone")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpRemoveLink, Path: "gone"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("link not removed: %v", err)
	}
}

func TestApplyDeleteSetFile(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "a"), "a")
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpDeleteSetFile, Path: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(setDir, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("set file not deleted: %v", err)
	}
}

func TestApplyBackupThenLink(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "a"), "set")
	write(t, filepath.Join(dest, "a"), "dest")
	res, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpBackup, Path: "a"}, {Op: plan.OpLink, Path: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	bak := filepath.Join(dest, "a.rivet-bak")
	if got := read(t, bak); got != "dest" {
		t.Errorf("backup = %q, want %q", got, "dest")
	}
	if len(res.Backups) != 1 || res.Backups[0] != (Backup{Path: "a", To: bak}) {
		t.Errorf("Backups = %+v", res.Backups)
	}
	assertLink(t, filepath.Join(dest, "a"), filepath.Join(setDir, "a"))
}

func TestApplyBackupPicksFreeName(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(dest, "a"), "new")
	write(t, filepath.Join(dest, "a.rivet-bak"), "older")
	write(t, filepath.Join(dest, "a.rivet-bak.1"), "oldest")
	res, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpBackup, Path: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dest, "a.rivet-bak.2")
	if len(res.Backups) != 1 || res.Backups[0].To != want {
		t.Fatalf("Backups = %+v, want one to %s", res.Backups, want)
	}
	if got := read(t, want); got != "new" {
		t.Errorf("backup = %q", got)
	}
	if got := read(t, filepath.Join(dest, "a.rivet-bak")); got != "older" {
		t.Errorf("existing backup overwritten: %q", got)
	}
}

func TestApplyBackupDirectory(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(dest, "d", "inner"), "x")
	if _, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpBackup, Path: "d"}, {Op: plan.OpMkDir, Path: "d"}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dest, "d.rivet-bak", "inner")); got != "x" {
		t.Errorf("backed-up directory contents = %q", got)
	}
	entries, err := os.ReadDir(filepath.Join(dest, "d"))
	if err != nil || len(entries) != 0 {
		t.Errorf("new dest/d = %v, %v; want an empty directory", entries, err)
	}
}

func TestApplyRefusesConflictsWithoutChanges(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(setDir, "a"), "a")
	actions := []plan.Action{
		{Op: plan.OpMkDir, Path: "."},
		{Op: plan.OpLink, Path: "a"},
		{Op: plan.OpConflict, Path: "b", Found: fsnap.Entry{Kind: fsnap.File}, Want: fsnap.File},
	}
	_, err := Apply(setDir, dest, actions)
	var ce *ConflictError
	if !errors.As(err, &ce) || len(ce.Conflicts) != 1 || ce.Conflicts[0].Path != "b" {
		t.Fatalf("err = %v, want a ConflictError for b", err)
	}
	if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dest was created despite conflicts: %v", err)
	}
}

func TestApplyStopsAtFirstFailure(t *testing.T) {
	setDir, dest := dirs(t)
	write(t, filepath.Join(dest, "a"), "in the way")
	_, err := Apply(setDir, dest, []plan.Action{{Op: plan.OpLink, Path: "a"}, {Op: plan.OpLink, Path: "b"}})
	if err == nil {
		t.Fatal("Apply succeeded linking over an existing file")
	}
	if _, err := os.Lstat(filepath.Join(dest, "b")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Apply continued after a failure: %v", err)
	}
}
