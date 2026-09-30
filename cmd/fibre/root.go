package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fibre",
		Short: "Manage your dotfiles, keep them in sync, and keep machine-specific files local",
		Long: `fibre manages your dotfiles as sets: directories in a dotfiles repo, each
linked into a destination such as ~/.claude or ~/.config/nvim.

Each file is symlinked on its own, never a whole directory, so an edit on
either side is an edit to the same file. You choose exactly which files are
shared: excluded files stay as real files in the destination, and each
machine links only the sets it needs.`,
		Version:      version,
		SilenceUsage: true,
	}
	cmd.AddCommand(newInitCmd(), newLinkCmd())
	return cmd
}
