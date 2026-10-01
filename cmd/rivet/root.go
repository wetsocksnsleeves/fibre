package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "rivet",
		Short:        "Manage your dotfiles, keep them in sync, and keep machine-specific files local",
		Version:      version,
		SilenceUsage: true,
	}
	cmd.AddCommand(newInitCmd(), newLinkCmd(), newUnlinkCmd(), newExcludeCmd(), newStatusCmd(), newWatchCmd())
	return cmd
}
