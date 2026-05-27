package arcset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func genDefCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-def",
		Short: "Generate shard-def files",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.E("--id is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.E("--target-root is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := arcset.NewSQLiteStore(sqlDB)
			a, err := store.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}

			shards, err := arcset.GenerateShardDefs(context.Background(), store, a.ID)
			if err != nil {
				return errors.WrapE(err, "generate shard defs")
			}

			for _, sd := range shards {
				fileName := fmt.Sprintf("%04d.bin.def", sd.Seq)
				if err := writeDefFile(targetRoot, fileName, a.ID, sd.DatasetID, sd.Segments); err != nil {
					return errors.WrapE(err, "write def file")
				}
			}

			fmt.Printf("generated %d shard-def file(s) for arcset %s in %s\n", len(shards), a.Name, targetRoot)
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID")
	cmd.Flags().String("target-root", "", "target root directory for shard-def files")
	return cmd
}

func writeDefFile(dir, fileName string, arcsetID, datasetID int, descs []arcset.SegmentDesc) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, fileName)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# arcset_id: %d\n", arcsetID)
	fmt.Fprintf(f, "# dataset_id: %d\n", datasetID)
	for _, d := range descs {
		fmt.Fprintln(f, d.FilePath)
	}
	return nil
}
