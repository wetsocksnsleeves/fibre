// Package plan computes the actions fibre takes from snapshots. Planners do
// no I/O; an executor applies their output.
package plan

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
)

// Op is the kind of an Action.
type Op int

const (
	// OpMkDir creates a real directory in dest.
	OpMkDir Op = iota + 1
	// OpLink creates a symlink in dest to the set's file. Nothing is at Path.
	OpLink
	// OpReplaceWithLink replaces a real file in dest, identical to the set's
	// file, with a symlink to it.
	OpReplaceWithLink
	// OpAdopt moves the real file in dest into the set, overwriting the set's
	// file, then links it.
	OpAdopt
	// OpBackup moves whatever is at Path in dest aside so Path is free. The
	// executor picks the backup name.
	OpBackup
	// OpConflict means Path cannot be linked without losing what is in dest.
	OpConflict
	// OpReplaceWithCopy replaces a symlink in dest with a copy of the set
	// file it points to.
	OpReplaceWithCopy
	// OpRemoveLink removes a symlink in dest whose set file is gone.
	OpRemoveLink
	// OpDeleteSetFile deletes the set's file because the user deleted its
	// link from dest.
	OpDeleteSetFile
	// OpUntracked reports a real file in dest that several sets could adopt
	// (see Tied). It is left in place.
	OpUntracked
)

func (o Op) String() string {
	switch o {
	case OpMkDir:
		return "mkdir"
	case OpLink:
		return "link"
	case OpReplaceWithLink:
		return "replace-with-link"
	case OpAdopt:
		return "adopt"
	case OpBackup:
		return "backup"
	case OpConflict:
		return "conflict"
	case OpReplaceWithCopy:
		return "replace-with-copy"
	case OpRemoveLink:
		return "remove-link"
	case OpDeleteSetFile:
		return "delete-set-file"
	case OpUntracked:
		return "untracked"
	}
	return "unknown"
}

// Action is one step of a plan. Actions are ordered so a directory comes
// before anything inside it.
type Action struct {
	Op Op
	// Path is slash-separated and relative to both the set and dest. "." is
	// dest itself.
	Path string
	// Found is what is at Path in dest, for OpReplaceWithLink, OpAdopt,
	// OpBackup and OpConflict.
	Found fsnap.Entry
	// Want is what the set needs at Path (fsnap.File or fsnap.Dir), for
	// OpConflict.
	Want fsnap.Kind
	// Tied names the sets that could each adopt Path, for OpUntracked.
	Tied []string
}

// LinkInput is everything Link needs to plan linking one set.
type LinkInput struct {
	// SetDir and Dest are absolute paths.
	SetDir, Dest string
	// Set is a snapshot of SetDir. DestTree is a snapshot of Dest holding at
	// least every path in Set.
	Set, DestTree fsnap.Tree
	// Excluded reports whether a set-relative path is excluded.
	Excluded func(rel string) bool
}

