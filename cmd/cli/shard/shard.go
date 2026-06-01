// Package shard provides CLI commands for shard management.
package shard

import (
	"github.com/spf13/cobra"
)

// Command returns the shard parent command with all subcommands registered.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shard",
		Short: "Manage shards",
	}

	cmd.AddCommand(makeCmd())
	cmd.AddCommand(unpackCmd())
	cmd.AddCommand(validateCmd())
	cmd.AddCommand(makeECCmd())
	cmd.AddCommand(recoverCmd())
	return cmd
}
