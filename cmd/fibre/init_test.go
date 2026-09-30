package main

import (
	"os"
	"path/filepath"
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

func TestInitRejectsArgs(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := run(t, "init", "claude"); err == nil {
		t.Error("init with a set name: got nil error")
	}
}
