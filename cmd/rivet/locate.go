package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wetsocksnsleeves/rivet/internal/root"
	"github.com/wetsocksnsleeves/rivet/internal/state"
)

// locate finds the root for commands that work from a linked dest as well
// as from inside the root (status and unlink). Inside a root, that root is
// used. Otherwise, if the working directory is inside a linked set's dest,
// the root recorded in state is used.
//
// inSet names the set whose dest is the deepest one containing the working
// directory. It is empty inside the root, on a tie, and when that dest is
// the home directory or contains it, since nearly every directory is inside
// such a dest.
func locate(st *state.State) (rootDir, inSet string, err error) {
	rootDir, err = findRoot()
	if err == nil || !errors.Is(err, root.ErrNotFound) || st.Root == "" {
		return rootDir, "", err
	}
	notFound := err

	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	wds, homes := pathForms(wd), pathForms(home)

	inDest := false
	deepest := -1
	var names []string
	for name, s := range st.Linked {
		dests := pathForms(s.Dest)
		if !anyWithin(wds, dests) {
			continue
		}
		inDest = true
		switch {
		case anyWithin(homes, dests):
		case len(s.Dest) > deepest:
			deepest, names = len(s.Dest), []string{name}
		case len(s.Dest) == deepest:
			names = append(names, name)
		}
	}
	if !inDest {
		return "", "", notFound
	}
	if len(names) == 1 {
		inSet = names[0]
	}
	return st.Root, inSet, nil
}

// pathForms returns p and, if different, p with symlinks resolved, so a
// directory reached through a symlinked path still matches.
func pathForms(p string) []string {
	forms := []string{p}
	if r, err := filepath.EvalSymlinks(p); err == nil && r != p {
		forms = append(forms, r)
	}
	return forms
}

func anyWithin(ps, dirs []string) bool {
	for _, p := range ps {
		for _, d := range dirs {
			if within(p, d) {
				return true
			}
		}
	}
	return false
}
