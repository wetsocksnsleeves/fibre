// Package workspace loads the sets linked on this machine and snapshots
// them for the planners. It is the read side of the commands and the
// watcher: filesystem in, plain values out.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
	"github.com/wetsocksnsleeves/fibre/internal/plan"
	"github.com/wetsocksnsleeves/fibre/internal/state"
)

// Set is a linked set with its config loaded. Err is set when the config
// cannot be loaded or no longer matches state; such a set is not synced.
type Set struct {
	Name, SetDir string
	Config       *config.Config
	Links        []string
	Err          error
}

// Ignored are directories never read as part of a dest, even when a dest
// contains them: the dotfiles root and fibre's state directory.
type Ignored struct {
	Root, StateDir string
}

// Dirs returns the ignored directories as a list.
func (ig Ignored) Dirs() []string {
	return []string{ig.Root, ig.StateDir}
}

// Load loads every set linked in st, sorted by name.
func Load(st *state.State, env config.Env) []Set {
	names := make([]string, 0, len(st.Linked))
	for name := range st.Linked {
		names = append(names, name)
	}
	sort.Strings(names)
	sets := make([]Set, 0, len(names))
	for _, name := range names {
		linked := st.Linked[name]
		s := Set{Name: name, SetDir: filepath.Join(st.Root, name), Links: linked.Links}
		cfg, err := config.Load(s.SetDir, env)
		switch {
		case err != nil:
			s.Err = err
		case cfg.Dest != linked.Dest:
			s.Err = fmt.Errorf("fibre.yaml now says dest %s but the set is linked into %s; unlink and link again", cfg.Dest, linked.Dest)
		default:
			s.Config = cfg
		}
		sets = append(sets, s)
	}
	return sets
}

// Bare is the set with empty snapshots. Reconcile needs every set to decide
// ownership, but only reads the dest, strictness and excludes of sets whose
// paths it is not planning.
func (s Set) Bare() plan.SetState {
	return plan.SetState{
		Name: s.Name, SetDir: s.SetDir, Dest: s.Config.Dest,
		Set: fsnap.Tree{}, DestTree: fsnap.Tree{}, Excluded: s.Config.Excluded, Strict: s.Config.Strict, Links: s.Links,
	}
}

// Snapshot reads the whole set and its dest. A strict set only needs its
// own paths in dest; any other set also needs every file in dest, to find
// untracked ones. Only paths the set maps to are hashed.
func (s Set) Snapshot(ig Ignored) (plan.SetState, error) {
	set, err := fsnap.Scan(s.SetDir, s.Config.Excluded)
	if err != nil {
		return plan.SetState{}, err
	}
	destTree := fsnap.Tree{}
	if !s.Config.Strict {
		if destTree, err = s.scanDest(".", ig); err != nil {
			return plan.SetState{}, err
		}
	}
	paths := keys(set)
	for _, l := range s.Links {
		if _, ok := set[l]; !ok {
			paths = append(paths, l)
		}
	}
	return s.finish(set, destTree, paths)
}

// SnapshotPaths reads only what Reconcile needs to plan rels (and anything
// below them): each path and its parents, the set and dest trees below it,
// and manifest paths below it.
func (s Set) SnapshotPaths(rels []string, ig Ignored) (plan.SetState, error) {
	set, destTree := fsnap.Tree{}, fsnap.Tree{}
	var paths []string
	for _, rel := range rels {
		for p := rel; p != "."; p = path.Dir(p) {
			paths = append(paths, p)
		}
		sub, err := scanBelow(s.SetDir, rel, s.Config.Excluded, fsnap.Scan)
		if err != nil {
			return plan.SetState{}, err
		}
		for k, v := range sub {
			set[k] = v
		}
		if !s.Config.Strict {
			sub, err := s.scanDest(rel, ig)
			if err != nil {
				return plan.SetState{}, err
			}
			for k, v := range sub {
				destTree[k] = v
			}
		}
		for _, l := range s.Links {
			if rel == "." || l == rel || strings.HasPrefix(l, rel+"/") {
				paths = append(paths, l)
			}
		}
	}
	// The scans found what is below each path; Lookup adds the paths
	// themselves and their parents on the set side, hashed.
	paths = append(paths, keys(set)...)
	setHashed, err := fsnap.Lookup(s.SetDir, paths)
	if err != nil {
		return plan.SetState{}, err
	}
	return s.finish(setHashed, destTree, paths)
}

// finish adds hashed dest entries for paths to destTree.
func (s Set) finish(set, destTree fsnap.Tree, paths []string) (plan.SetState, error) {
	hashed, err := fsnap.Lookup(s.Config.Dest, paths)
	if err != nil {
		return plan.SetState{}, err
	}
	for rel, e := range hashed {
		destTree[rel] = e
	}
	return plan.SetState{
		Name: s.Name, SetDir: s.SetDir, Dest: s.Config.Dest,
		Set: set, DestTree: destTree, Excluded: s.Config.Excluded, Strict: s.Config.Strict, Links: s.Links,
	}, nil
}

// scanDest lists everything in dest at and below rel without hashing,
// skipping excluded and ignored paths.
func (s Set) scanDest(rel string, ig Ignored) (fsnap.Tree, error) {
	dest := s.Config.Dest
	skip := func(r string) bool {
		p := filepath.Join(dest, filepath.FromSlash(r))
		return s.Config.Excluded(r) || p == ig.Root || p == ig.StateDir
	}
	return scanBelow(dest, rel, skip, fsnap.ScanKinds)
}

// scanBelow scans base/rel with scan and returns its entries keyed relative
// to base. A missing or non-directory base/rel yields nothing.
func scanBelow(base, rel string, skip func(string) bool, scan func(string, func(string) bool) (fsnap.Tree, error)) (fsnap.Tree, error) {
	dir := filepath.Join(base, filepath.FromSlash(rel))
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return fsnap.Tree{}, nil
	}
	if err != nil {
		return nil, err
	}
	join := func(r string) string {
		if rel == "." {
			return r
		}
		if r == "." {
			return rel
		}
		return rel + "/" + r
	}
	sub, err := scan(dir, func(r string) bool { return skip(join(r)) })
	if err != nil {
		return nil, err
	}
	out := fsnap.Tree{}
	for r, e := range sub {
		out[join(r)] = e
	}
	return out, nil
}

func keys(t fsnap.Tree) []string {
	out := make([]string, 0, len(t))
	for k := range t {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
