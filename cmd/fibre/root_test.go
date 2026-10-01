package main

import (
	"bytes"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd("1.2.3")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestVersion(t *testing.T) {
	out, err := run(t, "--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if want := "fibre version 1.2.3\n"; out != want {
		t.Errorf("--version output = %q, want %q", out, want)
	}
}

func TestUnknownFlagFails(t *testing.T) {
	if _, err := run(t, "--no-such-flag"); err == nil {
		t.Error("unknown flag: got nil error")
	}
}
