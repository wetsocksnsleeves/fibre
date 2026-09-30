package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "fibre",
		Short:        "Link dotfile sets into their destinations and keep them in sync",
		Version:      version,
		SilenceUsage: true,
	}
	cmd.AddCommand(newInitCmd())
	return cmd
}
