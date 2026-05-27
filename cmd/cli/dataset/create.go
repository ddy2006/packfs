package dataset

import (
	"context"
	"path/filepath"

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
			rootDir, _ := cmd.Flags().GetString("root-dir")
			if rootDir == "" {
				return errors.E("--root-dir is required")
			}
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				name = filepath.Base(rootDir)
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := dataset.NewSQLiteStore(sqlDB)
			return dataset.CreateFromDir(context.Background(), store, rootDir, name)
		},
	}
	cmd.Flags().String("root-dir", "", "root directory to scan recursively")
	cmd.Flags().String("name", "", "dataset name (default: last component of root-dir)")
	return cmd
}
