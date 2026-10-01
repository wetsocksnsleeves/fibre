package watch

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
	"github.com/wetsocksnsleeves/rivet/internal/plan"
)

const (
	home      = "/home"
	rootDir   = "/home/dotfiles"
	claudeDir = rootDir + "/claude"
	claudeDst = home + "/.claude"
)

var window = 500 * time.Millisecond

func claudeTarget() Target {
	return Target{Name: "claude", SetDir: claudeDir, Dest: claudeDst, Excluded: (&config.Config{Exclude: []string{"projects/**"}}).Excluded}
}

func homeTarget() Target {
	return Target{Name: "home", SetDir: rootDir + "/home", Dest: home, Excluded: (&config.Config{}).Excluded}
}

func claudeState(set, dest *fsnap.Builder, links ...string) plan.SetState {
	t := claudeTarget()
	return plan.SetState{
		Name: t.Name, SetDir: t.SetDir, Dest: t.Dest,
		Set: set.Tree(), DestTree: dest.Tree(), Excluded: t.Excluded, Links: links,
	}
}

func linkTo(rel string) string { return claudeDir + "/" + rel }

// runEvents feeds events through a debouncer, waits out the window, routes
// the due paths and plans them against the snapshots taken afterwards.
func runEvents(t *testing.T, targets []Target, events []string, sets ...plan.SetState) map[string][]string {
	t.Helper()
	d := NewDebouncer(window)
	for i, p := range events {
		d.Event(p, at(i*50))
	}
	var hits []Hit
	for {
		next, ok := d.Next()
		if !ok {
			break
		}
		for _, p := range d.Due(next) {
			hits = append(hits, Route(p, targets, []string{rootDir})...)
		}
	}
	if len(hits) == 0 {
		t.Fatal("no hits from events")
	}
	return render(Plan(sets, hits))
}

func render(plans []plan.SetPlan) map[string][]string {
	out := map[string][]string{}
	for _, p := range plans {
		lines := []string{}
		for _, a := range p.Actions {
			lines = append(lines, fmt.Sprintf("%s %s", a.Op, a.Path))
		}
		out[p.Name] = lines
	}
	return out
}

func assertPlans(t *testing.T, got, want map[string][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("plans for %d sets, want %d: %q", len(got), len(want), got)
	}
	for name, w := range want {
		if !slices.Equal(got[name], w) {
			t.Errorf("%s:\n got  %q\n want %q", name, got[name], w)
		}
	}
}

