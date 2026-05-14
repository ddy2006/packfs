package main

import "github.com/spf13/cobra"

var fsckCmd = &cobra.Command{
	Use:   "fsck",
	Short: "Manage Filesystem check",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(fsckCmd)
}
