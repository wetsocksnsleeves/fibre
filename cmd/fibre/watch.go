package main

import (
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/fibre/internal/config"
	"github.com/wetsocksnsleeves/fibre/internal/state"
	"github.com/wetsocksnsleeves/fibre/internal/watch"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run the watcher that keeps linked sets and their dests in sync",
	}
	var debounce time.Duration
	run := &cobra.Command{
		Use:   "run",
		Short: "Run the watcher in the foreground until interrupted",
		Long: `Run the watcher in the foreground until interrupted.

On start it reconciles every linked set, then watches each set and its dest:
new files in dest are adopted into the set, real files that replaced links
(atomic saves) are adopted and relinked, new set files are linked, and
deletions propagate both ways. Conflicts are left for ` + "`fibre status`" + `.

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
	cmd.AddCommand(run)
	return cmd
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
	return "Watcher is not running, so nothing is synced automatically. Start it with `fibre watch run`."
}
