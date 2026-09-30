package plan

import (
	"slices"
	"testing"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
)

func TestActionable(t *testing.T) {
	actions := []Action{
		{Op: OpLink, Path: "a"},
		{Op: OpConflict, Path: "b"},
		{Op: OpUntracked, Path: "c"},
		{Op: OpDeleteSetFile, Path: "d"},
	}
	if got := render(Actionable(actions)); !slices.Equal(got, []string{"link a", "delete-set-file d"}) {
		t.Errorf("Actionable = %q", got)
	}
}

func TestUpdateManifest(t *testing.T) {
	// After a watcher cycle touching new, deleted and adopted: new and
	// adopted are now linked, deleted is gone. untouched keeps its entry
	// even though this partial snapshot does not include it.
	set := fsnap.New().File("new", "n").File("adopted", "a").Tree()
	destTree := fsnap.New().
		Symlink("new", setDir+"/new").
		Symlink("adopted", setDir+"/adopted").
		Tree()
	in := LinkInput{SetDir: setDir, Dest: dest, Set: set, DestTree: destTree, Excluded: (&config.Config{}).Excluded}
	got := UpdateManifest([]string{"adopted", "deleted", "untouched"}, []string{"new", "deleted", "adopted"}, in)
	if want := []string{"adopted", "new", "untouched"}; !slices.Equal(got, want) {
		t.Errorf("UpdateManifest = %q, want %q", got, want)
	}
}
