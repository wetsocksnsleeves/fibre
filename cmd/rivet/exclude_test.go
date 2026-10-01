package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExcludeInsideSet(t *testing.T) {
	e := newEnv(t)
	setDir, _ := claudeSet(t, e)
	t.Chdir(filepath.Join(setDir, "agents"))
	out, err := run(t, "exclude", "draft.md,old/,../history.jsonl")
	if err != nil {
		t.Fatalf("exclude: %v\n%s", err, out)
	}
	want := "dest: " + filepath.Join(e.base, "home", ".claude") +
		"\nexclude:\n  - projects/**\n  - agents/draft.md\n  - agents/old\n  - history.jsonl\n"
	if got := readFile(t, filepath.Join(setDir, "rivet.yaml")); got != want {
		t.Errorf("rivet.yaml =\n%s\nwant\n%s", got, want)
	}
	if want := "excluded agents/draft.md, agents/old, history.jsonl from claude\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestExcludeSpaceSeparatedAndRepeated(t *testing.T) {
	e := newEnv(t)
	setDir, _ := claudeSet(t, e)
	t.Chdir(setDir)
	out, err := run(t, "exclude", "a.json", "projects/**, b.json")
	if err != nil {
		t.Fatalf("exclude: %v\n%s", err, out)
	}
	if !strings.Contains(out, "projects/** is already excluded\n") || !strings.Contains(out, "excluded a.json, b.json from claude\n") {
		t.Errorf("output = %q", out)
	}
}

func TestExcludeReplacesLinksWithCopies(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HOME", filepath.Join(e.base, "home"))
	setDir, dest := linkClaude(t, e)
	t.Chdir(dest)
	out, err := run(t, "exclude", "settings.json")
	if err != nil {
		t.Fatalf("exclude: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), "{}")
	assertRealFile(t, filepath.Join(setDir, "settings.json"), "{}")
	assertSymlink(t, filepath.Join(dest, "agents", "reviewer.md"), filepath.Join(setDir, "agents", "reviewer.md"))
	if links := e.state(t).Linked["claude"].Links; !slices.Equal(links, []string{"agents/reviewer.md"}) {
		t.Errorf("manifest = %v", links)
	}
	if !strings.Contains(out, "replaced 1 link in ~/.claude with copies") {
		t.Errorf("output = %q", out)
	}

	// The excluded file no longer syncs: status calls the set in sync and
	// linking again leaves the copy alone.
	if out, err := run(t, "status"); err != nil || !strings.Contains(out, "synced") || strings.Contains(out, "not synced") {
		t.Errorf("status after exclude: %v\n%s", err, out)
	}
	t.Chdir(e.root)
	if out, err := run(t, "link", "claude"); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "settings.json"), "{}")
}

func TestExcludeDirectoryInsideDest(t *testing.T) {
	e := newEnv(t)
	setDir, dest := linkClaude(t, e)
	t.Chdir(dest)
	if out, err := run(t, "exclude", "agents"); err != nil {
		t.Fatalf("exclude: %v\n%s", err, out)
	}
	assertRealFile(t, filepath.Join(dest, "agents", "reviewer.md"), "review")
	if !strings.Contains(readFile(t, filepath.Join(setDir, "rivet.yaml")), "  - agents\n") {
		t.Error("rivet.yaml does not exclude agents")
	}
}

func TestExcludeWithSetFlag(t *testing.T) {
	e := newEnv(t)
	setDir, _ := claudeSet(t, e)
	if _, err := run(t, "exclude", "x.json"); err == nil || !strings.Contains(err.Error(), "pass --set") {
		t.Errorf("exclude at the root: err = %v", err)
	}
	if out, err := run(t, "exclude", "--set", "claude", "x.json"); err != nil {
		t.Fatalf("exclude --set: %v\n%s", err, out)
	}
	if !strings.Contains(readFile(t, filepath.Join(setDir, "rivet.yaml")), "  - x.json\n") {
		t.Error("rivet.yaml does not exclude x.json")
	}
	if _, err := run(t, "exclude", "--set", "nope", "x.json"); err == nil {
		t.Error("exclude --set nope succeeded")
	}
}

func TestExcludeRejectsPathsOutsideTheSet(t *testing.T) {
	e := newEnv(t)
	setDir, _ := claudeSet(t, e)
	t.Chdir(setDir)
	before := readFile(t, filepath.Join(setDir, "rivet.yaml"))
	for _, p := range []string{"../other", ".", "rivet.yaml", filepath.Join(e.base, "elsewhere"), "ok.json,[bad"} {
		if _, err := run(t, "exclude", p); err == nil {
			t.Errorf("exclude %q succeeded", p)
		}
	}
	if got := readFile(t, filepath.Join(setDir, "rivet.yaml")); got != before {
		t.Errorf("rivet.yaml changed:\n%s", got)
	}
}

func TestExcludeOutsideRootAndDests(t *testing.T) {
	e := newEnv(t)
	linkClaude(t, e)
	dir := filepath.Join(e.base, "elsewhere")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if _, err := run(t, "exclude", "x"); err == nil {
		t.Error("exclude outside the root and dests succeeded")
	}
}
