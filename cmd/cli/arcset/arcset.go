// Package arcset provides CLI commands for arcset management.
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

// Command returns the arcset parent command with all subcommands registered.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "arcset",
		Short: "Manage arcsets",
	}

	cmd.AddCommand(makeCmd())
	cmd.AddCommand(genDefCmd())
	cmd.AddCommand(unpackCmd())
	return cmd
}

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

func unpackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Unpack arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().String("source-root", "", "source root directory")
	cmd.Flags().String("target-root", "", "target root directory")
	cmd.Flags().Int("dataset-id", 0, "filter by dataset ID")
	cmd.Flags().String("dataset-name", "", "filter by dataset name")
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
