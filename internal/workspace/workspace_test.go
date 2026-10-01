package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
)

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(p), 0o644); err != nil {
		t.Fatal(err)
	}
}

func paths(tree fsnap.Tree) []string {
	var out []string
	for p := range tree {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func testSet(t *testing.T) (Set, Ignored) {
	t.Helper()
	base := t.TempDir()
	setDir, dest := filepath.Join(base, "root", "claude"), filepath.Join(base, "dest")
	for _, p := range []string{"agents/a.md", "agents/sub/b.md", "settings.json", "projects/x.json"} {
		write(t, filepath.Join(setDir, p))
	}
	for _, p := range []string{"agents/new.md", "agents/sub/c.md", "other.md", "projects/y.json"} {
		write(t, filepath.Join(dest, p))
	}
	cfg := &config.Config{Dest: dest, Exclude: []string{"projects/**"}}
	s := Set{Name: "claude", SetDir: setDir, Config: cfg, Links: []string{"agents/gone.md", "settings.json"}}
	return s, Ignored{Root: filepath.Join(base, "root"), StateDir: filepath.Join(base, "state")}
}

func TestSnapshotPathsCoversOnlyTheSubtree(t *testing.T) {
	s, ig := testSet(t)
	got, err := s.SnapshotPaths([]string{"agents"}, ig)
	if err != nil {
		t.Fatal(err)
	}
	// agents/gone.md is a manifest path with no set file, so it is looked
	// up but absent.
	if want := []string{".", "agents", "agents/a.md", "agents/sub", "agents/sub/b.md"}; !slices.Equal(paths(got.Set), want) {
		t.Errorf("set paths = %q, want %q", paths(got.Set), want)
	}
	// Dest has the agents subtree and nothing outside it (other.md).
	if want := []string{".", "agents", "agents/new.md", "agents/sub", "agents/sub/c.md"}; !slices.Equal(paths(got.DestTree), want) {
		t.Errorf("dest paths = %q, want %q", paths(got.DestTree), want)
	}
}

func TestSnapshotPathsHashesSetFiles(t *testing.T) {
	s, ig := testSet(t)
	got, err := s.SnapshotPaths([]string{"agents/sub/b.md"}, ig)
	if err != nil {
		t.Fatal(err)
	}
	if e := got.Set["agents/sub/b.md"]; e.Kind != fsnap.File || e.Sum == ([32]byte{}) {
		t.Errorf("set entry = %+v, want a hashed file", e)
	}
	if _, ok := got.Set["agents/sub"]; !ok {
		t.Error("parent directory missing from the set snapshot")
	}
}

func TestSnapshotSkipsExcludedAndIgnored(t *testing.T) {
	s, ig := testSet(t)
	// The root sits inside dest here, as ~/.dotfiles does inside $HOME.
	s.Config.Dest = filepath.Dir(ig.Root)
	got, err := s.Snapshot(ig)
	if err != nil {
		t.Fatal(err)
	}
	for p := range got.DestTree {
		if p == "root" || filepath.Dir(p) == "root" {
			t.Errorf("dest snapshot includes the root: %s", p)
		}
	}
	if _, ok := got.Set["projects/x.json"]; ok {
		t.Error("set snapshot includes an excluded file")
	}
}
