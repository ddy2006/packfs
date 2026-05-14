package main

import "github.com/spf13/cobra"

var exportSqliteCmd = &cobra.Command{
	Use:   "export-sqlite",
	Short: "Export metadata database to sqlite",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportSqliteCmd)
}
