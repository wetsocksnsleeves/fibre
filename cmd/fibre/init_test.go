package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wetsocksnsleeves/fibre/internal/root"
)

func TestInitCreatesRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	out, err := run(t, "init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Initialized fibre root") {
		t.Errorf("output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, root.Marker)); err != nil {
		t.Errorf("marker not created: %v", err)
	}
}

func TestInitExistingRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := run(t, "init"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "already a fibre root") {
		t.Errorf("output = %q", out)
	}
}

func TestInitDestNeedsSetName(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := run(t, "init", "--dest", "/tmp/x"); err == nil || !strings.Contains(err.Error(), "needs a set name") {
		t.Errorf("err = %v", err)
	}
}

func TestInitSetRequiresDest(t *testing.T) {
	newEnv(t)
	if _, err := run(t, "init", "claude"); err == nil || !strings.Contains(err.Error(), "--dest is required") {
		t.Errorf("err = %v", err)
	}
}

func TestInitSetOutsideRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := run(t, "init", "claude", "--dest", "/tmp/x"); !errors.Is(err, root.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestInitSetWithMissingDest(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "elsewhere", ".claude")
	out, err := run(t, "init", "claude", "--dest", dest, "--exclude", "history.jsonl", "--exclude", "projects/**")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	want := "dest: " + dest + "\nexclude:\n    - history.jsonl\n    - projects/**\n"
	if got := readFile(t, filepath.Join(e.root, "claude", "fibre.yaml")); got != want {
		t.Errorf("fibre.yaml =\n%s\nwant\n%s", got, want)
	}
	assertMissing(t, dest)
	if _, ok := e.state(t).Linked["claude"]; ok {
		t.Error("set with no dest recorded in state")
	}
}

func TestInitSetWritesDestUnderHomeWithTilde(t *testing.T) {
	e := newEnv(t)
	home := filepath.Join(e.base, "home")
	t.Setenv("HOME", home)
	if out, err := run(t, "init", "claude", "--dest", filepath.Join(home, ".claude")); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(e.root, "claude", "fibre.yaml")); got != "dest: ~/.claude\n" {
		t.Errorf("fibre.yaml = %q", got)
	}
}

func TestInitSetKeepsUnexpandedDest(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", filepath.Join(e.base, "home"))
	if out, err := run(t, "init", "claude", "--dest", "$HOME/.claude"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(e.root, "claude", "fibre.yaml")); got != "dest: $HOME/.claude\n" {
		t.Errorf("fibre.yaml = %q", got)
	}
}

func TestInitSetMakesRelativeDestAbsolute(t *testing.T) {
	e := newEnv(t)
	if out, err := run(t, "init", "claude", "--dest", "../out"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	want := "dest: " + filepath.Join(e.base, "out") + "\n"
	if got := readFile(t, filepath.Join(e.root, "claude", "fibre.yaml")); got != want {
		t.Errorf("fibre.yaml = %q, want %q", got, want)
	}
}

func TestInitSetImportsDest(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "home", ".claude")
	writeFile(t, filepath.Join(dest, "settings.json"), "{}")
	writeFile(t, filepath.Join(dest, "agents", "reviewer.md"), "review")
	writeFile(t, filepath.Join(dest, "agents", "deep", "nested.md"), "nested")
	writeFile(t, filepath.Join(dest, "history.jsonl"), "history")
	writeFile(t, filepath.Join(dest, "projects", "p", "log.json"), "log")

	out, err := run(t, "init", "claude", "--dest", dest, "--exclude", "history.jsonl", "--exclude", "projects/**")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	setDir := filepath.Join(e.root, "claude")
	for rel, contents := range map[string]string{
		"settings.json":         "{}",
		"agents/reviewer.md":    "review",
		"agents/deep/nested.md": "nested",
	} {
		assertRealFile(t, filepath.Join(setDir, rel), contents)
		assertSymlink(t, filepath.Join(dest, rel), filepath.Join(setDir, rel))
	}
	if info, err := os.Lstat(filepath.Join(dest, "agents", "deep")); err != nil || !info.IsDir() {
		t.Errorf("dest/agents/deep should stay a real directory: %v", err)
	}
	if !strings.Contains(out, "imported 3 files") {
		t.Errorf("output = %q", out)
	}
}

func TestInitSetLeavesExcludedFilesInDest(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "home", ".claude")
	writeFile(t, filepath.Join(dest, "settings.json"), "{}")
	writeFile(t, filepath.Join(dest, "history.jsonl"), "history")
	writeFile(t, filepath.Join(dest, "projects", "p", "log.json"), "log")
	if out, err := run(t, "init", "claude", "--dest", dest, "--exclude", "history.jsonl", "--exclude", "projects/**"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	setDir := filepath.Join(e.root, "claude")
	assertRealFile(t, filepath.Join(dest, "history.jsonl"), "history")
	assertRealFile(t, filepath.Join(dest, "projects", "p", "log.json"), "log")
	assertMissing(t, filepath.Join(setDir, "history.jsonl"))
	assertMissing(t, filepath.Join(setDir, "projects"))
}

