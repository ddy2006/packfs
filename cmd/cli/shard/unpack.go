package shard

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/klauspost/compress/zstd"
	"github.com/spf13/cobra"
	"github.com/ulikunitz/xz"
)

func unpackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Unpack shard",
		RunE: func(cmd *cobra.Command, args []string) error {
			shardFile, _ := cmd.Flags().GetString("shard-file")
			if shardFile == "" {
				return errors.NewUsage("--shard-file is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.NewUsage("--target-root is required")
			}
			arcsetID, _ := cmd.Flags().GetInt("arcset-id")
			if arcsetID <= 0 {
				return errors.NewUsage("--arcset-id is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			shardStore := shard.NewSQLiteStore(sqlDB)

			relPath := filepath.Base(shardFile)
			sh, err := shardStore.FindByArcsetAndFilePath(context.Background(), arcsetID, relPath)
			if err != nil {
				sh, err = shardStore.FindByArcsetAndFilePath(context.Background(), arcsetID, shardFile)
				if err != nil {
					return errors.WrapE(err, "find shard", "arcset_id", arcsetID, "file", shardFile)
				}
			}

			arcStore := arcset.NewSQLiteStore(sqlDB)
			a, err := arcStore.FindByID(context.Background(), arcsetID)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}
			compress, _ := a.Metadata["compress"].(string)

			count, err := unpackShardFile(shardStore, sh.ID, shardFile, targetRoot, compress)
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

func unpackShardFile(store *shard.SQLiteStore, shardID int, shardAbsPath, targetRoot, compress string) (int, error) {
	infos, err := store.ListUnpackInfo(context.Background(), shardID)
	if err != nil {
		return 0, err
	}

	src, err := os.ReadFile(shardAbsPath)
	if err != nil {
		return 0, errors.WrapE(err, "open shard file", "path", shardAbsPath)
	}

	isShardCompress := compress == "zstd" || compress == "xz"
	isXZ := compress == "xz" || compress == "segment:xz"

	var decompressed []byte
	if isShardCompress {
		decompressed, err = decompressAll(src, isXZ)
		if err != nil {
			return 0, errors.WrapE(err, "decompress shard")
		}
	}

	for _, info := range infos {
		outPath := filepath.Join(targetRoot, info.FilePath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return 0, err
		}

		var data []byte
		if isShardCompress {
			data = decompressed[info.Offset : info.Offset+info.Size]
		} else if info.Csize > 0 {
			data, err = decompressAll(src[info.Offset:info.Offset+info.Csize], isXZ)
			if err != nil {
				return 0, errors.WrapE(err, "decompress segment", "file", info.FilePath)
			}
		} else {
			data = src[info.Offset : info.Offset+info.Size]
		}

		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return 0, errors.WrapE(err, "create output file", "path", outPath)
		}
	}
	return len(infos), nil
}

func decompressAll(data []byte, isXZ bool) ([]byte, error) {
	if isXZ {
		r, err := xz.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(r)
	}
	r, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
