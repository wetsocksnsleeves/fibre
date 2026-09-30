package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/exec"
	"github.com/wetsocksnsleeves/fibre/internal/fsnap"
	"github.com/wetsocksnsleeves/fibre/internal/plan"
	"github.com/wetsocksnsleeves/fibre/internal/root"
	"github.com/wetsocksnsleeves/fibre/internal/state"
)

func newInitCmd() *cobra.Command {
	var dest string
	var exclude []string
	var strict bool
	cmd := &cobra.Command{
		Use:   "init [set]",
		Short: "Make the current directory a dotfiles root, or create a set in the current root",
		Long: `With no set name, init makes the current directory a dotfiles root.

With a set name, init creates the set and its fibre.yaml in the current root.
If dest already exists, every non-excluded file in it is moved into the set
and linked back. Excluded files stay in dest.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				for _, f := range []string{"dest", "exclude", "strict"} {
					if cmd.Flags().Changed(f) {
						return fmt.Errorf("--%s needs a set name: fibre init <set> --dest <path>", f)
					}
				}
				return initRoot(cmd.OutOrStdout())
			}
			if dest == "" {
				return errors.New("--dest is required when creating a set")
			}
			return initSet(cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], dest, exclude, strict)
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "directory the set is linked into (~ and $VARS are kept as written in fibre.yaml)")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "comma-separated globs to exclude, relative to the set")
	cmd.Flags().BoolVar(&strict, "strict", false, "make the set link-only: never import or adopt files from dest")
	return cmd
}

func initRoot(w io.Writer) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	created, err := root.Init(dir)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(w, "Initialized fibre root in %s\n", dir)
	} else {
		fmt.Fprintf(w, "%s is already a fibre root\n", dir)
	}
	return nil
}

// setFile is the fibre.yaml init writes.
type setFile struct {
	Dest    string   `yaml:"dest"`
	Exclude []string `yaml:"exclude,omitempty"`
	Strict  bool     `yaml:"strict,omitempty"`
}

func initSet(stdout, stderr io.Writer, name, destArg string, exclude []string, strict bool) error {
	rootDir, err := findRoot()
	if err != nil {
		return err
	}
	if err := validateSetName(name); err != nil {
		return err
	}
	setDir := filepath.Join(rootDir, name)
	if err := checkNewSetDir(setDir); err != nil {
		return err
	}

	env, err := config.OSEnv()
	if err != nil {
		return err
	}
	destValue, err := configDest(destArg, env.Home)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(setFile{Dest: destValue, Exclude: exclude, Strict: strict})
	if err != nil {
		return err
	}
	cfg, err := config.Parse(data, env)
	if err != nil {
		return err
	}
	if within(cfg.Dest, rootDir) {
		return fmt.Errorf("dest %s is inside the dotfiles root %s", displayPath(cfg.Dest), displayPath(rootDir))
	}
	if !strict && within(env.Home, cfg.Dest) {
		return fmt.Errorf("dest %s contains your home directory, so importing it would move everything there into the set; pass --strict for a link-only set", displayPath(cfg.Dest))
	}

	info, err := os.Lstat(cfg.Dest)
	destExists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if destExists && !info.IsDir() {
		return fmt.Errorf("dest %s exists and is not a directory", displayPath(cfg.Dest))
	}

	if !destExists || strict {
		if err := writeSet(setDir, data); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "created set %s in %s\n", name, displayPath(setDir))
		if strict && destExists {
			fmt.Fprintf(stdout, "%s is strict, so nothing was imported from %s\n", name, displayPath(cfg.Dest))
		}
		fmt.Fprintf(stdout, "add files to the set, then run `fibre link %s`\n", name)
		return nil
	}

	return withState(func(stateDir string, st *state.State) error {
		if err := checkState(st, rootDir, name, cfg); err != nil {
			return err
		}
		// The root or fibre's state may sit inside dest (e.g. dest ~/.local
		// holds ~/.local/state/fibre); never import them.
		destTree, err := fsnap.Scan(cfg.Dest, func(rel string) bool {
			p := filepath.Join(cfg.Dest, filepath.FromSlash(rel))
			return cfg.Excluded(rel) || p == rootDir || p == stateDir
		})
		if err != nil {
			return err
		}
		actions, skipped := plan.Import(destTree, cfg.Excluded)

		if err := writeSet(setDir, data); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "created set %s in %s\n", name, displayPath(setDir))
		if _, err := exec.Apply(setDir, cfg.Dest, actions); err != nil {
			return fmt.Errorf("importing %s stopped partway; files already moved are in %s: %w", displayPath(cfg.Dest), displayPath(setDir), err)
		}
		out, err := linkSet(stderr, stateDir, st, rootDir, name, setDir, cfg, plan.Halt)
		if err != nil {
			return err
		}
		for _, s := range skipped {
			fmt.Fprintf(stdout, "  SKIPPED  %s  (%s; left in dest)\n", s.Path, describeSkipped(s))
		}
		fmt.Fprintf(stdout, "imported %s from %s (%s)\n", plural(len(actions), "file"), displayPath(cfg.Dest), plural(out.links, "link"))
		return nil
	})
}

// checkNewSetDir allows a missing or empty directory for a new set.
func checkNewSetDir(setDir string) error {
	entries, err := os.ReadDir(setDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(setDir, config.FileName)); err == nil {
		return fmt.Errorf("set %s already exists", filepath.Base(setDir))
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", displayPath(setDir))
	}
	return nil
}

func writeSet(setDir string, data []byte) error {
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(setDir, config.FileName), data, 0o644)
}

// configDest is the dest value written to fibre.yaml. A value the shell left
// unexpanded (~ or $VAR, because it was quoted) is kept as written. Anything
// else is made absolute, and a path under home is written with ~ so the set
// works on machines with a different home directory.
func configDest(arg, home string) (string, error) {
	if strings.HasPrefix(arg, "~") || strings.Contains(arg, "$") {
		return arg, nil
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	if abs == home {
		return "~", nil
	}
	if rest, ok := strings.CutPrefix(abs, home+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rest), nil
	}
	return abs, nil
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func describeSkipped(s plan.Skipped) string {
	if s.Found.Kind == fsnap.Symlink {
		return "symlink to " + s.Found.Target
	}
	return s.Found.Kind.String()
}
