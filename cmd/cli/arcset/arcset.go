// Package arcset provides CLI commands for arcset management.
package arcset

import (
	"github.com/spf13/cobra"
)

// Command returns the arcset parent command with all subcommands registered.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "arcset",
		Short: "Manage arcsets",
	}

	cmd.AddCommand(createCmd())
	cmd.AddCommand(appendCmd())
	cmd.AddCommand(buildDataCmd())
	cmd.AddCommand(makeECCmd())
	cmd.AddCommand(validateCmd())
	cmd.AddCommand(recoverCmd())
	cmd.AddCommand(exportCmd())
	cmd.AddCommand(listCmd())
	cmd.AddCommand(rebuildCmd())
	return cmd
}
