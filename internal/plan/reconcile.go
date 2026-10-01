package plan

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
)

// SetState is one linked set as Reconcile sees it.
type SetState struct {
	Name string
	// SetDir and Dest are absolute paths.
	SetDir, Dest string
	// Set is a snapshot of SetDir. DestTree is a snapshot of Dest holding
	// every set and manifest path, hashed, and (for sets that are not
	// strict) every other file in Dest, which need not be hashed.
	Set, DestTree fsnap.Tree
	Excluded      func(rel string) bool
	Strict        bool
	// Links is the manifest.
	Links []string
}

// SetPlan is the reconcile plan for one set.
type SetPlan struct {
	Name    string
	Actions []Action
}

// Reconcile plans bringing each linked set and its dest back in line after
// changes made outside fibre, using the manifest to tell a new set file
// from a deleted link:
//
//	set file  dest                 manifest  action
//	yes       missing              no        link
//	yes       missing              yes       delete set file (user deleted the link)
//	no        link to the set      yes       remove link (set file deleted)
//	yes       real file            yes       adopt and relink (atomic save)
//	no        real file            -         adopt, if this set owns the path
//
// A real file in dest belongs to the set, among those that are not strict,
// with the deepest dest containing it. If that set excludes it, no set
// adopts it. If several sets share that dest, it is reported as OpUntracked
// for each of them and left in place. Strict sets never adopt new files but
// still adopt real files that replaced their links.
//
// If a set's dest is missing entirely, its links are recreated rather than
// its files deleted: a missing dest (an app reinstalled, a volume not
// mounted) is not the user deleting every link.
func Reconcile(sets []SetState) []SetPlan {
	out := make([]SetPlan, len(sets))
	for i, s := range sets {
		out[i] = SetPlan{Name: s.Name, Actions: reconcileSet(s, sets)}
	}
	return out
}

func reconcileSet(s SetState, all []SetState) []Action {
	root, destExists := s.DestTree["."]
	if destExists && root.Kind != fsnap.Dir {
		return []Action{{Op: OpConflict, Path: ".", Found: root, Want: fsnap.Dir}}
	}
	manifest := map[string]bool{}
	for _, l := range s.Links {
		manifest[l] = true
	}

	var actions []Action
	var blocked []string // conflicting paths; nothing below them is planned
	mkdirs := map[string]bool{}
	conflict := func(rel string, found fsnap.Entry, want fsnap.Kind) {
		actions = append(actions, Action{Op: OpConflict, Path: rel, Found: found, Want: want})
		blocked = append(blocked, rel)
	}

	for _, rel := range sortedPaths(s.Set) {
		if rel == "." || s.Excluded(rel) || isUnderAny(rel, blocked) {
			continue
		}
		setEntry := s.Set[rel]
		found, exists := s.DestTree[rel]
		if setEntry.Kind == fsnap.Dir {
			if exists && found.Kind != fsnap.Dir {
				conflict(rel, found, fsnap.Dir)
			}
			continue
		}
		switch {
		case !exists && manifest[rel] && destExists:
			actions = append(actions, Action{Op: OpDeleteSetFile, Path: rel})
		case !exists:
			for p := path.Dir(rel); ; p = path.Dir(p) {
				if _, ok := s.DestTree[p]; !ok {
					mkdirs[p] = true
				}
				if p == "." {
					break
				}
			}
			actions = append(actions, Action{Op: OpLink, Path: rel})
		case found.Kind == fsnap.Symlink && pointsAt(found.Target, s.Dest, rel, s.SetDir):
			// Linked.
		case found.Kind == fsnap.File && setEntry.Kind == fsnap.File && found.Sum == setEntry.Sum:
			actions = append(actions, Action{Op: OpReplaceWithLink, Path: rel, Found: found})
		case found.Kind == fsnap.File && manifest[rel]:
			actions = append(actions, Action{Op: OpAdopt, Path: rel, Found: found})
		default:
			conflict(rel, found, fsnap.File)
		}
	}

	for _, rel := range s.Links {
		if _, inSet := s.Set[rel]; inSet || s.Excluded(rel) || isUnderAny(rel, blocked) {
			continue
		}
		if found, ok := s.DestTree[rel]; ok && found.Kind == fsnap.Symlink && pointsAt(found.Target, s.Dest, rel, s.SetDir) {
			actions = append(actions, Action{Op: OpRemoveLink, Path: rel, Found: found})
		}
	}

	if !s.Strict {
		for _, rel := range sortedPaths(s.DestTree) {
			found := s.DestTree[rel]
			if found.Kind != fsnap.File || isUnderAny(rel, blocked) {
				continue
			}
			if _, inSet := s.Set[rel]; inSet {
				continue
			}
			owners := owners(filepath.Join(s.Dest, filepath.FromSlash(rel)), all)
			switch {
			case len(owners) == 1 && owners[0] == s.Name:
				actions = append(actions, Action{Op: OpAdopt, Path: rel, Found: found})
			case len(owners) > 1 && contains(owners, s.Name):
				actions = append(actions, Action{Op: OpUntracked, Path: rel, Found: found, Tied: owners})
			}
		}
	}

	for p := range mkdirs {
		actions = append(actions, Action{Op: OpMkDir, Path: p})
	}
	sortActions(actions)
	return actions
}

// owners returns the sets that may adopt the file at abs: of the sets that
// are not strict and whose dest contains abs, those with the deepest dest,
// minus any that exclude it. Sorted by name.
func owners(abs string, all []SetState) []string {
	deepest := -1
	for _, s := range all {
		if !s.Strict && within(abs, s.Dest) && len(s.Dest) > deepest {
			deepest = len(s.Dest)
		}
	}
	var names []string
	for _, s := range all {
		if s.Strict || !within(abs, s.Dest) || len(s.Dest) != deepest {
			continue
		}
		rel, _ := filepath.Rel(s.Dest, abs)
		if !s.Excluded(filepath.ToSlash(rel)) {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	return names
}

// within reports whether p is strictly inside dir.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// sortActions orders actions by path with "." first, so every directory
// comes before its contents.
func sortActions(actions []Action) {
	key := func(p string) string {
		if p == "." {
			return ""
		}
		return p
	}
	sort.SliceStable(actions, func(i, j int) bool { return key(actions[i].Path) < key(actions[j].Path) })
}
