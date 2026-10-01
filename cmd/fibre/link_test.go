package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wetsocksnsleeves/fibre/internal/root"
	"github.com/wetsocksnsleeves/fibre/internal/state"
)

// env is a temp dotfiles root, a temp state dir, and a base directory for
// dests. The working directory is the root.
type env struct {
	root, stateDir, base string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{root: filepath.Join(base, "dotfiles"), stateDir: filepath.Join(base, "state", "fibre"), base: base}
	writeFile(t, filepath.Join(e.root, root.Marker), "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Chdir(e.root)
	return e
}

// set creates a set with the given fibre.yaml and files, and returns its dir.
func (e *env) set(t *testing.T, name, yaml string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(e.root, name)
	writeFile(t, filepath.Join(dir, "fibre.yaml"), yaml)
	for rel, contents := range files {
		writeFile(t, filepath.Join(dir, rel), contents)
	}
	return dir
}

func (e *env) state(t *testing.T) *state.State {
	t.Helper()
	st, err := state.Load(e.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func writeFile(t *testing.T, p, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertSymlink(t *testing.T, p, target string) {
	t.Helper()
	got, err := os.Readlink(p)
	if err != nil {
		t.Errorf("%s is not a symlink: %v", p, err)
		return
	}
	if got != target {
		t.Errorf("%s -> %s, want -> %s", p, got, target)
	}
}

func assertRealFile(t *testing.T, p, contents string) {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Errorf("%s: %v", p, err)
		return
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s is a %v, want a regular file", p, info.Mode().Type())
		return
	}
	if got := readFile(t, p); got != contents {
		t.Errorf("%s = %q, want %q", p, got, contents)
	}
}

func assertMissing(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists (err %v), want missing", p, err)
	}
}

func claudeSet(t *testing.T, e *env) (setDir, dest string) {
	t.Helper()
	dest = filepath.Join(e.base, "home", ".claude")
	setDir = e.set(t, "claude", "dest: "+dest+"\nexclude:\n  - projects/**\n", map[string]string{
		"settings.json":       "{}",
		"agents/reviewer.md":  "review",
		"projects/p/log.json": "runtime",
	})
	return setDir, dest
}

func TestLinkIntoMissingDest(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", filepath.Join(e.base, "home"))
	setDir, dest := claudeSet(t, e)

	out, err := run(t, "link", "claude")
	if err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	assertSymlink(t, filepath.Join(dest, "settings.json"), filepath.Join(setDir, "settings.json"))
	assertSymlink(t, filepath.Join(dest, "agents", "reviewer.md"), filepath.Join(setDir, "agents", "reviewer.md"))
	if info, err := os.Lstat(filepath.Join(dest, "agents")); err != nil || !info.IsDir() {
		t.Errorf("dest/agents should be a real directory: %v", err)
	}
	assertMissing(t, filepath.Join(dest, "projects"))
	assertMissing(t, filepath.Join(dest, "fibre.yaml"))
	if want := "linked claude into ~/.claude: 2 new (2 links)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLinkRecordsState(t *testing.T) {
	e := newEnv(t)
	_, dest := claudeSet(t, e)
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	st := e.state(t)
	if st.Root != e.root {
		t.Errorf("state root = %q, want %q", st.Root, e.root)
	}
	s := st.Linked["claude"]
	if s == nil {
		t.Fatal("claude not in state")
	}
	if s.Dest != dest {
		t.Errorf("state dest = %q, want %q", s.Dest, dest)
	}
	if s.LinkedAt.IsZero() {
		t.Error("linked_at not set")
	}
	if want := []string{"agents/reviewer.md", "settings.json"}; !slices.Equal(s.Links, want) {
		t.Errorf("manifest = %q, want %q", s.Links, want)
	}
}

func TestLinkTwiceIsNoOp(t *testing.T) {
	e := newEnv(t)
	claudeSet(t, e)
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("first link: %v\n%s", err, out)
	}
	first := e.state(t).Linked["claude"]

	out, err := run(t, "link", "claude")
	if err != nil {
		t.Fatalf("second link: %v\n%s", err, out)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("output = %q, want up to date", out)
	}
	second := e.state(t).Linked["claude"]
	if !second.LinkedAt.Equal(first.LinkedAt) || !slices.Equal(second.Links, first.Links) {
		t.Errorf("state changed: %+v -> %+v", first, second)
	}
}

func TestLinkReplacesIdenticalFile(t *testing.T) {
	e := newEnv(t)
	setDir, dest := claudeSet(t, e)
	writeFile(t, filepath.Join(dest, "settings.json"), "{}")
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	assertSymlink(t, filepath.Join(dest, "settings.json"), filepath.Join(setDir, "settings.json"))
}

func TestLinkLeavesUnrelatedDestFiles(t *testing.T) {
	e := newEnv(t)
	_, dest := claudeSet(t, e)
	writeFile(t, filepath.Join(dest, "history.jsonl"), "h")
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "history.jsonl"), "h")
}

