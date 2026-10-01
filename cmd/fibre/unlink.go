package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/fibre/internal/exec"
	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
	"github.com/wetsocksnsleeves/fibre/internal/plan"
	"github.com/wetsocksnsleeves/fibre/internal/state"
)

func newUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <set>",
		Short: "Replace a set's links in its dest with copies of its files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUnlink(cmd.OutOrStdout(), args[0])
		},
	}
}

func runUnlink(stdout io.Writer, name string) error {
	rootDir, err := findRoot()
	if err != nil {
		return err
	}
	if err := validateSetName(name); err != nil {
		return err
	}
	return withState(func(stateDir string, st *state.State) error {
		if st.Root != "" && st.Root != rootDir {
			return fmt.Errorf("this machine's sets are linked from %s, not this root", displayPath(st.Root))
		}
		linked := st.Linked[name]
		if linked == nil {
			return fmt.Errorf("%s is not linked on this machine", name)
		}

		// State, not fibre.yaml, says where the links are: the config may
		// have changed, or the set may have been deleted, since linking.
		setDir := filepath.Join(rootDir, name)
		set, err := fsnap.Lookup(setDir, linked.Links)
		if err != nil {
			return err
		}
		destTree, err := fsnap.Lookup(linked.Dest, linked.Links)
		if err != nil {
			return err
		}
		actions, skipped := plan.Unlink(plan.UnlinkInput{
			SetDir: setDir, Dest: linked.Dest, Links: linked.Links, Set: set, DestTree: destTree,
		})
		if _, err := exec.Apply(setDir, linked.Dest, actions); err != nil {
			return fmt.Errorf("unlink stopped partway; %s is still recorded as linked, rerun to finish: %w", name, err)
		}

		delete(st.Linked, name)
		if len(st.Linked) == 0 {
			st.Root = ""
		}
		if err := state.Save(stateDir, st); err != nil {
			return err
		}

		for _, s := range skipped {
			fmt.Fprintf(stdout, "  SKIPPED  %s  (now a %s, not a link to the set; left in place)\n", s.Path, s.Found.Kind)
		}
		counts := map[plan.Op]int{}
		for _, a := range actions {
			counts[a.Op]++
		}
		msg := fmt.Sprintf("unlinked %s from %s: %s copied", name, displayPath(linked.Dest), plural(counts[plan.OpReplaceWithCopy], "file"))
		if n := counts[plan.OpRemoveLink]; n > 0 {
			msg += fmt.Sprintf(", %s to deleted set files removed", plural(n, "link"))
		}
		fmt.Fprintln(stdout, msg)
		return nil
	})
}
