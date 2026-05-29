package arcset

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
		Short: "Unpack arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return errors.NewUsage("--name is required")
			}
			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				return errors.NewUsage("--source-root is required")
			}
			targetRoot, _ := cmd.Flags().GetString("target-root")
			if targetRoot == "" {
				return errors.NewUsage("--target-root is required")
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

			compress, _ := a.Metadata["compress"].(string)
			var totalFiles int
			for _, sh := range shards {
				shardAbsPath := filepath.Join(sourceRoot, sh.FilePath)
				count, err := unpackShardFile(shardStore, sh.ID, shardAbsPath, targetRoot, compress)
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

func unpackShardFile(shardStore *shard.SQLiteStore, shardID int, shardAbsPath, targetRoot, compress string) (int, error) {
	infos, err := shardStore.ListUnpackInfo(context.Background(), shardID)
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
