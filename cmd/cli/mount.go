package main

import "github.com/spf13/cobra"

var mountCmd = &cobra.Command{
	Use:   "mount",
	Short: "mount packfs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mountCmd)
}
