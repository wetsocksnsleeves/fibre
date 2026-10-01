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
	"time"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/exec"
	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
	"github.com/wetsocksnsleeves/fibre/internal/plan"
	"github.com/wetsocksnsleeves/fibre/internal/root"
	"github.com/wetsocksnsleeves/fibre/internal/state"
)

func newLinkCmd() *cobra.Command {
	var adopt, force, skip bool
	cmd := &cobra.Command{
		Use:   "link <set>",
		Short: "Symlink every non-excluded file in a set into its dest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := plan.Halt
			switch {
			case adopt:
				r = plan.Adopt
			case force:
				r = plan.Force
			case skip:
				r = plan.Skip
			}
			return runLink(cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], r)
		},
	}
	cmd.Flags().BoolVar(&adopt, "adopt", false, "move conflicting dest files into the set, then link")
	cmd.Flags().BoolVar(&force, "force", false, "back up conflicting dest paths, then link the set's version")
	cmd.Flags().BoolVar(&skip, "skip", false, "leave conflicting paths alone and link everything else")
	cmd.MarkFlagsMutuallyExclusive("adopt", "force", "skip")
	return cmd
}

func runLink(stdout, stderr io.Writer, name string, r plan.Resolution) error {
	rootDir, setDir, cfg, err := loadSet(name)
	if err != nil {
		return err
	}

	stateDir, err := state.DefaultDir()
	if err != nil {
		return err
	}
	lock, err := state.Acquire(stateDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	st, err := state.Load(stateDir)
	if err != nil {
		return err
	}
	if st.Root != "" && st.Root != rootDir && len(st.Linked) > 0 {
		return fmt.Errorf("this machine has sets linked from %s; fibre manages one root per machine", st.Root)
	}
	prev := st.Linked[name]
	if prev != nil && prev.Dest != cfg.Dest {
		return fmt.Errorf("%s is linked into %s but its fibre.yaml now says %s; unlink it first", name, displayPath(prev.Dest), displayPath(cfg.Dest))
	}

	in, err := snapshot(setDir, cfg)
	if err != nil {
		return err
	}

	var others []plan.Claim
	for other, s := range st.Linked {
		if other != name {
			others = append(others, plan.Claim{Set: other, Dest: s.Dest, Links: s.Links})
		}
	}
	if sc := plan.SetConflicts(in, others); len(sc) > 0 {
		fmt.Fprintf(stderr, "%s shares paths with other linked sets; nothing was changed:\n", name)
		for _, c := range sc {
			fmt.Fprintf(stderr, "  %s  (linked by %s)\n", c.Path, c.OtherSet)
		}
		return fmt.Errorf("%s conflict with other sets", plural(len(sc), "path"))
	}

	actions := plan.Resolve(plan.Link(in), r)
	res, err := exec.Apply(setDir, cfg.Dest, actions)
	var ce *exec.ConflictError
	if errors.As(err, &ce) {
		printConflicts(stderr, name, ce.Conflicts, r)
		return fmt.Errorf("%s in %s; nothing was changed", plural(len(ce.Conflicts), "conflict"), name)
	}
	if err != nil {
		return err
	}

	after, err := fsnap.Lookup(cfg.Dest, sortedKeys(in.Set))
	if err != nil {
		return err
	}
	in.DestTree = after
	links := plan.Linked(in)

	linkedAt := time.Now().UTC().Truncate(time.Second)
	if prev != nil {
		linkedAt = prev.LinkedAt
	}
	st.Root = rootDir
	st.Linked[name] = &state.Set{Dest: cfg.Dest, LinkedAt: linkedAt, Links: links}
	if err := state.Save(stateDir, st); err != nil {
		return err
	}

	printLinkSummary(stdout, name, cfg.Dest, actions, res, len(links))
	return nil
}

// loadSet finds the root from the working directory and loads the named set.
func loadSet(name string) (rootDir, setDir string, cfg *config.Config, err error) {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return "", "", nil, fmt.Errorf("invalid set name %q: use the name of a directory in the root", name)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", "", nil, err
	}
	rootDir, err = root.Find(wd)
	if err != nil {
		return "", "", nil, err
	}
	// Symlink targets and state use the resolved path, so the same root
	// reached through a symlinked path is recognized as the same root.
	if rootDir, err = filepath.EvalSymlinks(rootDir); err != nil {
		return "", "", nil, err
	}
	setDir = filepath.Join(rootDir, name)
	env, err := config.OSEnv()
	if err != nil {
		return "", "", nil, err
	}
	cfg, err = config.Load(setDir, env)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", nil, fmt.Errorf("no set %q in %s (expected %s)", name, rootDir, filepath.Join(setDir, config.FileName))
	}
	if err != nil {
		return "", "", nil, err
	}
	return rootDir, setDir, cfg, nil
}

// snapshot reads the set and the dest paths the set maps to.
func snapshot(setDir string, cfg *config.Config) (plan.LinkInput, error) {
	set, err := fsnap.Scan(setDir, cfg.Excluded)
	if err != nil {
		return plan.LinkInput{}, err
	}
	destTree, err := fsnap.Lookup(cfg.Dest, sortedKeys(set))
	if err != nil {
		return plan.LinkInput{}, err
	}
	return plan.LinkInput{SetDir: setDir, Dest: cfg.Dest, Set: set, DestTree: destTree, Excluded: cfg.Excluded}, nil
}

func printConflicts(w io.Writer, name string, conflicts []plan.Action, r plan.Resolution) {
	fmt.Fprintf(w, "%s has conflicts; nothing was changed:\n", name)
	for _, c := range conflicts {
		fmt.Fprintf(w, "  CONFLICT  %s  (%s)\n", c.Path, describeConflict(c))
	}
	if r == plan.Adopt {
		fmt.Fprintln(w, "--adopt only adopts regular files; use --force or --skip for these")
	} else {
		fmt.Fprintln(w, "resolve with --adopt, --force or --skip")
	}
}

func describeConflict(c plan.Action) string {
	if c.Want == fsnap.Dir {
		return fmt.Sprintf("set has a directory, dest has a %s", c.Found.Kind)
	}
	switch c.Found.Kind {
	case fsnap.File:
		return "dest file differs from the set's"
	case fsnap.Symlink:
		return "dest is a symlink to " + c.Found.Target
	}
	return fmt.Sprintf("dest has a %s", c.Found.Kind)
}

func printLinkSummary(w io.Writer, name, dest string, actions []plan.Action, res exec.Result, total int) {
	counts := map[plan.Op]int{}
	for _, a := range actions {
		counts[a.Op]++
	}
	for _, b := range res.Backups {
		fmt.Fprintf(w, "backed up %s to %s\n", b.Path, displayPath(b.To))
	}
	var parts []string
	for _, c := range []struct {
		op    plan.Op
		label string
	}{
		{plan.OpLink, "new"},
		{plan.OpReplaceWithLink, "replaced"},
		{plan.OpAdopt, "adopted"},
		{plan.OpBackup, "backed up"},
	} {
		if n := counts[c.op]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, c.label))
		}
	}
	if len(parts) == 0 {
		fmt.Fprintf(w, "%s is up to date in %s (%s)\n", name, displayPath(dest), plural(total, "link"))
		return
	}
	fmt.Fprintf(w, "linked %s into %s: %s (%s)\n", name, displayPath(dest), strings.Join(parts, ", "), plural(total, "link"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// displayPath shortens a path under the home directory to ~/...
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return p
}

func sortedKeys(t fsnap.Tree) []string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
