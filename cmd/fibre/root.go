package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:          "fibre",
		Short:        "Link dotfile sets into their destinations and keep them in sync",
		Version:      version,
		SilenceUsage: true,
	}
}
