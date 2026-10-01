package plan

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

const (
	setDir = "/root/claude"
	dest   = "/home/.claude"
)

// render formats a plan as "op path" lines, with what was found for
// conflicts, so test failures read as a diff of plans.
func render(actions []Action) []string {
	out := []string{}
	for _, a := range actions {
		s := fmt.Sprintf("%s %s", a.Op, a.Path)
		if a.Op == OpConflict {
			s += fmt.Sprintf(" (found %s, want %s)", a.Found.Kind, a.Want)
		}
		out = append(out, s)
	}
	return out
}

func linkPlan(set, destTree fsnap.Tree, exclude ...string) []Action {
	cfg := &config.Config{Dest: dest, Exclude: exclude}
	return Link(LinkInput{SetDir: setDir, Dest: dest, Set: set, DestTree: destTree, Excluded: cfg.Excluded})
}

func TestLink(t *testing.T) {
	set := fsnap.New().
		File("rivet.yaml", "dest: ~/.claude").
		File("settings.json", "{}").
		File("agents/reviewer.md", "review").
		Tree()

	tests := []struct {
		name string
		dest fsnap.Tree
		want []string
	}{
		{
			name: "dest does not exist",
			dest: fsnap.Tree{},
			want: []string{"mkdir .", "mkdir agents", "link agents/reviewer.md", "link settings.json"},
		},
		{
			name: "dest exists and is empty",
			dest: fsnap.New().Tree(),
			want: []string{"mkdir agents", "link agents/reviewer.md", "link settings.json"},
		},
		{
			name: "already linked with absolute symlinks",
			dest: fsnap.New().
				Symlink("settings.json", setDir+"/settings.json").
				Symlink("agents/reviewer.md", setDir+"/agents/reviewer.md").
				Tree(),
			want: []string{},
		},
		{
			name: "already linked with relative symlinks",
			dest: fsnap.New().
				Symlink("settings.json", "../../root/claude/settings.json").
				Symlink("agents/reviewer.md", "../../../root/claude/agents/reviewer.md").
				Tree(),
			want: []string{},
		},
		{
			name: "identical real file is replaced with a link",
			dest: fsnap.New().File("settings.json", "{}").Dir("agents").Tree(),
			want: []string{"link agents/reviewer.md", "replace-with-link settings.json"},
		},
		{
			name: "different real file conflicts",
			dest: fsnap.New().File("settings.json", `{"x":1}`).Dir("agents").Tree(),
			want: []string{"link agents/reviewer.md", "conflict settings.json (found file, want file)"},
		},
		{
			name: "symlink elsewhere conflicts",
			dest: fsnap.New().Symlink("settings.json", "/elsewhere/settings.json").Dir("agents").Tree(),
			want: []string{"link agents/reviewer.md", "conflict settings.json (found symlink, want file)"},
		},
		{
			name: "directory at a file path conflicts",
			dest: fsnap.New().Dir("settings.json").Dir("agents").Tree(),
			want: []string{"link agents/reviewer.md", "conflict settings.json (found directory, want file)"},
		},
		{
			name: "files in dest that are not in the set are untouched",
			dest: fsnap.New().File("history.jsonl", "x").File("agents/draft.md", "d").Tree(),
			want: []string{"link agents/reviewer.md", "link settings.json"},
		},
		{
			name: "file at a directory path conflicts and its contents are planned as if empty",
			dest: fsnap.New().File("agents", "x").Tree(),
			want: []string{"conflict agents (found file, want directory)", "link agents/reviewer.md", "link settings.json"},
		},
		{
			name: "symlinked directory conflicts and paths through it are ignored",
			dest: fsnap.New().
				Symlink("agents", "/stow/claude/.claude/agents").
				File("agents/reviewer.md", "review").
				Tree(),
			want: []string{"conflict agents (found symlink, want directory)", "link agents/reviewer.md", "link settings.json"},
		},
		{
			name: "dest itself is a symlink",
			dest: fsnap.Tree{".": {Kind: fsnap.Symlink, Target: "/stow/claude/.claude"}},
			want: []string{"conflict . (found symlink, want directory)", "mkdir agents", "link agents/reviewer.md", "link settings.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(linkPlan(set, tt.dest))
			if !slices.Equal(got, tt.want) {
				t.Errorf("plan:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

func TestLinkExclusion(t *testing.T) {
	set := fsnap.New().
		File("rivet.yaml", "dest: ~/.claude").
		File("settings.json", "{}").
		File("history.jsonl", "h").
		File("projects/a/session.json", "s").
		File("agents/reviewer.md", "review").
		File("agents/deep/nested/debug.log", "log").
		File("agents/deep/nested/keep.md", "keep").
		Tree()
	got := render(linkPlan(set, fsnap.New().Tree(), "history.jsonl", "projects/**", "**/*.log"))
	want := []string{
		"mkdir agents",
		"mkdir agents/deep",
		"mkdir agents/deep/nested",
		"link agents/deep/nested/keep.md",
		"link agents/reviewer.md",
		"link settings.json",
	}
	if !slices.Equal(got, want) {
		t.Errorf("plan:\n got  %q\n want %q", got, want)
	}
}

func TestLinkNestedRivetYAMLIsNotExcluded(t *testing.T) {
	set := fsnap.New().File("rivet.yaml", "dest: /d").File("sub/rivet.yaml", "x").Tree()
	got := render(linkPlan(set, fsnap.New().Tree()))
	want := []string{"mkdir sub", "link sub/rivet.yaml"}
	if !slices.Equal(got, want) {
		t.Errorf("plan:\n got  %q\n want %q", got, want)
	}
}

// conflictPlan has one conflict of each shape Resolve distinguishes: a real
// file (adoptable), a symlink (not adoptable), and a directory conflict with
// a file planned below it.
func conflictPlan() []Action {
	set := fsnap.New().
		File("a.json", "set").
		File("b.json", "set").
		File("dir/c.md", "set").
		File("z.md", "set").
		Tree()
	destTree := fsnap.New().
		File("a.json", "dest").
		Symlink("b.json", "/elsewhere").
		File("dir", "not a dir").
		Tree()
	return linkPlan(set, destTree)
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		r    Resolution
		want []string
	}{
		{
			name: "halt keeps every conflict",
			r:    Halt,
			want: []string{
				"conflict a.json (found file, want file)",
				"conflict b.json (found symlink, want file)",
				"conflict dir (found file, want directory)",
				"link dir/c.md",
				"link z.md",
			},
		},
		{
			name: "adopt adopts real files and keeps the rest",
			r:    Adopt,
			want: []string{
				"adopt a.json",
				"conflict b.json (found symlink, want file)",
				"conflict dir (found file, want directory)",
				"link dir/c.md",
				"link z.md",
			},
		},
		{
			name: "force backs up and links",
			r:    Force,
			want: []string{
				"backup a.json", "link a.json",
				"backup b.json", "link b.json",
				"backup dir", "mkdir dir",
				"link dir/c.md",
				"link z.md",
			},
		},
		{
			name: "skip drops conflicts and everything below a conflicting directory",
			r:    Skip,
			want: []string{"link z.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(Resolve(conflictPlan(), tt.r))
			if !slices.Equal(got, tt.want) {
				t.Errorf("plan:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSkipDestItself(t *testing.T) {
	set := fsnap.New().File("a", "x").Tree()
	destTree := fsnap.Tree{".": {Kind: fsnap.File}}
	got := render(Resolve(linkPlan(set, destTree), Skip))
	if len(got) != 0 {
		t.Errorf("plan = %q, want empty", got)
	}
}

func TestResolveKeepsFoundOnBackupAndAdopt(t *testing.T) {
	for _, r := range []Resolution{Adopt, Force} {
		for _, a := range Resolve(conflictPlan(), r) {
			if (a.Op == OpAdopt || a.Op == OpBackup) && a.Found.Kind == 0 {
				t.Errorf("%v: %s %s has no Found entry", r, a.Op, a.Path)
			}
		}
	}
}

func TestConflicts(t *testing.T) {
	got := render(Conflicts(conflictPlan()))
	want := []string{
		"conflict a.json (found file, want file)",
		"conflict b.json (found symlink, want file)",
		"conflict dir (found file, want directory)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Conflicts:\n got  %q\n want %q", got, want)
	}
}
