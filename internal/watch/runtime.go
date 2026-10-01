package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/exec"
	"github.com/wetsocksnsleeves/fibre/internal/plan"
	"github.com/wetsocksnsleeves/fibre/internal/state"
	"github.com/wetsocksnsleeves/fibre/internal/workspace"
)

// Runtime is the watcher process: it watches every linked set and its dest,
// and applies reconcile plans as paths settle.
type Runtime struct {
	StateDir string
	// Window is how long a path must be quiet before it is acted on.
	Window time.Duration
	Env    config.Env
	Log    io.Writer
	// Ready, if set, is called once startup reconciliation is done and
	// events are being handled.
	Ready func()
}

type runtime struct {
	*Runtime
	log     *log.Logger
	fsw     *dirWatcher
	sets    []workspace.Set // as of the last watch sync
	ig      workspace.Ignored
	resync  bool // watches need syncing
	deb     *Debouncer
	watched map[string]bool
	locked  bool // the CLI held the lock when paths last came due
}

// lockPoll is how often a watcher waiting on the CLI lock checks it.
const lockPoll = 100 * time.Millisecond

// Run watches until ctx is done. Only one watcher runs per state directory.
func (r *Runtime) Run(ctx context.Context) error {
	wl, err := state.AcquireWatcher(r.StateDir)
	if err != nil {
		return err
	}
	defer wl.Release()

	fsw, err := newDirWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	rt := &runtime{
		Runtime: r,
		log:     log.New(r.Log, "", log.LstdFlags),
		fsw:     fsw,
		deb:     NewDebouncer(r.Window),
		watched: map[string]bool{},
	}
	if err := rt.startup(ctx); err != nil {
		return err
	}
	if r.Ready != nil {
		r.Ready()
	}

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	poll := time.NewTicker(lockPoll)
	defer poll.Stop()
	for {
		if next, ok := rt.deb.Next(); ok {
			timer.Reset(max(0, time.Until(next)))
		} else {
			timer.Stop()
		}
		select {
		case <-ctx.Done():
			return nil
		case dir := <-fsw.events:
			rt.onEvent(dir)
		case err := <-fsw.errors:
			rt.log.Printf("watch error: %v", err)
		case <-timer.C:
			rt.processDue(time.Now())
		case <-poll.C:
			rt.checkLock()
			if rt.resync {
				rt.resync = false
				rt.syncWatches(rt.sets, rt.ig)
			}
		}
	}
}

// startup waits for the CLI lock, then reconciles every linked set in full:
// changes made while the watcher was stopped raised no events.
func (rt *runtime) startup(ctx context.Context) error {
	if err := os.MkdirAll(rt.StateDir, 0o755); err != nil {
		return err
	}
	for {
		lock, err := state.TryAcquire(rt.StateDir)
		if err == nil {
			defer lock.Release()
			break
		}
		if !errors.Is(err, state.ErrLocked) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockPoll):
		}
	}
	rt.cycle(nil)
	return nil
}

// onEvent handles a change in a watched directory.
func (rt *runtime) onEvent(dir string) {
	if dir == rt.StateDir {
		// Another process (link, unlink, init) may have changed state: pick
		// up new and removed sets. The watcher's own saves land here too.
		rt.refreshWatches()
		return
	}
	rt.deb.Event(dir, time.Now())
	// The directory may have gained subdirectories; watch them soon, so
	// changes inside raise events. Anything already inside is covered when
	// this directory's event comes due, since its whole subtree is
	// reconciled.
	rt.resync = true
}

// processDue acts on the paths that have settled, unless the CLI holds the
// lock, in which case they wait until it is released.
func (rt *runtime) processDue(now time.Time) {
	due := rt.deb.Due(now)
	if len(due) == 0 {
		return
	}
	lock, err := state.TryAcquire(rt.StateDir)
	if errors.Is(err, state.ErrLocked) {
		for _, p := range due {
			rt.deb.Event(p, now)
		}
		rt.deb.SetLocked(true)
		rt.locked = true
		return
	}
	if err != nil {
		rt.log.Printf("lock: %v", err)
		return
	}
	defer lock.Release()
	rt.cycle(due)
}

