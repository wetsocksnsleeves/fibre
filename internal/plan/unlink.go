package plan

import "github.com/wetsocksnsleeves/fibre/internal/fsnap"

// UnlinkInput is everything Unlink needs to plan unlinking one set.
type UnlinkInput struct {
	// SetDir and Dest are absolute paths.
	SetDir, Dest string
	// Links is the set's manifest.
	Links []string
	// Set and DestTree are snapshots of SetDir and Dest holding at least
	// every path in Links.
	Set, DestTree fsnap.Tree
}

// Unlink plans replacing each manifest link that still points at the set
// with a copy of its set file, so dest keeps the same contents. A link whose
// set file is gone is removed. Manifest paths that are no longer links to
// the set (the user replaced them) are returned as skipped; paths with
// nothing at them need no action.
func Unlink(in UnlinkInput) ([]Action, []Skipped) {
	var actions []Action
	var skipped []Skipped
	for _, rel := range in.Links {
		found, ok := in.DestTree[rel]
		if !ok {
			continue
		}
		if found.Kind != fsnap.Symlink || !pointsAt(found.Target, in.Dest, rel, in.SetDir) {
			skipped = append(skipped, Skipped{Path: rel, Found: found})
			continue
		}
		switch set, ok := in.Set[rel]; {
		case !ok:
			actions = append(actions, Action{Op: OpRemoveLink, Path: rel, Found: found})
		case set.Kind == fsnap.File || set.Kind == fsnap.Symlink:
			actions = append(actions, Action{Op: OpReplaceWithCopy, Path: rel, Found: found})
		default:
			// A directory or special file where a linked file was; copying it
			// is not meaningful, so leave the link for the user.
			skipped = append(skipped, Skipped{Path: rel, Found: found})
		}
	}
	return actions, skipped
}
