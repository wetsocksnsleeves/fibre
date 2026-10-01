package plan

import (
	"slices"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

func TestUnlink(t *testing.T) {
	set := fsnap.New().
		File("settings.json", "{}").
		File("agents/reviewer.md", "r").
		Symlink("tool", "/usr/bin/tool").
		File("replaced.md", "set").
		File("relinked.md", "set").
		Dir("now-a-dir").
		Tree()
	destTree := fsnap.New().
		Symlink("settings.json", setDir+"/settings.json").
		Symlink("agents/reviewer.md", "../../../root/claude/agents/reviewer.md").
		Symlink("tool", setDir+"/tool").
		Symlink("deleted.md", setDir+"/deleted.md").
		File("replaced.md", "user edit").
		Symlink("relinked.md", "/elsewhere/relinked.md").
		Symlink("now-a-dir", setDir+"/now-a-dir").
		Symlink("not-in-manifest.md", setDir+"/not-in-manifest.md").
		Tree()
	links := []string{
		"agents/reviewer.md", "deleted.md", "gone.md", "now-a-dir",
		"relinked.md", "replaced.md", "settings.json", "tool",
	}

	actions, skipped := Unlink(UnlinkInput{SetDir: setDir, Dest: dest, Links: links, Set: set, DestTree: destTree})

	wantActions := []string{
		"replace-with-copy agents/reviewer.md",
		"remove-link deleted.md",
		"replace-with-copy settings.json",
		"replace-with-copy tool",
	}
	if got := render(actions); !slices.Equal(got, wantActions) {
		t.Errorf("actions:\n got  %q\n want %q", got, wantActions)
	}
	var gotSkipped []string
	for _, s := range skipped {
		gotSkipped = append(gotSkipped, s.Path)
	}
	if want := []string{"now-a-dir", "relinked.md", "replaced.md"}; !slices.Equal(gotSkipped, want) {
		t.Errorf("skipped = %q, want %q", gotSkipped, want)
	}
}
