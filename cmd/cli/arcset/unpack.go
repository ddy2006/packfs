package arcset

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func unpackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Unpack arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return errors.E("--name is required")
			}
			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				return errors.E("--source-root is required")
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

			arcStore := arcset.NewSQLiteStore(sqlDB)
			a, err := arcStore.FindByName(context.Background(), name)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}

			shardStore := shard.NewSQLiteStore(sqlDB)
			shards, err := shardStore.FindByArcset(context.Background(), a.ID)
			if err != nil {
				return errors.WrapE(err, "find shards")
			}

			var totalFiles int
			for _, sh := range shards {
				shardAbsPath := filepath.Join(sourceRoot, sh.FilePath)
				count, err := unpackShardFile(shardStore, sh.ID, shardAbsPath, targetRoot)
				if err != nil {
					return errors.WrapE(err, "unpack shard", "path", sh.FilePath)
				}
				totalFiles += count
			}

			fmt.Printf("unpacked %d files from arcset %s (%d shards)\n", totalFiles, name, len(shards))
			return nil
		},
	}
	cmd.Flags().String("source-root", "", "source root directory where shard files reside")
	cmd.Flags().String("target-root", "", "target root directory for extracted files")
	cmd.Flags().String("name", "", "arcset name")
	cmd.Flags().Int("dataset-id", 0, "filter by dataset ID")
	cmd.Flags().String("dataset-name", "", "filter by dataset name")
	return cmd
}

// unpackShardFile extracts all segments from a shard file to targetRoot.
func unpackShardFile(shardStore *shard.SQLiteStore, shardID int, shardAbsPath, targetRoot string) (int, error) {
	infos, err := shardStore.ListUnpackInfo(context.Background(), shardID)
	if err != nil {
		return 0, err
	}

	src, err := os.Open(shardAbsPath)
	if err != nil {
		return 0, errors.WrapE(err, "open shard file", "path", shardAbsPath)
	}
	defer src.Close()

	for _, info := range infos {
		outPath := filepath.Join(targetRoot, info.FilePath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return 0, err
		}
		out, err := os.Create(outPath)
		if err != nil {
			return 0, errors.WrapE(err, "create output file", "path", outPath)
		}

		if _, err := src.Seek(info.Offset, io.SeekStart); err != nil {
			out.Close()
			return 0, errors.WrapE(err, "seek shard", "offset", info.Offset)
		}
		_, err = io.CopyN(out, src, info.Size)
		out.Close()
		if err != nil {
			return 0, errors.WrapE(err, "copy segment", "file", info.FilePath)
		}
	}
	return len(infos), nil
}
