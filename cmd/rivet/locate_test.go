package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/root"
)

func TestStatusInsideDestShowsThatSet(t *testing.T) {
	e := statusScenario(t)
	for _, dir := range []string{
		filepath.Join(e.base, "home", ".config", "nvim"),
		filepath.Join(e.base, "home", ".claude", "agents"),
	} {
		t.Chdir(dir)
		out, err := run(t, "status")
		if err != nil {
			t.Fatalf("status in %s: %v\n%s", dir, err, out)
		}
		wantSet := filepath.Base(dir)
		if wantSet == "agents" {
			wantSet = "claude"
		}
		if !strings.HasPrefix(out, "Linked:\n  "+wantSet+" → ") {
			t.Errorf("status in %s = %q, want the %s set first", dir, out, wantSet)
		}
		if n := strings.Count(out, " → "); n != 1 {
			t.Errorf("status in %s shows %d sets, want 1:\n%s", dir, n, out)
		}
	}
}

func TestStatusInsideDestAll(t *testing.T) {
	e := statusScenario(t)
	t.Chdir(filepath.Join(e.base, "home", ".config", "nvim"))
	out, err := run(t, "status", "--all")
	if err != nil {
		t.Fatalf("status --all: %v\n%s", err, out)
	}
	golden(t, "status.golden", out)
}

func TestStatusInsideDestNamedSet(t *testing.T) {
	e := statusScenario(t)
	t.Chdir(filepath.Join(e.base, "home", ".config", "nvim"))
	out, err := run(t, "status", "rofi")
	if err != nil {
		t.Fatalf("status rofi: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "Linked:\n  rofi → ") {
		t.Errorf("status rofi = %q", out)
	}
}

func TestStatusAllWithSetName(t *testing.T) {
	statusScenario(t)
	if _, err := run(t, "status", "--all", "nvim"); err == nil {
		t.Error("status --all nvim succeeded")
	}
}

func TestStatusInsideTiedDestShowsAll(t *testing.T) {
	// rofi and tools share ~/.local.
	e := statusScenario(t)
	t.Chdir(filepath.Join(e.base, "home", ".local", "bin"))
	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	golden(t, "status.golden", out)
}

func TestStatusUnderHomeDestShowsAll(t *testing.T) {
	// A set linked at ~ finds the root from anywhere under home, but does
	// not narrow status to itself.
	e := newEnv(t)
	home := filepath.Join(e.base, "home")
	t.Setenv("HOME", home)
	e.set(t, "zsh", "dest: ~\nstrict: true\n", map[string]string{".zshrc.local": "z"})
	e.set(t, "nvim", "dest: ~/.config/nvim\n", map[string]string{"init.lua": "x"})
	for _, s := range []string{"zsh", "nvim"} {
		if out, err := run(t, "link", s); err != nil {
			t.Fatalf("link %s: %v\n%s", s, err, out)
		}
	}
	dir := filepath.Join(home, "code", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "  nvim → ") || !strings.Contains(out, "  zsh  → ") {
		t.Errorf("status under home should show every set:\n%s", out)
	}
}

func TestStatusOutsideRootAndDests(t *testing.T) {
	e := statusScenario(t)
	t.Chdir(e.base)
	if _, err := run(t, "status"); !errors.Is(err, root.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUnlinkInsideDest(t *testing.T) {
	e := newEnv(t)
	_, dest := linkClaude(t, e)
	t.Chdir(filepath.Join(dest, "agents"))
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), "{}")
}

func TestLinkInsideDestStillNeedsRoot(t *testing.T) {
	e := newEnv(t)
	_, dest := linkClaude(t, e)
	t.Chdir(dest)
	if _, err := run(t, "link", "claude"); !errors.Is(err, root.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestStatusInsideDestAfterLastUnlink(t *testing.T) {
	e := newEnv(t)
	_, dest := linkClaude(t, e)
	if out, err := run(t, "unlink", "claude"); err != nil {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	t.Chdir(dest)
	if _, err := run(t, "status"); !errors.Is(err, root.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound once nothing is linked", err)
	}
}
