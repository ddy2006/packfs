package main

import "github.com/spf13/cobra"

var unpackArcSetCmd = &cobra.Command{
	Use:   "unpack-arcset",
	Short: "Unpack arcset",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(unpackArcSetCmd)
}