func TestInitSetRecordsState(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "home", ".claude")
	writeFile(t, filepath.Join(dest, "settings.json"), "{}")
	writeFile(t, filepath.Join(dest, "agents", "reviewer.md"), "review")
	if out, err := run(t, "init", "claude", "--dest", dest); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	s := e.state(t).Linked["claude"]
	if s == nil {
		t.Fatal("claude not in state")
	}
	if s.Dest != dest {
		t.Errorf("state dest = %q", s.Dest)
	}
	if want := []string{"agents/reviewer.md", "settings.json"}; !slices.Equal(s.Links, want) {
		t.Errorf("manifest = %q, want %q", s.Links, want)
	}
}

func TestInitSetThenLinkIsNoOp(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "home", ".claude")
	writeFile(t, filepath.Join(dest, "settings.json"), "{}")
	if out, err := run(t, "init", "claude", "--dest", dest); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	out, err := run(t, "link", "claude")
	if err != nil || !strings.Contains(out, "up to date") {
		t.Errorf("link after init: %v\n%s", err, out)
	}
}

func TestInitSetSkipsSymlinksInDest(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "home", ".claude")
	writeFile(t, filepath.Join(dest, "settings.json"), "{}")
	if err := os.Symlink("/stow/claude/.claude/agents", filepath.Join(dest, "agents")); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "init", "claude", "--dest", dest)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	assertSymlink(t, filepath.Join(dest, "agents"), "/stow/claude/.claude/agents")
	assertMissing(t, filepath.Join(e.root, "claude", "agents"))
	if !strings.Contains(out, "SKIPPED  agents  (symlink to /stow/claude/.claude/agents; left in dest)") {
		t.Errorf("output does not report the skipped symlink:\n%s", out)
	}
}

func TestInitSetNeverImportsRootOrState(t *testing.T) {
	// dest is e.base, which contains both the root and the state dir.
	e := newEnv(t)
	writeFile(t, filepath.Join(e.base, "notes.txt"), "n")
	if out, err := run(t, "init", "base", "--dest", e.base); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	setDir := filepath.Join(e.root, "base")
	assertSymlink(t, filepath.Join(e.base, "notes.txt"), filepath.Join(setDir, "notes.txt"))
	assertMissing(t, filepath.Join(setDir, "dotfiles"))
	assertMissing(t, filepath.Join(setDir, "state"))
	if got := e.state(t).Linked["base"].Links; !slices.Equal(got, []string{"notes.txt"}) {
		t.Errorf("manifest = %q", got)
	}
}

func TestInitSetRefusesExistingSet(t *testing.T) {
	e := newEnv(t)
	e.set(t, "claude", "dest: /somewhere\n", map[string]string{"a": "a"})
	dest := filepath.Join(e.base, "home", ".claude")
	writeFile(t, filepath.Join(dest, "b"), "b")
	if _, err := run(t, "init", "claude", "--dest", dest); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
	if got := readFile(t, filepath.Join(e.root, "claude", "fibre.yaml")); got != "dest: /somewhere\n" {
		t.Errorf("fibre.yaml overwritten: %q", got)
	}
	assertRealFile(t, filepath.Join(dest, "b"), "b")
}

func TestInitSetRefusesNonEmptyDirectory(t *testing.T) {
	e := newEnv(t)
	writeFile(t, filepath.Join(e.root, "claude", "stray"), "x")
	if _, err := run(t, "init", "claude", "--dest", filepath.Join(e.base, "d")); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("err = %v", err)
	}
}

func TestInitSetAllowsEmptyDirectory(t *testing.T) {
	e := newEnv(t)
	if err := os.Mkdir(filepath.Join(e.root, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "init", "claude", "--dest", filepath.Join(e.base, "d")); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

func TestInitSetInvalidExcludeChangesNothing(t *testing.T) {
	e := newEnv(t)
	if _, err := run(t, "init", "claude", "--dest", filepath.Join(e.base, "d"), "--exclude", "[a"); err == nil {
		t.Fatal("init succeeded with an invalid glob")
	}
	assertMissing(t, filepath.Join(e.root, "claude"))
}

func TestInitSetRefusesDestInsideRoot(t *testing.T) {
	e := newEnv(t)
	if _, err := run(t, "init", "claude", "--dest", filepath.Join(e.root, "x")); err == nil || !strings.Contains(err.Error(), "inside the dotfiles root") {
		t.Errorf("err = %v", err)
	}
	assertMissing(t, filepath.Join(e.root, "claude"))
}

func TestInitSetRefusesDestFile(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(e.base, "file")
	writeFile(t, dest, "x")
	if _, err := run(t, "init", "claude", "--dest", dest); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v", err)
	}
	assertMissing(t, filepath.Join(e.root, "claude"))
}

func TestInitSetRefusesHomeUnlessStrict(t *testing.T) {
	e := newEnv(t)
	home := filepath.Join(e.base, "home")
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".zshrc"), "z")

	if _, err := run(t, "init", "zsh", "--dest", home); err == nil || !strings.Contains(err.Error(), "--strict") {
		t.Errorf("err = %v, want a hint to use --strict", err)
	}
	assertMissing(t, filepath.Join(e.root, "zsh"))

	out, err := run(t, "init", "zsh", "--dest", home, "--strict")
	if err != nil {
		t.Fatalf("init --strict: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(e.root, "zsh", "fibre.yaml")); got != "dest: \"~\"\nstrict: true\n" {
		t.Errorf("fibre.yaml = %q", got)
	}
	assertRealFile(t, filepath.Join(home, ".zshrc"), "z")
	assertMissing(t, filepath.Join(e.root, "zsh", ".zshrc"))
}