// Link plans linking every non-excluded file in the set into dest. Paths
// already linked correctly get no action. Conflicts are reported as
// OpConflict actions; Resolve turns them into other actions.
//
// Below a directory conflict, dest is planned as if empty, since resolving
// the conflict (backup) leaves an empty directory there.
func Link(in LinkInput) []Action {
	var actions []Action
	var dirConflicts []string

	lookup := func(rel string) (fsnap.Entry, bool) {
		for _, c := range dirConflicts {
			if under(rel, c) {
				return fsnap.Entry{}, false
			}
		}
		e, ok := in.DestTree[rel]
		return e, ok
	}

	// dest itself.
	if root, ok := in.DestTree["."]; !ok {
		actions = append(actions, Action{Op: OpMkDir, Path: "."})
	} else if root.Kind != fsnap.Dir {
		actions = append(actions, Action{Op: OpConflict, Path: ".", Found: root, Want: fsnap.Dir})
		dirConflicts = append(dirConflicts, ".")
	}

	for _, rel := range sortedPaths(in.Set) {
		if rel == "." || in.Excluded(rel) {
			continue
		}
		setEntry := in.Set[rel]
		found, exists := lookup(rel)

		if setEntry.Kind == fsnap.Dir {
			switch {
			case !exists:
				actions = append(actions, Action{Op: OpMkDir, Path: rel})
			case found.Kind != fsnap.Dir:
				actions = append(actions, Action{Op: OpConflict, Path: rel, Found: found, Want: fsnap.Dir})
				dirConflicts = append(dirConflicts, rel)
			}
			continue
		}

		switch {
		case !exists:
			actions = append(actions, Action{Op: OpLink, Path: rel})
		case found.Kind == fsnap.Symlink && pointsAt(found.Target, in.Dest, rel, in.SetDir):
			// Already linked.
		case found.Kind == fsnap.File && setEntry.Kind == fsnap.File && found.Sum == setEntry.Sum:
			actions = append(actions, Action{Op: OpReplaceWithLink, Path: rel, Found: found})
		default:
			actions = append(actions, Action{Op: OpConflict, Path: rel, Found: found, Want: fsnap.File})
		}
	}
	return actions
}

// Resolution is how conflicts are handled.
type Resolution int

const (
	// Halt keeps conflicts; the executor refuses to run a plan with any.
	Halt Resolution = iota
	// Adopt moves conflicting real files into the set. Conflicts it
	// cannot adopt (directories, symlinks) are kept.
	Adopt
	// Force backs up whatever is in dest, then links the set's version.
	Force
	// Skip leaves conflicting paths, and everything below a conflicting
	// directory, alone.
	Skip
)

// Resolve transforms the OpConflict actions in a Link plan according to r.
func Resolve(actions []Action, r Resolution) []Action {
	if r == Halt {
		return actions
	}
	var skipped []string
	out := make([]Action, 0, len(actions))
	for _, a := range actions {
		if isUnderAny(a.Path, skipped) {
			continue
		}
		if a.Op != OpConflict {
			out = append(out, a)
			continue
		}
		switch r {
		case Adopt:
			if a.Want == fsnap.File && a.Found.Kind == fsnap.File {
				out = append(out, Action{Op: OpAdopt, Path: a.Path, Found: a.Found})
			} else {
				out = append(out, a)
			}
		case Force:
			out = append(out, Action{Op: OpBackup, Path: a.Path, Found: a.Found})
			if a.Want == fsnap.Dir {
				out = append(out, Action{Op: OpMkDir, Path: a.Path})
			} else {
				out = append(out, Action{Op: OpLink, Path: a.Path})
			}
		case Skip:
			if a.Want == fsnap.Dir {
				skipped = append(skipped, a.Path)
			}
		}
	}
	return out
}

// Conflicts returns the OpConflict actions in a plan.
func Conflicts(actions []Action) []Action {
	var out []Action
	for _, a := range actions {
		if a.Op == OpConflict {
			out = append(out, a)
		}
	}
	return out
}

// pointsAt reports whether a symlink at dest/rel with the given target
// resolves to setDir/rel. Relative targets (as Stow creates) are resolved
// against the link's directory.
func pointsAt(target, dest, rel, setDir string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(dest, filepath.FromSlash(path.Dir(rel)), target)
	}
	return filepath.Clean(target) == filepath.Join(setDir, filepath.FromSlash(rel))
}

// under reports whether rel is strictly below dir.
func under(rel, dir string) bool {
	if dir == "." {
		return rel != "."
	}
	return strings.HasPrefix(rel, dir+"/")
}

func isUnderAny(rel string, dirs []string) bool {
	for _, d := range dirs {
		if under(rel, d) {
			return true
		}
	}
	return false
}

// sortedPaths returns the tree's paths in lexical order, which puts every
// directory before its contents.
func sortedPaths(t fsnap.Tree) []string {
	paths := make([]string, 0, len(t))
	for p := range t {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
