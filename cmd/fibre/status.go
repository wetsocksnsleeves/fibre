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
			return runStatus(cmd.OutOrStdout(), only, all, verbose)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "list every link")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "show every set, even inside a set's dest")
	return cmd
}

// setStatus is one line of status output and the paths listed under it.
type setStatus struct {
	name, dest string
	summary    string
	lines      []string
}

func runStatus(w io.Writer, only string, all, verbose bool) error {
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
			ss.dest, ss.summary = displayPath(cfg.Dest), "not linked"
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
		ss.summary, ss.lines = describePlan(s, p.Actions, linked, verbose)
	}

	var shown []*setStatus
	for _, name := range names {
		if only == "" || name == only {
			shown = append(shown, statuses[name])
		}
	}
	printStatus(w, shown)
	fmt.Fprintln(w, "watcher: not running")
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

// statusKinds are the labels status uses, in summary order.
var statusKinds = []struct {
	label, singular, plural string
}{
	{"CONFLICT", "conflict", "conflicts"},
	{"UNLINKED", "unlinked", "unlinked"},
	{"MODIFIED", "modified", "modified"},
	{"DELETED", "deleted", "deleted"},
	{"DANGLING", "dangling", "dangling"},
	{"UNTRACKED", "untracked", "untracked"},
}

// statusLabel names what an action means for the user. Directory creation
// is part of linking and has no line of its own.
func statusLabel(s plan.SetState, a plan.Action) string {
	switch a.Op {
	case plan.OpConflict:
		return "CONFLICT"
	case plan.OpLink, plan.OpReplaceWithLink:
		return "UNLINKED"
	case plan.OpAdopt:
		if _, inSet := s.Set[a.Path]; inSet {
			return "MODIFIED"
		}
		return "UNTRACKED"
	case plan.OpDeleteSetFile:
		return "DELETED"
	case plan.OpRemoveLink:
		return "DANGLING"
	case plan.OpUntracked:
		return "UNTRACKED"
	}
	return ""
}

func describePlan(s plan.SetState, actions []plan.Action, linked []string, verbose bool) (string, []string) {
	type line struct{ path, text string }
	var lines []line
	counts := map[string]int{}
	for _, a := range actions {
		label := statusLabel(s, a)
		if label == "" {
			continue
		}
		counts[label]++
		text := fmt.Sprintf("%-10s %s", label, a.Path)
		switch label {
		case "CONFLICT":
			text += "  (" + describeConflict(a) + ")"
		case "UNLINKED":
			text += "  (in the set, not linked yet)"
		case "MODIFIED":
			text += "  (link replaced by a real file)"
		case "DELETED":
			text += "  (link removed from dest)"
		case "DANGLING":
			text += "  (set file deleted)"
		case "UNTRACKED":
			if a.Op == plan.OpUntracked {
				text += "  (claimed by " + strings.Join(a.Tied, " and ") + ")"
			}
		}
		lines = append(lines, line{a.Path, text})
	}
	if verbose {
		for _, p := range linked {
			lines = append(lines, line{p, fmt.Sprintf("%-10s %s", "LINKED", p)})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].path < lines[j].path })

	parts := []string{fmt.Sprintf("%d linked", len(linked))}
	for _, k := range statusKinds {
		switch n := counts[k.label]; n {
		case 0:
		case 1:
			parts = append(parts, "1 "+k.singular)
		default:
			parts = append(parts, fmt.Sprintf("%d %s", n, k.plural))
		}
	}
	if len(parts) == 1 {
		parts = append(parts, "ok")
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return strings.Join(parts, "  "), out
}

func printStatus(w io.Writer, sets []*setStatus) {
	nameWidth, destWidth := 0, 0
	for _, s := range sets {
		nameWidth = max(nameWidth, len(s.name))
		destWidth = max(destWidth, len(s.dest))
	}
	for _, s := range sets {
		fmt.Fprintf(w, "%-*s  %-*s  %s\n", nameWidth, s.name, destWidth, s.dest, s.summary)
		for _, l := range s.lines {
			fmt.Fprintf(w, "  %s\n", l)
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
