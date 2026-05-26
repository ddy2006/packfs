package main

import "github.com/spf13/cobra"

var dataSetCmd = &cobra.Command{
	Use:   "dataset",
	Short: "Manage datasets",
}

// packfs dataset create --source-root=/absolute-path --name=<dataset-name>
var createDataSetCmd = &cobra.Command{
	Use:   "create",
	Short: "Create dataset",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// packfs dataset list [--dataset-id=n] [--dataset-name=<regex>]
var listDataSetCmd = &cobra.Command{
	Use:   "list",
	Short: "List datasets",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	dataSetCmd.AddCommand(createDataSetCmd)
	createDataSetCmd.Flags().String("source-root", "", "source root directory")
	createDataSetCmd.Flags().String("name", "", "dataset name")

	dataSetCmd.AddCommand(listDataSetCmd)
	listDataSetCmd.Flags().Int("dataset-id", 0, "filter by dataset ID")
	listDataSetCmd.Flags().String("dataset-name", "", "filter by dataset name (regex)")

	rootCmd.AddCommand(dataSetCmd)
}
