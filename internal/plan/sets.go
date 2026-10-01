package plan

import (
	"path/filepath"
	"sort"

	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

// Linked returns the set's non-excluded files that dest links to correctly,
// sorted. After a link run it is the set's manifest.
func Linked(in LinkInput) []string {
	var out []string
	for rel, e := range in.Set {
		if rel == "." || e.Kind == fsnap.Dir || in.Excluded(rel) {
			continue
		}
		found, ok := in.DestTree[rel]
		if ok && found.Kind == fsnap.Symlink && pointsAt(found.Target, in.Dest, rel, in.SetDir) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// Claim is the paths another linked set owns: its dest and manifest.
type Claim struct {
	Set   string
	Dest  string
	Links []string
}

// SetConflict is a path the set being linked shares with another set.
type SetConflict struct {
	Path     string // relative to the linking set's dest
	OtherSet string
}

// SetConflicts returns the non-excluded files of the set being linked whose
// dest path is already linked by one of others. Sets can share a dest, but a
// path can only link to one of them. Sorted by path.
func SetConflicts(in LinkInput, others []Claim) []SetConflict {
	owner := map[string]string{}
	for _, c := range others {
		for _, l := range c.Links {
			owner[filepath.Join(c.Dest, filepath.FromSlash(l))] = c.Set
		}
	}
	var out []SetConflict
	for rel, e := range in.Set {
		if rel == "." || e.Kind == fsnap.Dir || in.Excluded(rel) {
			continue
		}
		if other, ok := owner[filepath.Join(in.Dest, filepath.FromSlash(rel))]; ok {
			out = append(out, SetConflict{Path: rel, OtherSet: other})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
