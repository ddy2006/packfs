package arcset

import "github.com/spf13/cobra"

func makeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Make arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().String("source-root", "", "source root directory")
	cmd.Flags().String("target-root", "", "target root directory")
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().String("dataset-ids", "", "comma-separated dataset IDs")
	return cmd
}
