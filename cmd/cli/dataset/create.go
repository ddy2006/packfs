package dataset

import (
	"context"
	"fmt"
	"os"
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
				return errors.NewUsage("--root-dir is required")
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
			ds, err := dataset.CreateFromDir(context.Background(), store, rootDir, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, `{"dataset_id":%d}`+"\n", ds.ID)
			return nil
		},
	}
	cmd.Flags().String("root-dir", "", "root directory to scan recursively")
	cmd.Flags().String("name", "", "dataset name (default: last component of root-dir)")
	return cmd
}
