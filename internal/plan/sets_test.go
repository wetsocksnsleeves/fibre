package plan

import (
	"slices"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

func TestLinked(t *testing.T) {
	set := fsnap.New().
		File("rivet.yaml", "x").
		File("a", "a").
		File("b", "b").
		File("c", "c").
		File("sub/d", "d").
		File("excluded", "e").
		Tree()
	destTree := fsnap.New().
		Symlink("a", setDir+"/a").
		Symlink("b", "/elsewhere/b").
		File("c", "c").
		Symlink("sub/d", "../../../root/claude/sub/d").
		Symlink("excluded", setDir+"/excluded").
		Tree()
	cfg := &config.Config{Exclude: []string{"excluded"}}
	got := Linked(LinkInput{SetDir: setDir, Dest: dest, Set: set, DestTree: destTree, Excluded: cfg.Excluded})
	if want := []string{"a", "sub/d"}; !slices.Equal(got, want) {
		t.Errorf("Linked = %q, want %q", got, want)
	}
}

func TestSetConflicts(t *testing.T) {
	// Linking "tools" with dest /home while "rofi" (dest /home) and "claude"
	// (dest /home/.claude) are linked.
	set := fsnap.New().
		File(".local/bin/rofi-run", "x").
		File(".local/bin/tool", "x").
		File(".claude/settings.json", "x").
		File(".claude/excluded.json", "x").
		Tree()
	cfg := &config.Config{Exclude: []string{".claude/excluded.json"}}
	in := LinkInput{SetDir: "/root/tools", Dest: "/home", Set: set, DestTree: fsnap.Tree{}, Excluded: cfg.Excluded}
	others := []Claim{
		{Set: "rofi", Dest: "/home", Links: []string{".local/bin/rofi-run", ".config/rofi/config.rasi"}},
		{Set: "claude", Dest: "/home/.claude", Links: []string{"settings.json", "excluded.json"}},
	}
	got := SetConflicts(in, others)
	want := []SetConflict{
		{Path: ".claude/settings.json", OtherSet: "claude"},
		{Path: ".local/bin/rofi-run", OtherSet: "rofi"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("SetConflicts = %+v, want %+v", got, want)
	}
}

func TestSetConflictsNone(t *testing.T) {
	set := fsnap.New().File("a", "x").Tree()
	in := LinkInput{SetDir: setDir, Dest: dest, Set: set, Excluded: (&config.Config{}).Excluded}
	if got := SetConflicts(in, []Claim{{Set: "other", Dest: "/other", Links: []string{"a"}}}); len(got) != 0 {
		t.Errorf("SetConflicts = %+v, want none", got)
	}
}
