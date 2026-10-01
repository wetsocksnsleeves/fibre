package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
	"github.com/wetsocksnsleeves/fibre/internal/plan"
	"github.com/wetsocksnsleeves/fibre/internal/state"
)

func newStatusCmd() *cobra.Command {
	var verbose, all bool
	cmd := &cobra.Command{
		Use:   "status [set]",
		Short: "Show each set's links and the paths that need attention",
		Long: `Show each set's links and the paths that need attention.

status works inside the dotfiles root and inside any linked set's dest. In a
dest it shows only that set; pass --all to show every set.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			only := ""
			if len(args) == 1 {
				if all {
					return errors.New("--all cannot be combined with a set name")
				}
				only = args[0]
			}
			return runStatus(cmd.OutOrStdout(), only, all, verbose, useColor(cmd.OutOrStdout()))
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "list every link")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "show every set, even inside a set's dest")
	return cmd
}

// setStatus is one set's block of status output.
type setStatus struct {
	name, dest string
	summary    string
	groups     []group
}

// group is a heading and the paths listed under it.
type group struct {
	heading string
	tone    tone
	items   []string
}

func runStatus(w io.Writer, only string, all, verbose, color bool) error {
	if only != "" {
		if err := validateSetName(only); err != nil {
			return err
		}
	}
	stateDir, err := state.DefaultDir()
	if err != nil {
		return err
	}
	st, err := state.Load(stateDir)
	if err != nil {
		return err
	}
	rootDir, inSet, err := locate(st)
	if err != nil {
		return err
	}
	if only == "" && !all {
		only = inSet
	}
	if st.Root != "" && st.Root != rootDir && len(st.Linked) > 0 {
		return fmt.Errorf("this machine's sets are linked from %s, not this root", displayPath(st.Root))
	}

	names, err := rootSets(rootDir)
	if err != nil {
		return err
	}
	for name := range st.Linked {
		if !contains(names, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if only != "" && !contains(names, only) {
		return fmt.Errorf("no set %q in %s", only, displayPath(rootDir))
	}

	env, err := config.OSEnv()
	if err != nil {
		return err
	}

	// Reconcile every linked set together, since which set owns an
	// untracked file depends on all of them.
	statuses := map[string]*setStatus{}
	var states []plan.SetState
	for _, name := range names {
		setDir := filepath.Join(rootDir, name)
		linked := st.Linked[name]
		cfg, cfgErr := config.Load(setDir, env)
		ss := &setStatus{name: name}
		statuses[name] = ss
		switch {
		case linked == nil && cfgErr != nil:
			ss.summary = "error: " + cfgErr.Error()
		case linked == nil:
			ss.dest, ss.summary = displayPath(cfg.Dest), fmt.Sprintf("not linked (run `fibre link %s`)", name)
		case cfgErr != nil:
			ss.dest, ss.summary = displayPath(linked.Dest), "error: "+cfgErr.Error()
		case cfg.Dest != linked.Dest:
			ss.dest = displayPath(linked.Dest)
			ss.summary = fmt.Sprintf("error: fibre.yaml now says dest %s; unlink and link again", displayPath(cfg.Dest))
		default:
			ss.dest = displayPath(linked.Dest)
			s, err := reconcileSnapshot(name, setDir, cfg, linked.Links, rootDir, stateDir)
			if err != nil {
				ss.summary = "error: " + err.Error()
				continue
			}
			states = append(states, s)
		}
	}

	for i, p := range plan.Reconcile(states) {
		s := states[i]
		ss := statuses[p.Name]
		linked := plan.Linked(plan.LinkInput{SetDir: s.SetDir, Dest: s.Dest, Set: s.Set, DestTree: s.DestTree, Excluded: s.Excluded})
		ss.summary, ss.groups = describePlan(s, p.Actions, linked, verbose)
	}

	var shown []*setStatus
	for _, name := range names {
		if only == "" || name == only {
			shown = append(shown, statuses[name])
		}
	}
	printStatus(w, shown, color)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Watcher is not running, so nothing is synced automatically.")
	return nil
}

// reconcileSnapshot reads a linked set and its dest for Reconcile. A strict
// set only needs its own paths in dest; any other set also needs every file
// in dest, to find untracked ones. Only paths the set maps to are hashed.
func reconcileSnapshot(name, setDir string, cfg *config.Config, links []string, rootDir, stateDir string) (plan.SetState, error) {
	set, err := fsnap.Scan(setDir, cfg.Excluded)
	if err != nil {
		return plan.SetState{}, err
	}
	paths := sortedKeys(set)
	for _, l := range links {
		if _, ok := set[l]; !ok {
			paths = append(paths, l)
		}
	}
	destTree := fsnap.Tree{}
	if !cfg.Strict {
		if _, err := os.Lstat(cfg.Dest); err == nil {
			destTree, err = fsnap.ScanKinds(cfg.Dest, func(rel string) bool {
				p := filepath.Join(cfg.Dest, filepath.FromSlash(rel))
				return cfg.Excluded(rel) || p == rootDir || p == stateDir
			})
			if err != nil {
				return plan.SetState{}, err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return plan.SetState{}, err
		}
	}
	hashed, err := fsnap.Lookup(cfg.Dest, paths)
	if err != nil {
		return plan.SetState{}, err
	}
	for rel, e := range hashed {
		destTree[rel] = e
	}
	return plan.SetState{
		Name: name, SetDir: setDir, Dest: cfg.Dest,
		Set: set, DestTree: destTree, Excluded: cfg.Excluded, Strict: cfg.Strict, Links: links,
	}, nil
}

// statusKind is one heading in a set's block. Kinds are listed in this
// order, most urgent first.
type statusKind int

const (
	kindConflict statusKind = iota
	kindUnlinked
	kindModified
	kindDeleted
	kindDangling
	kindUntracked
	kindTied
	kindLinked
)

func (k statusKind) heading(set string) string {
	switch k {
	case kindConflict:
		return fmt.Sprintf("Conflicts (resolve with `fibre link %s` and --adopt, --force or --skip):", set)
	case kindUnlinked:
		return fmt.Sprintf("Not linked (new in the set; the watcher or `fibre link %s` will link them):", set)
	case kindModified:
		return "Modified in dest (a real file replaced the link; the watcher will copy it into the set):"
	case kindDeleted:
		return "Deleted from dest (the watcher will delete the set's copy):"
	case kindDangling:
		return "Deleted from the set (the watcher will remove the link):"
	case kindUntracked:
		return "Untracked (new in dest; the watcher will adopt them):"
	case kindTied:
		return "Untracked, claimed by more than one set (exclude it from all but one):"
	case kindLinked:
		return "Linked:"
	}
	return ""
}

func (k statusKind) tone() tone {
	switch k {
	case kindConflict:
		return toneBad
	case kindLinked:
		return toneGood
	}
	return toneWarn
}

// statusKindOf says which heading an action goes under. Directory creation
// is part of linking and is not listed.
func statusKindOf(s plan.SetState, a plan.Action) (statusKind, bool) {
	switch a.Op {
	case plan.OpConflict:
		return kindConflict, true
	case plan.OpLink, plan.OpReplaceWithLink:
		return kindUnlinked, true
	case plan.OpAdopt:
		if _, inSet := s.Set[a.Path]; inSet {
			return kindModified, true
		}
		return kindUntracked, true
	case plan.OpDeleteSetFile:
		return kindDeleted, true
	case plan.OpRemoveLink:
		return kindDangling, true
	case plan.OpUntracked:
		return kindTied, true
	}
	return 0, false
}

func describePlan(s plan.SetState, actions []plan.Action, linked []string, verbose bool) (string, []group) {
	items := map[statusKind][]string{}
	for _, a := range actions {
		k, ok := statusKindOf(s, a)
		if !ok {
			continue
		}
		item := a.Path
		switch k {
		case kindConflict:
			item += "  (" + describeConflict(a) + ")"
		case kindTied:
			item += "  (" + strings.Join(a.Tied, ", ") + ")"
		}
		items[k] = append(items[k], item)
	}
	if verbose {
		items[kindLinked] = linked
	}

	var groups []group
	problems := 0
	for k := kindConflict; k <= kindLinked; k++ {
		if len(items[k]) == 0 {
			continue
		}
		groups = append(groups, group{heading: k.heading(s.Name), tone: k.tone(), items: items[k]})
		if k != kindLinked {
			problems++
		}
	}
	summary := fmt.Sprintf("%d linked", len(linked))
	if problems == 0 {
		summary += ", in sync"
	}
	return summary, groups
}

func printStatus(w io.Writer, sets []*setStatus, color bool) {
	for i, s := range sets {
		if i > 0 {
			fmt.Fprintln(w)
		}
		head := paint(color, toneBold, s.name)
		if s.dest != "" {
			head += " → " + s.dest
		}
		fmt.Fprintf(w, "%s   %s\n", head, s.summary)
		for _, g := range s.groups {
			fmt.Fprintf(w, "  %s\n", paint(color, g.tone, g.heading))
			for _, item := range g.items {
				fmt.Fprintf(w, "    %s\n", item)
			}
		}
	}
}

// rootSets returns the names of the directories in rootDir that hold a
// fibre.yaml.
func rootSets(rootDir string) ([]string, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(rootDir, e.Name(), config.FileName)); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
