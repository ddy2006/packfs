package main

import "github.com/spf13/cobra"

var arcSetCmd = &cobra.Command{
	Use:   "arcset",
	Short: "Manage arcsets",
}

// packfs arcset make --source-root=/absolute-path --target-root=/absolute-path --name=<arcset-name> --dataset-ids=1,2,3
var makeArcSetCmd = &cobra.Command{
	Use:   "make",
	Short: "Make arcset",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// packfs arcset gen-def --source-root=/absolute-path --target-root=/absolute-path
var genDefCmd = &cobra.Command{
	Use:   "gen-def",
	Short: "Generate shard-def files",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// packfs arcset unpack --source-root=/absolute-path --target-root=/absolute-path [--dataset-id=n] [--dataset-name=<regex>]
var unpackArcSetCmd = &cobra.Command{
	Use:   "unpack",
	Short: "Unpack arcset",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	arcSetCmd.AddCommand(makeArcSetCmd)
	makeArcSetCmd.Flags().String("source-root", "", "source root directory")
	makeArcSetCmd.Flags().String("target-root", "", "target root directory")
	makeArcSetCmd.Flags().String("name", "", "arcset name")
	makeArcSetCmd.Flags().String("dataset-ids", "", "comma-separated dataset IDs")

	arcSetCmd.AddCommand(genDefCmd)
	genDefCmd.Flags().String("source-root", "", "source root directory")
	genDefCmd.Flags().String("target-root", "", "target root directory for shard-def files")

	arcSetCmd.AddCommand(unpackArcSetCmd)
	unpackArcSetCmd.Flags().String("source-root", "", "source root directory")
	unpackArcSetCmd.Flags().String("target-root", "", "target root directory")
	unpackArcSetCmd.Flags().Int("dataset-id", 0, "filter by dataset ID")
	unpackArcSetCmd.Flags().String("dataset-name", "", "filter by dataset name")

	rootCmd.AddCommand(arcSetCmd)
}
