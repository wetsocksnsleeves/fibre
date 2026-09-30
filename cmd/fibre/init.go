package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/wetsocksnsleeves/fibre/internal/root"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Make the current directory a dotfiles root",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			created, err := root.Init(dir)
			if err != nil {
				return err
			}
			if created {
				fmt.Fprintf(cmd.OutOrStdout(), "Initialized fibre root in %s\n", dir)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is already a fibre root\n", dir)
			}
			return nil
		},
	}
}
