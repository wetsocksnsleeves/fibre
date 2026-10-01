package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// testdata is resolved at package init, before tests change directory.
var testdata, _ = filepath.Abs("testdata")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join(testdata, name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s:\n--- got\n%s--- want\n%s", p, got, want)
	}
}

// statusScenario links sets and then changes things behind rivet's back so
// that status has one line of every kind to show.
func statusScenario(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	home := filepath.Join(e.base, "home")
	t.Setenv("HOME", home)
	claude := filepath.Join(home, ".claude")

	claudeDir := e.set(t, "claude", "dest: ~/.claude\nexclude:\n  - projects/**\n", map[string]string{
		"settings.json":      "{}",
		"agents/reviewer.md": "review",
		"notes.md":           "notes",
		"keybindings.json":   "[]",
		"gone.md":            "gone",
	})
	e.set(t, "nvim", "dest: ~/.config/nvim\n", map[string]string{"init.lua": "vim.o.number = true"})
	e.set(t, "rofi", "dest: ~/.local\n", map[string]string{"bin/rofi-run": "r"})
	e.set(t, "tools", "dest: ~/.local\n", map[string]string{"bin/tool": "t"})
	e.set(t, "zsh", "dest: ~\nstrict: true\n", map[string]string{".zshrc.local": "z"})
	for _, s := range []string{"claude", "nvim", "rofi", "tools"} {
		if out, err := run(t, "link", s); err != nil {
			t.Fatalf("link %s: %v\n%s", s, err, out)
		}
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// DELETED: the user removed a link.
	must(os.Remove(filepath.Join(claude, "notes.md")))
	// MODIFIED: an atomic save replaced a link with a real file.
	must(os.Remove(filepath.Join(claude, "keybindings.json")))
	writeFile(t, filepath.Join(claude, "keybindings.json"), `["edited"]`)
	// DANGLING: a set file was deleted.
	must(os.Remove(filepath.Join(claudeDir, "gone.md")))
	// UNLINKED: a new set file, e.g. from git pull.
	writeFile(t, filepath.Join(claudeDir, "commands", "new.md"), "new")
	// CONFLICT: a new set file with a different file already in dest.
	writeFile(t, filepath.Join(claudeDir, "conflict.json"), "set")
	writeFile(t, filepath.Join(claude, "conflict.json"), "dest")
	// UNTRACKED: new real files in dest; one excluded, one a tie.
	writeFile(t, filepath.Join(claude, "agents", "draft.md"), "draft")
	writeFile(t, filepath.Join(claude, "projects", "p", "log.json"), "log")
	writeFile(t, filepath.Join(claude, "settings.json.rivet-bak"), "backup")
	writeFile(t, filepath.Join(home, ".local", "bin", "myscript"), "s")
	return e
}

func TestStatusGolden(t *testing.T) {
	statusScenario(t)
	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	golden(t, "status.golden", out)
}

func TestStatusVerboseGolden(t *testing.T) {
	statusScenario(t)
	out, err := run(t, "status", "-v")
	if err != nil {
		t.Fatalf("status -v: %v\n%s", err, out)
	}
	golden(t, "status-verbose.golden", out)
}

func TestStatusVeryVerboseGolden(t *testing.T) {
	statusScenario(t)
	out, err := run(t, "status", "-vv")
	if err != nil {
		t.Fatalf("status -vv: %v\n%s", err, out)
	}
	golden(t, "status-very-verbose.golden", out)
}

func TestStatusListsLinkedSetsFirst(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", filepath.Join(e.base, "home"))
	e.set(t, "alpha", "dest: ~/.alpha\n", map[string]string{"a": "a"})
	e.set(t, "beta", "dest: ~/.beta\n", map[string]string{"b": "b"})
	if out, err := run(t, "link", "beta"); err != nil {
		t.Fatalf("link beta: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"status"}, {"status", "-v"}} {
		out, err := run(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if a, b := strings.Index(out, "alpha"), strings.Index(out, "beta"); a < 0 || b < 0 || b > a {
			t.Errorf("%v does not list the linked set beta before alpha:\n%s", args, out)
		}
	}
}

func TestStatusOneSet(t *testing.T) {
	statusScenario(t)
	out, err := run(t, "status", "nvim")
	if err != nil {
		t.Fatalf("status nvim: %v\n%s", err, out)
	}
	want := "Linked:\n  nvim → ~/.config/nvim  synced\n\nWatcher is not running, so nothing is synced automatically. Start it with `rivet watch install` (or `rivet watch run` in the foreground).\n"
	if out != want {
		t.Errorf("status nvim = %q, want %q", out, want)
	}
}

func TestStatusOneSetStillSeesOtherSetsForOwnership(t *testing.T) {
	statusScenario(t)
	out, err := run(t, "status", "-v", "rofi")
	if err != nil {
		t.Fatalf("status -v rofi: %v\n%s", err, out)
	}
	if !strings.Contains(out, "    bin/myscript  (rofi, tools)") {
		t.Errorf("status rofi does not report the tie:\n%s", out)
	}
}

func TestStatusChangesNothing(t *testing.T) {
	e := statusScenario(t)
	claude := filepath.Join(e.base, "home", ".claude")
	if _, err := run(t, "status"); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, filepath.Join(claude, "notes.md"))
	assertMissing(t, filepath.Join(claude, "commands"))
	assertRealFile(t, filepath.Join(claude, "agents", "draft.md"), "draft")
	assertRealFile(t, filepath.Join(e.root, "claude", "notes.md"), "notes")
}

func TestStatusUnknownSet(t *testing.T) {
	newEnv(t)
	if _, err := run(t, "status", "nope"); err == nil || !strings.Contains(err.Error(), `no set "nope"`) {
		t.Errorf("err = %v", err)
	}
}

func TestStatusReportsBrokenConfig(t *testing.T) {
	e := newEnv(t)
	e.set(t, "bad", "dest: relative/path\n", nil)
	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "bad") || !strings.Contains(out, "must be an absolute path") {
		t.Errorf("status does not report the broken set:\n%s", out)
	}
}

func TestStatusReportsChangedDest(t *testing.T) {
	e := newEnv(t)
	setDir, _ := linkClaude(t, e)
	writeFile(t, filepath.Join(setDir, "rivet.yaml"), "dest: "+filepath.Join(e.base, "elsewhere")+"\n")
	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "unlink and link again") {
		t.Errorf("status does not report the changed dest:\n%s", out)
	}
}

func TestPaint(t *testing.T) {
	if got := paint(false, toneBad, "x"); got != "x" {
		t.Errorf("paint without color = %q", got)
	}
	if got := paint(true, toneBad, "x"); got != "\x1b[31mx\x1b[0m" {
		t.Errorf("paint with color = %q", got)
	}
}

func TestUseColorOffForNonTerminals(t *testing.T) {
	if useColor(&strings.Builder{}) {
		t.Error("useColor = true for a non-file writer")
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(os.Stdout) {
		t.Error("useColor = true with NO_COLOR set")
	}
}
