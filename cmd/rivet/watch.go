package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/rivet/internal/config"
	"github.com/wetsocksnsleeves/rivet/internal/launchd"
	"github.com/wetsocksnsleeves/rivet/internal/state"
	"github.com/wetsocksnsleeves/rivet/internal/watch"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run and manage the watcher that keeps linked sets and their dests in sync",
	}
	var debounce time.Duration
	run := &cobra.Command{
		Use:   "run",
		Short: "Run the watcher in the foreground until interrupted",
		Long: `Run the watcher in the foreground until interrupted.

On start it reconciles every linked set, then watches each set and its dest:
new files in dest are adopted into the set, real files that replaced links
(atomic saves) are adopted and relinked, new set files are linked, and
deletions propagate both ways. Conflicts are left for ` + "`rivet status`" + `.

It reads linked sets from state, so it runs from any directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := state.DefaultDir()
			if err != nil {
				return err
			}
			env, err := config.OSEnv()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return (&watch.Runtime{
				StateDir: stateDir,
				Window:   debounce,
				Env:      env,
				Log:      cmd.ErrOrStderr(),
			}).Run(ctx)
		},
	}
	run.Flags().DurationVar(&debounce, "debounce", 500*time.Millisecond, "how long a path must be quiet before it is acted on")

	install := &cobra.Command{
		Use:   "install",
		Short: "Install the watcher as a launchd agent that starts at login, and start it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			if err := m.Install(); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Installed %s; the watcher is running and starts at login.\n", displayPath(m.PlistPath))
			fmt.Fprintf(w, "It runs %s", displayPath(m.Agent.Program))
			if d, ok := m.Agent.Env["XDG_STATE_HOME"]; ok {
				fmt.Fprintf(w, " with XDG_STATE_HOME=%s", displayPath(d))
			}
			fmt.Fprintf(w, " and logs to %s.\n", displayPath(m.Agent.LogPath))
			return nil
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the watcher agent and remove it from launchd",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			if err := m.Uninstall(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Uninstalled the watcher agent.")
			return nil
		},
	}
	start := &cobra.Command{
		Use:   "start",
		Short: "Start the installed watcher agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			if err := m.Start(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Started the watcher.")
			return nil
		},
	}
	stop := &cobra.Command{
		Use:   "stop",
		Short: "Stop the watcher agent until the next login or `rivet watch start`",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			if !m.Status().Loaded {
				fmt.Fprintln(cmd.OutOrStdout(), "The watcher agent is not running.")
				return nil
			}
			if err := m.Stop(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Stopped the watcher. It starts again at login, or with `rivet watch start`.")
			return nil
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show whether the watcher agent is installed and running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := newManager()
			if err != nil {
				return err
			}
			stateDir, err := state.DefaultDir()
			if err != nil {
				return err
			}
			printWatchStatus(cmd.OutOrStdout(), m, stateDir)
			return nil
		},
	}
	cmd.AddCommand(run, install, uninstall, start, stop, status)
	return cmd
}

// newManager builds the launchd manager for this executable. Tests replace
// it to avoid calling launchctl.
var newManager = func() (*launchd.Manager, error) {
	program, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// program is not resolved through symlinks: a Homebrew install is a
	// symlink into a versioned Cellar directory that upgrades remove.
	env := map[string]string{}
	if d, ok := os.LookupEnv("XDG_STATE_HOME"); ok && d != "" {
		env["XDG_STATE_HOME"] = d
	}
	return launchd.NewManager(program, env)
}

func printWatchStatus(w io.Writer, m *launchd.Manager, stateDir string) {
	s := m.Status()
	agent := "not installed (run `rivet watch install`)"
	if s.Installed {
		agent = "installed (" + displayPath(m.PlistPath) + ")"
	}
	launchdState := "not loaded"
	switch {
	case s.Running:
		launchdState = fmt.Sprintf("running (pid %d)", s.PID)
	case s.Loaded:
		launchdState = "loaded, not running"
	}
	fmt.Fprintf(w, "agent:    %s\n", agent)
	fmt.Fprintf(w, "launchd:  %s\n", launchdState)
	fmt.Fprintf(w, "watcher:  %s\n", strings.TrimSuffix(watcherLine(stateDir), "."))
	fmt.Fprintf(w, "log:      %s\n", displayPath(m.Agent.LogPath))
}

// watcherLine is the last line of status.
func watcherLine(stateDir string) string {
	pid, running, err := state.WatcherPID(stateDir)
	switch {
	case err != nil:
		return "Watcher state unknown: " + err.Error()
	case running:
		return "Watcher is running (pid " + strconv.Itoa(pid) + ")."
	}
	return "Watcher is not running, so nothing is synced automatically. Start it with `rivet watch install` (or `rivet watch run` in the foreground)."
}