func TestLinkConflictHaltsWithoutChanges(t *testing.T) {
	e := newEnv(t)
	_, dest := claudeSet(t, e)
	writeFile(t, filepath.Join(dest, "settings.json"), `{"mine":1}`)

	out, err := run(t, "link", "claude")
	if err == nil {
		t.Fatal("link succeeded despite a conflict")
	}
	if !strings.Contains(out, "CONFLICT  settings.json") {
		t.Errorf("output does not list the conflict:\n%s", out)
	}
	if !strings.Contains(err.Error(), "1 conflict in claude") {
		t.Errorf("err = %v", err)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), `{"mine":1}`)
	assertMissing(t, filepath.Join(dest, "agents"))
	if _, ok := e.state(t).Linked["claude"]; ok {
		t.Error("claude recorded in state after a halted run")
	}
}

func TestLinkAdopt(t *testing.T) {
	e := newEnv(t)
	setDir, dest := claudeSet(t, e)
	writeFile(t, filepath.Join(dest, "settings.json"), `{"mine":1}`)
	if out, err := run(t, "link", "claude", "--adopt"); err != nil {
		t.Fatalf("link --adopt: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(setDir, "settings.json"), `{"mine":1}`)
	assertSymlink(t, filepath.Join(dest, "settings.json"), filepath.Join(setDir, "settings.json"))
}

func TestLinkForce(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", filepath.Join(e.base, "home"))
	setDir, dest := claudeSet(t, e)
	writeFile(t, filepath.Join(dest, "settings.json"), `{"mine":1}`)
	out, err := run(t, "link", "claude", "--force")
	if err != nil {
		t.Fatalf("link --force: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json.fibre-bak"), `{"mine":1}`)
	assertRealFile(t, filepath.Join(setDir, "settings.json"), "{}")
	assertSymlink(t, filepath.Join(dest, "settings.json"), filepath.Join(setDir, "settings.json"))
	if !strings.Contains(out, "backed up settings.json to ~/.claude/settings.json.fibre-bak") {
		t.Errorf("output does not report the backup:\n%s", out)
	}
}

func TestLinkSkip(t *testing.T) {
	e := newEnv(t)
	setDir, dest := claudeSet(t, e)
	writeFile(t, filepath.Join(dest, "settings.json"), `{"mine":1}`)
	if out, err := run(t, "link", "claude", "--skip"); err != nil {
		t.Fatalf("link --skip: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), `{"mine":1}`)
	assertSymlink(t, filepath.Join(dest, "agents", "reviewer.md"), filepath.Join(setDir, "agents", "reviewer.md"))
	if got := e.state(t).Linked["claude"].Links; !slices.Equal(got, []string{"agents/reviewer.md"}) {
		t.Errorf("manifest = %q, want only the linked path", got)
	}
}

func TestLinkResolutionFlagsAreExclusive(t *testing.T) {
	e := newEnv(t)
	claudeSet(t, e)
	if _, err := run(t, "link", "claude", "--adopt", "--force"); err == nil {
		t.Error("link --adopt --force succeeded")
	}
}

func TestLinkSetsSharingDest(t *testing.T) {
	e := newEnv(t)
	home := filepath.Join(e.base, "home")
	rofi := e.set(t, "rofi", "dest: "+home+"\n", map[string]string{".local/bin/rofi-run": "r"})
	tools := e.set(t, "tools", "dest: "+home+"\n", map[string]string{".local/bin/tool": "t"})
	for _, s := range []string{"rofi", "tools"} {
		if out, err := run(t, "link", s); err != nil {
			t.Fatalf("link %s: %v\n%s", s, err, out)
		}
	}
	assertSymlink(t, filepath.Join(home, ".local", "bin", "rofi-run"), filepath.Join(rofi, ".local", "bin", "rofi-run"))
	assertSymlink(t, filepath.Join(home, ".local", "bin", "tool"), filepath.Join(tools, ".local", "bin", "tool"))
}

func TestLinkSamePathInTwoSets(t *testing.T) {
	e := newEnv(t)
	home := filepath.Join(e.base, "home")
	rofi := e.set(t, "rofi", "dest: "+home+"\n", map[string]string{".local/bin/run": "r"})
	e.set(t, "tools", "dest: "+home+"\n", map[string]string{".local/bin/run": "t", ".local/bin/other": "o"})
	if out, err := run(t, "link", "rofi"); err != nil {
		t.Fatalf("link rofi: %v\n%s", err, out)
	}

	for _, flag := range []string{"", "--force"} {
		args := []string{"link", "tools"}
		if flag != "" {
			args = append(args, flag)
		}
		out, err := run(t, args...)
		if err == nil {
			t.Fatalf("%v succeeded despite sharing a path with rofi", args)
		}
		if !strings.Contains(out, ".local/bin/run  (linked by rofi)") {
			t.Errorf("%v output does not name the other set:\n%s", args, out)
		}
	}
	assertSymlink(t, filepath.Join(home, ".local", "bin", "run"), filepath.Join(rofi, ".local", "bin", "run"))
	assertMissing(t, filepath.Join(home, ".local", "bin", "other"))
}

func TestLinkRefusesChangedDest(t *testing.T) {
	e := newEnv(t)
	setDir, _ := claudeSet(t, e)
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(setDir, "fibre.yaml"), "dest: "+filepath.Join(e.base, "elsewhere")+"\n")
	if _, err := run(t, "link", "claude"); err == nil || !strings.Contains(err.Error(), "unlink it first") {
		t.Errorf("err = %v, want a changed-dest error", err)
	}
}

func TestLinkUnknownSet(t *testing.T) {
	newEnv(t)
	if _, err := run(t, "link", "nope"); err == nil || !strings.Contains(err.Error(), `no set "nope"`) {
		t.Errorf("err = %v", err)
	}
}

func TestLinkInvalidSetName(t *testing.T) {
	newEnv(t)
	for _, name := range []string{"a/b", "..", ".hidden"} {
		if _, err := run(t, "link", name); err == nil || !strings.Contains(err.Error(), "invalid set name") {
			t.Errorf("link %q: err = %v", name, err)
		}
	}
}

func TestLinkOutsideRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	if _, err := run(t, "link", "claude"); !errors.Is(err, root.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLinkFromNestedDirectory(t *testing.T) {
	e := newEnv(t)
	setDir, dest := claudeSet(t, e)
	t.Chdir(filepath.Join(setDir, "agents"))
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	assertSymlink(t, filepath.Join(dest, "settings.json"), filepath.Join(setDir, "settings.json"))
}

func TestLinkWhileLocked(t *testing.T) {
	e := newEnv(t)
	old := lockWait
	lockWait = 100 * time.Millisecond
	t.Cleanup(func() { lockWait = old })
	claudeSet(t, e)
	lock, err := state.TryAcquire(e.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := run(t, "link", "claude"); !errors.Is(err, state.ErrLocked) {
		t.Errorf("err = %v, want ErrLocked", err)
	}
}

func TestLinkShortFlags(t *testing.T) {
	for flag, check := range map[string]func(t *testing.T, setDir, dest string){
		"-a": func(t *testing.T, setDir, dest string) {
			assertRealFile(t, filepath.Join(setDir, "settings.json"), "mine")
		},
		"-f": func(t *testing.T, setDir, dest string) {
			assertRealFile(t, filepath.Join(dest, "settings.json.fibre-bak"), "mine")
		},
		"-s": func(t *testing.T, setDir, dest string) {
			assertRealFile(t, filepath.Join(dest, "settings.json"), "mine")
		},
	} {
		t.Run(flag, func(t *testing.T) {
			e := newEnv(t)
			setDir, dest := claudeSet(t, e)
			writeFile(t, filepath.Join(dest, "settings.json"), "mine")
			if out, err := run(t, "link", "claude", flag); err != nil {
				t.Fatalf("link %s: %v\n%s", flag, err, out)
			}
			check(t, setDir, dest)
		})
	}
}
