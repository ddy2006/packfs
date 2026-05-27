package shard

import "github.com/spf13/cobra"

func recoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Recover shard from EC",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().String("ec-shard-file", "", "EC shard file to recover from")
	cmd.Flags().String("target-root", "", "target root directory")
	return cmd
}
