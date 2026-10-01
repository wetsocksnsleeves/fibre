package plan

import "github.com/wetsocksnsleeves/fibre/internal/fsnap"

// Skipped is a dest path Import leaves in place because it is not a regular
// file, such as a symlink left by Stow.
type Skipped struct {
	Path  string
	Found fsnap.Entry
}

// Import plans moving every non-excluded regular file in dest into an empty
// set and linking it back, as OpAdopt actions. Directories need no action:
// they already exist in dest, and adopting a file creates its parents in the
// set. Symlinks and special files are returned as skipped.
func Import(destTree fsnap.Tree, excluded func(rel string) bool) ([]Action, []Skipped) {
	var actions []Action
	var skipped []Skipped
	for _, rel := range sortedPaths(destTree) {
		if rel == "." || excluded(rel) {
			continue
		}
		switch e := destTree[rel]; e.Kind {
		case fsnap.Dir:
		case fsnap.File:
			actions = append(actions, Action{Op: OpAdopt, Path: rel, Found: e})
		default:
			skipped = append(skipped, Skipped{Path: rel, Found: e})
		}
	}
	return actions, skipped
}
