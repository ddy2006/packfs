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
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return errors.E("--name is required")
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
			a, err := store.FindByName(context.Background(), name)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}

			descs, err := arcset.GenerateSegments(context.Background(), store, a.ID)
			if err != nil {
				return errors.WrapE(err, "generate segments")
			}

			if err := writeDefFile(targetRoot, name, descs); err != nil {
				return errors.WrapE(err, "write def file")
			}

			fmt.Printf("generated %s/%s.bin.def with %d segments\n", targetRoot, name, len(descs))
			return nil
		},
	}
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().String("target-root", "", "target root directory for shard-def files")
	return cmd
}

func writeDefFile(dir, arcsetName string, descs []arcset.SegmentDesc) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, arcsetName+".bin.def")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, d := range descs {
		if d.FileOffset == 0 && d.SegmentSize == d.FileSize {
			fmt.Fprintln(f, d.FilePath)
		} else {
			fmt.Fprintf(f, `{"path":"%s","offset":%d,"size":%d}`+"\n",
				d.FilePath, d.FileOffset, d.SegmentSize)
		}
	}
	return nil
}
