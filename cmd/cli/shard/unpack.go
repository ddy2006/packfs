package shard

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
			arcsetID, _ := cmd.Flags().GetInt("arcset-id")

			if arcsetID <= 0 {
				format := inferFormat(shardFile)
				if format != "tar" {
					return errors.NewUsage("--arcset-id is required for non-tar shard files")
				}
				compress := inferCompress(shardFile)
				count, err := unpackTarShard(shardFile, targetRoot, compress)
				if err != nil {
					return err
				}
				fmt.Printf("unpacked %d files from %s\n", count, shardFile)
				return nil
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
			format, _ := a.Metadata["format"].(string)
			if format == "" {
				format = "bin"
			}

			count, err := unpackShardFile(shardStore, sh.ID, shardFile, targetRoot, compress, format)
			if err != nil {
				return err
			}
			fmt.Printf("unpacked %d files from %s\n", count, shardFile)
			return nil
		},
	}
	cmd.Flags().String("shard-file", "", "shard file to unpack")
	cmd.Flags().String("target-root", ".", "target root directory (default: current directory)")
	cmd.Flags().Int("arcset-id", 0, "arcset ID (not required for tar format)")
	return cmd
}

func unpackTarShard(shardAbsPath, targetRoot, compress string) (int, error) {
	src, err := os.ReadFile(shardAbsPath)
	if err != nil {
		return 0, errors.WrapE(err, "open shard file", "path", shardAbsPath)
	}

	isShardCompress := compress == "zstd" || compress == "xz"
	isSegmentCompress := compress == "segment:zstd" || compress == "segment:xz"
	isXZ := compress == "xz" || compress == "segment:xz"

	var data []byte = src
	if isShardCompress {
		data, err = decompressAll(src, isXZ)
		if err != nil {
			return 0, errors.WrapE(err, "decompress shard")
		}
	}

	tr := tar.NewReader(bytes.NewReader(data))
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, errors.WrapE(err, "read tar entry")
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		outPath := filepath.Join(targetRoot, hdr.Name)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return 0, err
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			return 0, errors.WrapE(err, "read tar entry data", "name", hdr.Name)
		}

		if isSegmentCompress {
			content, err = decompressAll(content, isXZ)
			if err != nil {
				return 0, errors.WrapE(err, "decompress segment", "file", hdr.Name)
			}
		}

		if err := os.WriteFile(outPath, content, os.FileMode(hdr.Mode)); err != nil {
			return 0, errors.WrapE(err, "create output file", "path", outPath)
		}
		count++
	}
	return count, nil
}

func unpackShardFile(store *shard.SQLiteStore, shardID int, shardAbsPath, targetRoot, compress, format string) (int, error) {
	infos, err := store.ListUnpackInfo(context.Background(), shardID)
	if err != nil {
		return 0, err
	}

	src, err := os.ReadFile(shardAbsPath)
	if err != nil {
		return 0, errors.WrapE(err, "open shard file", "path", shardAbsPath)
	}

	isShardCompress := compress == "zstd" || compress == "xz"
	isSegmentCompress := compress == "segment:zstd" || compress == "segment:xz"
	isXZ := compress == "xz" || compress == "segment:xz"

	if format == "tar" {
		var data []byte = src
		if isShardCompress {
			data, err = decompressAll(src, isXZ)
			if err != nil {
				return 0, errors.WrapE(err, "decompress shard")
			}
		}

		tr := tar.NewReader(bytes.NewReader(data))
		count := 0
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, errors.WrapE(err, "read tar entry")
			}
			if hdr.Typeflag != tar.TypeReg {
				continue
			}

			outPath := filepath.Join(targetRoot, hdr.Name)
			if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
				return 0, err
			}

			content, err := io.ReadAll(tr)
			if err != nil {
				return 0, errors.WrapE(err, "read tar entry data", "name", hdr.Name)
			}

			if isSegmentCompress {
				content, err = decompressAll(content, isXZ)
				if err != nil {
					return 0, errors.WrapE(err, "decompress segment", "file", hdr.Name)
				}
			}

			if err := os.WriteFile(outPath, content, os.FileMode(hdr.Mode)); err != nil {
				return 0, errors.WrapE(err, "create output file", "path", outPath)
			}
			count++
		}
		return count, nil
	}

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

// inferFormat detects the pack format from the shard filename extension.
func inferFormat(filename string) string {
	base := filepath.Base(filename)
	if strings.Contains(base, ".tar") {
		return "tar"
	}
	return "bin"
}

// inferCompress detects the compression mode from the shard filename extension.
func inferCompress(filename string) string {
	base := filepath.Base(filename)
	// segment-level: .zst.format or .xz.format
	if strings.Contains(base, ".zst.tar") || strings.Contains(base, ".zst.bin") {
		return "segment:zstd"
	}
	if strings.Contains(base, ".xz.tar") || strings.Contains(base, ".xz.bin") {
		return "segment:xz"
	}
	// shard-level: format.zst or format.xz
	if strings.Contains(base, ".tar.zst") || strings.Contains(base, ".bin.zst") {
		return "zstd"
	}
	if strings.Contains(base, ".tar.xz") || strings.Contains(base, ".bin.xz") {
		return "xz"
	}
	return ""
}
