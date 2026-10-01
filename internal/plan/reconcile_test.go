package plan

import (
	"slices"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

func claudeState(set, destTree fsnap.Tree, links ...string) SetState {
	cfg := &config.Config{Exclude: []string{"projects/**"}}
	return SetState{
		Name: "claude", SetDir: setDir, Dest: dest,
		Set: set, DestTree: destTree, Excluded: cfg.Excluded, Links: links,
	}
}

func reconcileOne(s SetState) []string {
	return render(Reconcile([]SetState{s})[0].Actions)
}

func TestReconcile(t *testing.T) {
	link := func(rel string) string { return setDir + "/" + rel }
	tests := []struct {
		name  string
		set   *fsnap.Builder
		dest  *fsnap.Builder
		links []string
		want  []string
	}{
		{
			name:  "everything linked",
			set:   fsnap.New().File("a", "a").File("sub/b", "b"),
			dest:  fsnap.New().Symlink("a", link("a")).Symlink("sub/b", link("sub/b")),
			links: []string{"a", "sub/b"},
			want:  []string{},
		},
		{
			name: "new set file is linked",
			set:  fsnap.New().File("a", "a"),
			dest: fsnap.New(),
			want: []string{"link a"},
		},
		{
			name: "new set file in a new directory gets its directories",
			set:  fsnap.New().File("x/y/a", "a"),
			dest: fsnap.New(),
			want: []string{"mkdir x", "mkdir x/y", "link x/y/a"},
		},
		{
			name:  "deleted link deletes the set file",
			set:   fsnap.New().File("a", "a"),
			dest:  fsnap.New(),
			links: []string{"a"},
			want:  []string{"delete-set-file a"},
		},
		{
			name:  "deleted directory of links deletes the set files and makes no directories",
			set:   fsnap.New().File("sub/a", "a").File("sub/b", "b"),
			dest:  fsnap.New(),
			links: []string{"sub/a", "sub/b"},
			want:  []string{"delete-set-file sub/a", "delete-set-file sub/b"},
		},
		{
			name:  "deleted set file removes the dangling link",
			set:   fsnap.New(),
			dest:  fsnap.New().Symlink("a", link("a")),
			links: []string{"a"},
			want:  []string{"remove-link a"},
		},
		{
			name:  "set file and link both gone needs nothing",
			set:   fsnap.New(),
			dest:  fsnap.New(),
			links: []string{"a"},
			want:  []string{},
		},
		{
			name:  "atomic save replacing a link is adopted",
			set:   fsnap.New().File("a", "old"),
			dest:  fsnap.New().File("a", "new"),
			links: []string{"a"},
			want:  []string{"adopt a"},
		},
		{
			name:  "atomic save with identical contents is relinked",
			set:   fsnap.New().File("a", "same"),
			dest:  fsnap.New().File("a", "same"),
			links: []string{"a"},
			want:  []string{"replace-with-link a"},
		},
		{
			name: "new set file with an identical real file in dest is relinked",
			set:  fsnap.New().File("a", "same"),
			dest: fsnap.New().File("a", "same"),
			want: []string{"replace-with-link a"},
		},
		{
			name: "new set file with a different real file in dest conflicts",
			set:  fsnap.New().File("a", "set"),
			dest: fsnap.New().File("a", "dest"),
			want: []string{"conflict a (found file, want file)"},
		},
		{
			name:  "linked path now a symlink elsewhere conflicts",
			set:   fsnap.New().File("a", "a"),
			dest:  fsnap.New().Symlink("a", "/elsewhere"),
			links: []string{"a"},
			want:  []string{"conflict a (found symlink, want file)"},
		},
		{
			name: "file where the set has a directory conflicts and blocks below it",
			set:  fsnap.New().File("sub/a", "a"),
			dest: fsnap.New().File("sub", "x"),
			want: []string{"conflict sub (found file, want directory)"},
		},
		{
			name: "untracked real file is adopted",
			set:  fsnap.New(),
			dest: fsnap.New().File("agents/draft.md", "d"),
			want: []string{"adopt agents/draft.md"},
		},
		{
			name: "excluded real file is left alone",
			set:  fsnap.New(),
			dest: fsnap.New().File("projects/p/log.json", "l").File("x.rivet-bak", "b"),
			want: []string{},
		},
		{
			name: "untracked symlinks and directories are left alone",
			set:  fsnap.New(),
			dest: fsnap.New().Symlink("mine", "/somewhere").Dir("empty"),
			want: []string{},
		},
		{
			name:  "manifest path that is now excluded is left alone",
			set:   fsnap.New(),
			dest:  fsnap.New().Symlink("projects/a", link("projects/a")),
			links: []string{"projects/a"},
			want:  []string{},
		},
		{
			name:  "missing dest is relinked, not deleted",
			set:   fsnap.New().File("a", "a").File("sub/b", "b"),
			links: []string{"a", "sub/b"},
			want:  []string{"mkdir .", "link a", "mkdir sub", "link sub/b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destTree := fsnap.Tree{}
			if tt.dest != nil {
				destTree = tt.dest.Tree()
			}
			got := reconcileOne(claudeState(tt.set.Tree(), destTree, tt.links...))
			if !slices.Equal(got, tt.want) {
				t.Errorf("plan:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

func TestReconcileDestIsNotADirectory(t *testing.T) {
	got := reconcileOne(claudeState(fsnap.New().File("a", "a").Tree(), fsnap.Tree{".": {Kind: fsnap.File}}))
	if want := []string{"conflict . (found file, want directory)"}; !slices.Equal(got, want) {
		t.Errorf("plan = %q, want %q", got, want)
	}
}

func TestReconcileStrict(t *testing.T) {
	s := claudeState(
		fsnap.New().File("a", "old").Tree(),
		fsnap.New().File("a", "new").File("untracked", "u").Tree(),
		"a",
	)
	s.Strict = true
	got := reconcileOne(s)
	if want := []string{"adopt a"}; !slices.Equal(got, want) {
		t.Errorf("plan = %q, want only the replaced link adopted", got)
	}
}

// ownershipSets models $HOME with sets at several depths, all seeing the
// same new files in their dest snapshots.
func ownershipSets(t *testing.T) []SetState {
	t.Helper()
	home := "/home"
	mk := func(name, dest string, strict bool, exclude []string, files ...string) SetState {
		b := fsnap.New()
		for _, f := range files {
			b.File(f, "x")
		}
		cfg := &config.Config{Exclude: exclude}
		return SetState{
			Name: name, SetDir: "/root/" + name, Dest: dest, Strict: strict,
			Set: fsnap.New().Tree(), DestTree: b.Tree(), Excluded: cfg.Excluded,
		}
	}
	return []SetState{
		mk("home", home, false, nil,
			".claude/agents/draft.md", ".claude/projects/p.json", ".local/bin/myscript", ".zshrc.new"),
		mk("zsh", home, true, nil,
			".claude/agents/draft.md", ".claude/projects/p.json", ".local/bin/myscript", ".zshrc.new"),
		mk("claude", home+"/.claude", false, []string{"projects/**"},
			"agents/draft.md", "projects/p.json"),
		mk("rofi", home+"/.local", false, nil, "bin/myscript"),
		mk("tools", home+"/.local", false, nil, "bin/myscript"),
	}
}

func TestReconcileOwnership(t *testing.T) {
	plans := Reconcile(ownershipSets(t))
	got := map[string][]string{}
	for _, p := range plans {
		got[p.Name] = render(p.Actions)
	}
	want := map[string][]string{
		// Deeper sets own .claude and .local; only .zshrc.new is home's.
		"home": {"adopt .zshrc.new"},
		// Strict sets never adopt.
		"zsh": {},
		// claude is deepest for its files; its exclude keeps projects/p.json
		// from falling through to home.
		"claude": {"adopt agents/draft.md"},
		// rofi and tools tie.
		"rofi":  {"untracked bin/myscript"},
		"tools": {"untracked bin/myscript"},
	}
	for name, w := range want {
		if !slices.Equal(got[name], w) {
			t.Errorf("%s:\n got  %q\n want %q", name, got[name], w)
		}
	}
}

func TestReconcileTieNamesSets(t *testing.T) {
	for _, p := range Reconcile(ownershipSets(t)) {
		if p.Name != "rofi" {
			continue
		}
		if len(p.Actions) != 1 || !slices.Equal(p.Actions[0].Tied, []string{"rofi", "tools"}) {
			t.Errorf("rofi actions = %+v, want one untracked action tied between rofi and tools", p.Actions)
		}
	}
}

func TestReconcileTieBrokenByExclude(t *testing.T) {
	sets := ownershipSets(t)
	for i := range sets {
		if sets[i].Name == "tools" {
			sets[i].Excluded = (&config.Config{Exclude: []string{"bin/**"}}).Excluded
		}
	}
	for _, p := range Reconcile(sets) {
		if p.Name == "rofi" {
			if got := render(p.Actions); !slices.Equal(got, []string{"adopt bin/myscript"}) {
				t.Errorf("rofi = %q, want it to adopt once tools excludes the path", got)
			}
		}
	}
}
