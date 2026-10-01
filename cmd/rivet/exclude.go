package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/exec"
	"github.com/wetsocksnsleeves/rivet/internal/fsnap"
	"github.com/wetsocksnsleeves/rivet/internal/plan"
	"github.com/wetsocksnsleeves/rivet/internal/root"
	"github.com/wetsocksnsleeves/rivet/internal/state"
)

func newExcludeCmd() *cobra.Command {
	var setName string
	cmd := &cobra.Command{
		Use:   "exclude <path>[,<path>...]",
		Short: "Add paths to the current set's exclude list",
		Long: `Add paths or globs to a set's exclude list in its rivet.yaml.

Run exclude inside a set, or inside a linked set's dest. Paths are relative
to the working directory and are written relative to the set. Separate
several paths with commas or spaces.

A path that is already linked is replaced with a copy of its file, so it
keeps its contents but no longer syncs. The set's copy stays in the root.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExclude(cmd.OutOrStdout(), setName, args)
		},
	}
	cmd.Flags().StringVarP(&setName, "set", "s", "", "the set to exclude from, when not inside one")
	return cmd
}

func runExclude(stdout io.Writer, setName string, args []string) error {
	if setName != "" {
		if err := validateSetName(setName); err != nil {
			return err
		}
	}
	var paths []string
	for _, a := range args {
		for _, p := range strings.Split(a, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		return errors.New("no paths to exclude")
	}

	return withState(func(stateDir string, st *state.State) error {
		rootDir, name, base, err := excludeTarget(st, setName)
		if err != nil {
			return err
		}
		setDir := filepath.Join(rootDir, name)
		linked := st.Linked[name]
		if st.Root != rootDir {
			linked = nil
		}

		var patterns []string
		for _, p := range paths {
			pat, err := excludePattern(p, base, setDir, linked)
			if err != nil {
				return err
			}
			patterns = append(patterns, pat)
		}

		cfgPath := filepath.Join(setDir, config.FileName)
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return err
		}
		updated, added, err := config.AddExcludes(data, patterns)
		if err != nil {
			return fmt.Errorf("%s: %w", displayPath(cfgPath), err)
		}
		env, err := config.OSEnv()
		if err != nil {
			return err
		}
		cfg, err := config.Parse(updated, env)
		if err != nil {
			return fmt.Errorf("%s: %w", displayPath(cfgPath), err)
		}
		if len(added) > 0 {
			if err := os.WriteFile(cfgPath, updated, 0o644); err != nil {
				return err
			}
		}
		for _, p := range patterns {
			if !contains(added, p) {
				fmt.Fprintf(stdout, "%s is already excluded\n", p)
			}
		}
		if len(added) > 0 {
			fmt.Fprintf(stdout, "excluded %s from %s\n", strings.Join(added, ", "), name)
		}

		// The config is written first, so a watcher that runs after the lock
		// is released already ignores the copies made below. Unlinking runs
		// even when nothing was added, to finish a run that stopped partway.
		if linked == nil {
			return nil
		}
		copied, err := unlinkExcluded(stateDir, st, name, setDir, linked, cfg)
		if err != nil {
			return err
		}
		if copied > 0 {
			fmt.Fprintf(stdout, "replaced %s in %s with copies; the set's copies are still in %s\n",
				plural(copied, "link"), displayPath(linked.Dest), displayPath(setDir))
		}
		return nil
	})
}

// excludeTarget finds the set to exclude from and base, the working
// directory relative to that set (or its dest), or "." when the working
// directory is in neither.
func excludeTarget(st *state.State, setName string) (rootDir, name, base string, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", "", err
	}
	rootDir, err = findRoot()
	switch {
	case err == nil:
		// Inside the root: the set is the top-level directory holding wd.
		rel := "."
		if r, err := filepath.EvalSymlinks(wd); err == nil {
			rel, _ = filepath.Rel(rootDir, r)
		}
		rel = filepath.ToSlash(rel)
		first, rest, _ := strings.Cut(rel, "/")
		if rest == "" {
			rest = "."
		}
		inSet := ""
		if rel != "." {
			inSet = first
		}
		switch {
		case setName == "" && inSet == "":
			return "", "", "", errors.New("not inside a set; cd into one or pass --set")
		case setName == "":
			name, base = inSet, rest
		case setName == inSet:
			name, base = setName, rest
		default:
			name, base = setName, "."
		}
	case errors.Is(err, root.ErrNotFound) && st.Root != "":
		var inSet string
		if rootDir, inSet, err = locate(st); err != nil {
			return "", "", "", err
		}
		name = setName
		if name == "" {
			name = inSet
		}
		if name == "" {
			return "", "", "", errors.New("not inside a set or a single set's dest; pass --set")
		}
		base = "."
		if s := st.Linked[name]; s != nil {
			if rel, ok := relWithin(wd, s.Dest); ok {
				base = rel
			}
		}
	default:
		return "", "", "", err
	}
	if _, err := os.Stat(filepath.Join(rootDir, name, config.FileName)); err != nil {
		return "", "", "", fmt.Errorf("no set %q in %s", name, displayPath(rootDir))
	}
	return rootDir, name, base, nil
}

// excludePattern turns p, as typed in base, into a pattern relative to the
// set. An absolute p must be inside the set or its dest.
func excludePattern(p, base, setDir string, linked *state.Set) (string, error) {
	if filepath.IsAbs(p) {
		rel, ok := relWithin(p, setDir)
		if !ok && linked != nil {
			rel, ok = relWithin(p, linked.Dest)
		}
		if !ok {
			return "", fmt.Errorf("%s is not inside the set or its dest", p)
		}
		p, base = rel, "."
	}
	pat := path.Join(base, filepath.ToSlash(p))
	if pat == "." || pat == ".." || strings.HasPrefix(pat, "../") {
		return "", fmt.Errorf("%s is not inside the set", p)
	}
	if pat == config.FileName {
		return "", fmt.Errorf("%s is always excluded", config.FileName)
	}
	return pat, nil
}

// relWithin returns p relative to dir, slash-separated, if p is dir or
// inside it. Both are also tried with symlinks resolved.
func relWithin(p, dir string) (string, bool) {
	for _, pf := range pathForms(p) {
		for _, df := range pathForms(dir) {
			if within(pf, df) {
				rel, _ := filepath.Rel(df, pf)
				return filepath.ToSlash(rel), true
			}
		}
	}
	return "", false
}

// unlinkExcluded replaces the set's links to paths cfg now excludes with
// copies, and drops those paths from the manifest. It returns how many links
// were replaced. The caller holds the lock.
func unlinkExcluded(stateDir string, st *state.State, name, setDir string, linked *state.Set, cfg *config.Config) (int, error) {
	var excluded, kept []string
	for _, l := range linked.Links {
		if cfg.Excluded(l) {
			excluded = append(excluded, l)
		} else {
			kept = append(kept, l)
		}
	}
	if len(excluded) == 0 {
		return 0, nil
	}
	set, err := fsnap.Lookup(setDir, excluded)
	if err != nil {
		return 0, err
	}
	destTree, err := fsnap.Lookup(linked.Dest, excluded)
	if err != nil {
		return 0, err
	}
	// Paths the user already replaced are skipped and left alone; they are
	// excluded now, so they leave the manifest too.
	actions, _ := plan.Unlink(plan.UnlinkInput{
		SetDir: setDir, Dest: linked.Dest, Links: excluded, Set: set, DestTree: destTree,
	})
	if _, err := exec.Apply(setDir, linked.Dest, actions); err != nil {
		return 0, fmt.Errorf("replacing excluded links stopped partway; rerun to finish: %w", err)
	}
	linked.Links = kept
	if err := state.Save(stateDir, st); err != nil {
		return 0, err
	}
	copied := 0
	for _, a := range actions {
		if a.Op == plan.OpReplaceWithCopy {
			copied++
		}
	}
	return copied, nil
}