func (rt *runtime) checkLock() {
	if !rt.locked {
		return
	}
	lock, err := state.TryAcquire(rt.StateDir)
	if err != nil {
		return
	}
	lock.Release()
	rt.locked = false
	rt.deb.SetLocked(false)
}

// cycle loads state, plans the given paths (every path when paths is nil),
// applies what can be applied, and records the new manifests. The caller
// holds the lock. State is reloaded every cycle, so a CLI command that ran
// between cycles (such as unlink) is always seen.
func (rt *runtime) cycle(paths []string) {
	st, err := state.Load(rt.StateDir)
	if err != nil {
		rt.log.Printf("state: %v", err)
		return
	}
	sets := workspace.Load(st, rt.Env)
	ig := workspace.Ignored{Root: st.Root, StateDir: rt.StateDir}

	var targets []Target
	for _, s := range sets {
		if s.Err != nil {
			if paths == nil {
				rt.log.Printf("%s: not synced: %v", s.Name, s.Err)
			}
			continue
		}
		targets = append(targets, Target{Name: s.Name, SetDir: s.SetDir, Dest: s.Config.Dest, Excluded: s.Config.Excluded})
	}
	rels := map[string][]string{}
	var hits []Hit
	for _, p := range paths {
		for _, h := range Route(p, targets, ig.Dirs()) {
			hits = append(hits, h)
			rels[h.Set] = append(rels[h.Set], h.Rel)
		}
	}

	snapshot := func(s workspace.Set) (plan.SetState, error) {
		if paths == nil {
			return s.Snapshot(ig)
		}
		return s.SnapshotPaths(rels[s.Name], ig)
	}
	var states []plan.SetState
	byName := map[string]workspace.Set{}
	for _, s := range sets {
		if s.Err != nil {
			continue
		}
		byName[s.Name] = s
		if _, hit := rels[s.Name]; paths != nil && !hit {
			states = append(states, s.Bare())
			continue
		}
		ss, err := snapshot(s)
		if err != nil {
			rt.log.Printf("%s: %v", s.Name, err)
			ss = s.Bare()
		}
		states = append(states, ss)
	}

	var plans []plan.SetPlan
	if paths == nil {
		plans = plan.Reconcile(states)
	} else {
		plans = Plan(states, hits)
	}

	changed := false
	for _, p := range plans {
		s := byName[p.Name]
		for _, a := range p.Actions {
			if a.Op == plan.OpConflict || a.Op == plan.OpUntracked {
				rt.log.Printf("%s: %s %s left in place; see `fibre status`", s.Name, a.Op, a.Path)
			}
		}
		actions := plan.Actionable(p.Actions)
		if len(actions) == 0 {
			continue
		}
		_, err := exec.Apply(s.SetDir, s.Config.Dest, actions)
		for _, a := range actions {
			if line := describe(a); line != "" {
				rt.log.Printf("%s: %s", s.Name, line)
			}
		}
		if err != nil {
			rt.log.Printf("%s: %v", s.Name, err)
		}
		after, serr := snapshot(s)
		if serr != nil {
			rt.log.Printf("%s: %v", s.Name, serr)
			continue
		}
		in := plan.LinkInput{SetDir: after.SetDir, Dest: after.Dest, Set: after.Set, DestTree: after.DestTree, Excluded: after.Excluded}
		st.Linked[s.Name].Links = plan.UpdateManifest(s.Links, touched(after, actions, s.Links, rels[s.Name], paths == nil), in)
		changed = true
	}
	if changed {
		if err := state.Save(rt.StateDir, st); err != nil {
			rt.log.Printf("state: %v", err)
		}
	}
	rt.syncWatches(sets, ig)
}

