package shard

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func unpackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Unpack shard",
		RunE: func(cmd *cobra.Command, args []string) error {
			shardFile, _ := cmd.Flags().GetString("shard-file")
			if shardFile == "" {
				return errors.E("--shard-file is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.E("--target-root is required")
			}
			arcsetID, _ := cmd.Flags().GetInt("arcset-id")
			if arcsetID <= 0 {
				return errors.E("--arcset-id is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := shard.NewSQLiteStore(sqlDB)

			// 用 filepath.Base 匹配 DB 中的相对路径
			relPath := filepath.Base(shardFile)
			sh, err := store.FindByArcsetAndFilePath(context.Background(), arcsetID, relPath)
			if err != nil {
				// 尝试用完整参数路径（可能用户传的就是相对路径）
				sh, err = store.FindByArcsetAndFilePath(context.Background(), arcsetID, shardFile)
				if err != nil {
					return errors.WrapE(err, "find shard", "arcset_id", arcsetID, "file", shardFile)
				}
			}

			count, err := unpackShardFile(store, sh.ID, shardFile, targetRoot)
			if err != nil {
				return err
			}
			fmt.Printf("unpacked %d files from %s\n", count, shardFile)
			return nil
		},
	}
	cmd.Flags().String("shard-file", "", "shard file to unpack")
	cmd.Flags().String("target-root", "", "target root directory")
	cmd.Flags().Int("arcset-id", 0, "arcset ID")
	return cmd
}

// unpackShardFile extracts all segments from a shard file.
func unpackShardFile(store *shard.SQLiteStore, shardID int, shardAbsPath, targetRoot string) (int, error) {
	infos, err := store.ListUnpackInfo(context.Background(), shardID)
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
