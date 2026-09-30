package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkClaude(t *testing.T, e *env) (setDir, dest string) {
	t.Helper()
	setDir, dest = claudeSet(t, e)
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	return setDir, dest
}

func TestUnlinkReplacesLinksWithCopies(t *testing.T) {
	e := newEnv(t)
	_, dest := linkClaude(t, e)
	out, err := run(t, "unlink", "claude")
	if err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), "{}")
	assertRealFile(t, filepath.Join(dest, "agents", "reviewer.md"), "review")
	if !strings.Contains(out, "2 files copied") {
		t.Errorf("output = %q", out)
	}
}

func TestUnlinkLeavesSetUnchanged(t *testing.T) {
	e := newEnv(t)
	setDir, _ := linkClaude(t, e)
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(setDir, "settings.json"), "{}")
	assertRealFile(t, filepath.Join(setDir, "agents", "reviewer.md"), "review")
	assertRealFile(t, filepath.Join(setDir, "projects", "p", "log.json"), "runtime")
}

func TestUnlinkCopiesAreIndependent(t *testing.T) {
	e := newEnv(t)
	setDir, dest := linkClaude(t, e)
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(dest, "settings.json"), "changed in dest")
	assertRealFile(t, filepath.Join(setDir, "settings.json"), "{}")
}

func TestUnlinkRemovesSetFromState(t *testing.T) {
	e := newEnv(t)
	linkClaude(t, e)
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	st := e.state(t)
	if _, ok := st.Linked["claude"]; ok {
		t.Error("claude still in state")
	}
	if st.Root != "" {
		t.Errorf("root = %q, want cleared once no sets are linked", st.Root)
	}
}

func TestUnlinkKeepsOtherSets(t *testing.T) {
	e := newEnv(t)
	linkClaude(t, e)
	e.set(t, "nvim", "dest: "+filepath.Join(e.base, "home", ".config", "nvim")+"\n", map[string]string{"init.lua": "x"})
	if out, err := run(t, "link", "nvim"); err != nil {
		t.Fatalf("link nvim: %v\n%s", err, out)
	}
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	st := e.state(t)
	if st.Linked["nvim"] == nil || st.Root != e.root {
		t.Errorf("state after unlinking claude = %+v", st)
	}
}

func TestUnlinkLeavesUnmanagedPaths(t *testing.T) {
	e := newEnv(t)
	setDir, dest := linkClaude(t, e)
	// A real file fibre never linked, a link the user made by hand, and a
	// manifest path the user replaced with a real file.
	writeFile(t, filepath.Join(dest, "history.jsonl"), "h")
	writeFile(t, filepath.Join(setDir, "extra.md"), "extra")
	if err := os.Symlink(filepath.Join(setDir, "extra.md"), filepath.Join(dest, "extra.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dest, "settings.json")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dest, "settings.json"), "user's own")

	out, err := run(t, "unlink", "claude")
	if err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "history.jsonl"), "h")
	assertSymlink(t, filepath.Join(dest, "extra.md"), filepath.Join(setDir, "extra.md"))
	assertRealFile(t, filepath.Join(dest, "settings.json"), "user's own")
	if !strings.Contains(out, "SKIPPED  settings.json") {
		t.Errorf("output does not report the replaced path:\n%s", out)
	}
}

func TestUnlinkRemovesLinksToDeletedSetFiles(t *testing.T) {
	e := newEnv(t)
	setDir, dest := linkClaude(t, e)
	if err := os.Remove(filepath.Join(setDir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "unlink", "claude")
	if err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	assertMissing(t, filepath.Join(dest, "settings.json"))
	assertRealFile(t, filepath.Join(dest, "agents", "reviewer.md"), "review")
	if !strings.Contains(out, "1 link to deleted set files removed") {
		t.Errorf("output = %q", out)
	}
}

func TestUnlinkWorksAfterDestChangeInConfig(t *testing.T) {
	e := newEnv(t)
	setDir, dest := linkClaude(t, e)
	writeFile(t, filepath.Join(setDir, "fibre.yaml"), "dest: "+filepath.Join(e.base, "elsewhere")+"\n")
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), "{}")
}

func TestUnlinkThenLinkAgain(t *testing.T) {
	e := newEnv(t)
	setDir, dest := linkClaude(t, e)
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	// The copies are identical to the set, so relinking replaces them.
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link again: %v\n%s", err, out)
	}
	assertSymlink(t, filepath.Join(dest, "settings.json"), filepath.Join(setDir, "settings.json"))
}

func TestUnlinkNotLinked(t *testing.T) {
	e := newEnv(t)
	claudeSet(t, e)
	if _, err := run(t, "unlink", "claude"); err == nil || !strings.Contains(err.Error(), "not linked") {
		t.Errorf("err = %v", err)
	}
}

func TestUnlinkInvalidSetName(t *testing.T) {
	newEnv(t)
	if _, err := run(t, "unlink", "../x"); err == nil || !strings.Contains(err.Error(), "invalid set name") {
		t.Errorf("err = %v", err)
	}
}
