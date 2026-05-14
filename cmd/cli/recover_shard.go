package main

import "github.com/spf13/cobra"

// 		make-archive-file json-file
// json file:
//		archive-file:
//		root-dir:
//		part-files
//			- entity-file-0:offset:length
//			- entity-file-1:offset:length
//			- entity-file-2:offset:length

var recoverShardCmd = &cobra.Command{
	Use:   "recover-shard",
	Short: "Recover shard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(recoverShardCmd)
}