// refreshWatches reloads state and updates the watched directories.
func (rt *runtime) refreshWatches() {
	st, err := state.Load(rt.StateDir)
	if err != nil {
		rt.log.Printf("state: %v", err)
		return
	}
	rt.syncWatches(workspace.Load(st, rt.Env), workspace.Ignored{Root: st.Root, StateDir: rt.StateDir})
}

// syncWatches watches exactly the directories events can matter in: the
// state directory, every set directory, every non-strict dest in full, and
// for strict dests (often $HOME) only the directories holding their links.
// Excluded directories are never watched: on macOS each watch holds a file
// descriptor per entry.
func (rt *runtime) syncWatches(sets []workspace.Set, ig workspace.Ignored) {
	rt.sets, rt.ig = sets, ig
	want := map[string]bool{rt.StateDir: true}
	for _, s := range sets {
		if s.Err != nil {
			continue
		}
		addDirs(want, s.SetDir, s.Config.Excluded, ig)
		if !s.Config.Strict {
			addDirs(want, s.Config.Dest, s.Config.Excluded, ig)
			continue
		}
		dirs := map[string]bool{".": true}
		for _, l := range s.Links {
			for d := path.Dir(l); d != "."; d = path.Dir(d) {
				dirs[d] = true
			}
		}
		for d := range dirs {
			p := filepath.Join(s.Config.Dest, filepath.FromSlash(d))
			if info, err := os.Lstat(p); err == nil && info.IsDir() {
				want[p] = true
			}
		}
	}
	for d := range rt.watched {
		if !want[d] {
			rt.fsw.Remove(d)
			delete(rt.watched, d)
		}
	}
	var add []string
	for d := range want {
		add = append(add, d) // Add is a no-op for directories already watched
	}
	sort.Strings(add)
	for _, d := range add {
		// A directory deleted and recreated needs a fresh descriptor; the
		// watcher dropped the old one, so Add is cheap to repeat.
		if err := rt.fsw.Add(d); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				rt.log.Printf("watch %s: %v", d, err)
			}
			continue
		}
		rt.watched[d] = true
	}
}

// addDirs adds base and every directory below it, skipping excluded and
// ignored ones. Symlinked directories are not followed.
func addDirs(want map[string]bool, base string, excluded func(string) bool, ig workspace.Ignored) {
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		rel = filepath.ToSlash(rel)
		if rel != "." && (excluded(rel) || p == ig.Root || p == ig.StateDir) {
			return filepath.SkipDir
		}
		want[p] = true
		return nil
	})
}

// touched is every path whose manifest entry a cycle re-decides: what the
// after-snapshot saw, what was acted on (a deleted link and set file leave
// nothing to see), and the manifest paths the cycle covered.
func touched(after plan.SetState, actions []plan.Action, links, rels []string, full bool) []string {
	seen := map[string]bool{}
	for rel := range after.Set {
		seen[rel] = true
	}
	for rel := range after.DestTree {
		seen[rel] = true
	}
	for _, a := range actions {
		seen[a.Path] = true
	}
	for _, l := range links {
		if full || coveredBy(l, rels) {
			seen[l] = true
		}
	}
	delete(seen, ".")
	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	return out
}

func coveredBy(p string, rels []string) bool {
	for _, r := range rels {
		if r == "." || p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// describe is the log line for an applied action. Directory creation is
// part of linking and is not logged.
func describe(a plan.Action) string {
	switch a.Op {
	case plan.OpLink:
		return "linked " + a.Path
	case plan.OpReplaceWithLink:
		return "relinked " + a.Path
	case plan.OpAdopt:
		return "adopted " + a.Path
	case plan.OpRemoveLink:
		return "removed link " + a.Path + " (set file deleted)"
	case plan.OpDeleteSetFile:
		return "deleted " + a.Path + " from the set (link removed from dest)"
	case plan.OpMkDir:
		return ""
	}
	return fmt.Sprintf("%s %s", a.Op, a.Path)
}
