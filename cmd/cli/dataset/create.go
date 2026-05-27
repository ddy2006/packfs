package dataset

import (
	"context"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				return errors.E("--source-root is required")
			}
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return errors.E("--name is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := dataset.NewSQLiteStore(sqlDB)
			return dataset.CreateFromDir(context.Background(), store, sourceRoot, name)
		},
	}
	cmd.Flags().String("source-root", "", "source root directory")
	cmd.Flags().String("name", "", "dataset name")
	return cmd
}
