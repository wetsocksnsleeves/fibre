package plan

import (
	"slices"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

func TestImport(t *testing.T) {
	destTree := fsnap.New().
		File("settings.json", "{}").
		File("rivet.yaml", "not ours").
		File("agents/reviewer.md", "r").
		File("agents/deep/nested.md", "n").
		Dir("empty").
		File("history.jsonl", "h").
		File("projects/p/log.json", "l").
		Symlink("stowed.md", "../.dotfiles/claude/.claude/stowed.md").
		Tree()
	destTree["socket"] = fsnap.Entry{Kind: fsnap.Other}
	cfg := &config.Config{Exclude: []string{"history.jsonl", "projects/**"}}

	actions, skipped := Import(destTree, cfg.Excluded)

	wantActions := []string{"adopt agents/deep/nested.md", "adopt agents/reviewer.md", "adopt settings.json"}
	if got := render(actions); !slices.Equal(got, wantActions) {
		t.Errorf("actions:\n got  %q\n want %q", got, wantActions)
	}
	var gotSkipped []string
	for _, s := range skipped {
		gotSkipped = append(gotSkipped, s.Path+" "+s.Found.Kind.String())
	}
	if want := []string{"socket special file", "stowed.md symlink"}; !slices.Equal(gotSkipped, want) {
		t.Errorf("skipped = %q, want %q", gotSkipped, want)
	}
}

func TestImportEmptyDest(t *testing.T) {
	actions, skipped := Import(fsnap.New().Tree(), (&config.Config{}).Excluded)
	if len(actions) != 0 || len(skipped) != 0 {
		t.Errorf("Import of empty dest = %v, %v", actions, skipped)
	}
}
