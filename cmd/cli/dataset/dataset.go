// Package dataset provides CLI commands for dataset management.
package dataset

import (
	"github.com/spf13/cobra"
)

// Command returns the dataset parent command with all subcommands registered.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dataset",
		Short: "Manage datasets",
	}

	cmd.AddCommand(createCmd())
	cmd.AddCommand(listCmd())
	cmd.AddCommand(unpackCmd())
	cmd.AddCommand(validateCmd())
	cmd.AddCommand(finalizeCmd())
	return cmd
}