func TestRoute(t *testing.T) {
	targets := []Target{claudeTarget(), homeTarget()}
	tests := []struct {
		path string
		want []Hit
	}{
		{claudeDst + "/settings.json", []Hit{{"claude", "settings.json"}, {"home", ".claude/settings.json"}}},
		{claudeDir + "/settings.json", []Hit{{"claude", "settings.json"}}},
		{claudeDst, []Hit{{"claude", "."}, {"home", ".claude"}}},
		{claudeDst + "/projects/p/log.json", []Hit{{"home", ".claude/projects/p/log.json"}}},
		{home + "/.zshrc", []Hit{{"home", ".zshrc"}}},
		{rootDir + "/.git/index", nil},
		{"/elsewhere/file", nil},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := Route(tt.path, targets, []string{rootDir}); !slices.Equal(got, tt.want) {
				t.Errorf("Route = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAtomicRenameIsAdopted(t *testing.T) {
	// The editor writes a temp file and renames it over the link.
	events := []string{
		claudeDst + "/.settings.json.tmp1",
		claudeDst + "/.settings.json.tmp1",
		claudeDst + "/settings.json",
	}
	after := claudeState(
		fsnap.New().File("settings.json", "old"),
		fsnap.New().File("settings.json", "new"),
		"settings.json",
	)
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	assertPlans(t, got, map[string][]string{"claude": {"adopt settings.json"}})
}

func TestDeleteThenCreateIsAReplacement(t *testing.T) {
	// Some apps save by deleting the file and writing a new one. The delete
	// is not propagated to the set.
	events := []string{claudeDst + "/settings.json", claudeDst + "/settings.json"}
	after := claudeState(
		fsnap.New().File("settings.json", "old"),
		fsnap.New().File("settings.json", "new"),
		"settings.json",
	)
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	assertPlans(t, got, map[string][]string{"claude": {"adopt settings.json"}})
}

func TestDeleteAloneIsPropagated(t *testing.T) {
	events := []string{claudeDst + "/settings.json"}
	after := claudeState(fsnap.New().File("settings.json", "x"), fsnap.New(), "settings.json")
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	assertPlans(t, got, map[string][]string{"claude": {"delete-set-file settings.json"}})
}

func TestNewSetFileIsLinked(t *testing.T) {
	// e.g. from git pull, into a directory dest does not have yet.
	events := []string{claudeDir + "/commands", claudeDir + "/commands/new.md"}
	after := claudeState(fsnap.New().File("commands/new.md", "n").File("settings.json", "s"), fsnap.New(), "settings.json")
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	// settings.json's missing link is not part of this event.
	assertPlans(t, got, map[string][]string{"claude": {"mkdir commands", "link commands/new.md"}})
}

func TestSetFileDeletedRemovesLink(t *testing.T) {
	events := []string{claudeDir + "/gone.md"}
	after := claudeState(fsnap.New(), fsnap.New().Symlink("gone.md", linkTo("gone.md")), "gone.md")
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	assertPlans(t, got, map[string][]string{"claude": {"remove-link gone.md"}})
}

func TestNewFileInNestedDestGoesToDeepestSet(t *testing.T) {
	events := []string{claudeDst + "/agents/draft.md"}
	claude := claudeState(fsnap.New(), fsnap.New().File("agents/draft.md", "d"))
	h := homeTarget()
	homeSet := plan.SetState{
		Name: h.Name, SetDir: h.SetDir, Dest: h.Dest, Excluded: h.Excluded,
		Set: fsnap.New().Tree(), DestTree: fsnap.New().File(".claude/agents/draft.md", "d").Tree(),
	}
	got := runEvents(t, []Target{claudeTarget(), h}, events, claude, homeSet)
	assertPlans(t, got, map[string][]string{
		"claude": {"adopt agents/draft.md"},
		"home":   {},
	})
}

func TestExcludedEventsAreDropped(t *testing.T) {
	hits := Route(claudeDst+"/projects/p/log.json", []Target{claudeTarget()}, nil)
	if len(hits) != 0 {
		t.Errorf("Route = %+v, want nothing", hits)
	}
}

func TestSelfTriggeredEventsPlanNothing(t *testing.T) {
	// After the watcher adopts settings.json, it sees events for the move
	// into the set and the new link. Both sides are consistent by then.
	events := []string{claudeDst + "/settings.json", claudeDir + "/settings.json", claudeDst + "/settings.json"}
	after := claudeState(
		fsnap.New().File("settings.json", "new"),
		fsnap.New().Symlink("settings.json", linkTo("settings.json")),
		"settings.json",
	)
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	assertPlans(t, got, map[string][]string{"claude": {}})
}

func TestEventsDuringCLILockPlanNothing(t *testing.T) {
	// rivet link runs while the watcher is up: its links raise events, which
	// are held until the lock is released and then find nothing to do.
	d := NewDebouncer(window)
	d.SetLocked(true)
	d.Event(claudeDst+"/settings.json", at(0))
	d.Event(claudeDst+"/agents/reviewer.md", at(10))
	if due := d.Due(at(5000)); len(due) != 0 {
		t.Fatalf("due while locked: %q", due)
	}
	d.SetLocked(false)
	var hits []Hit
	for _, p := range d.Due(at(5000)) {
		hits = append(hits, Route(p, []Target{claudeTarget()}, nil)...)
	}
	after := claudeState(
		fsnap.New().File("settings.json", "s").File("agents/reviewer.md", "r"),
		fsnap.New().Symlink("settings.json", linkTo("settings.json")).Symlink("agents/reviewer.md", linkTo("agents/reviewer.md")),
		"agents/reviewer.md", "settings.json",
	)
	assertPlans(t, render(Plan([]plan.SetState{after}, hits)), map[string][]string{"claude": {}})
}

func TestDestRemovedEntirelyIsRelinked(t *testing.T) {
	events := []string{claudeDst}
	after := claudeState(fsnap.New().File("settings.json", "s"), fsnap.New(), "settings.json")
	after.DestTree = fsnap.Tree{}
	got := runEvents(t, []Target{claudeTarget()}, events, after)
	assertPlans(t, got, map[string][]string{"claude": {"mkdir .", "link settings.json"}})
}

func TestPlanLeavesOutUnrelatedProblems(t *testing.T) {
	// The set has an untracked file and a deleted link, but only the event
	// path is planned.
	hits := Route(claudeDst+"/settings.json", []Target{claudeTarget()}, nil)
	s := claudeState(
		fsnap.New().File("settings.json", "old").File("notes.md", "n"),
		fsnap.New().File("settings.json", "new").File("draft.md", "d"),
		"notes.md", "settings.json",
	)
	assertPlans(t, render(Plan([]plan.SetState{s}, hits)), map[string][]string{"claude": {"adopt settings.json"}})
}
