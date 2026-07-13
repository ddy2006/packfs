package shard

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/kaichao/gopkg/errors"
	"github.com/kdomanski/iso9660"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// DecompressAll decompresses data using zstd or xz based on isXZ flag.
func DecompressAll(data []byte, isXZ bool) ([]byte, error) {
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

// UnpackShardFile unpacks a single shard file into targetRoot directory.
// Returns the number of files extracted.
func UnpackShardFile(ctx context.Context, store Store, shardID int, shardAbsPath, targetRoot, compress, format string) (int, error) {
	infos, err := store.ListUnpackInfo(ctx, shardID)
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

	// tar format
	if format == "tar" {
		var data []byte = src
		if isShardCompress {
			data, err = DecompressAll(src, isXZ)
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
				content, err = DecompressAll(content, isXZ)
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

	// iso format
	if format == "iso" {
		var data []byte = src
		if isShardCompress {
			data, err = DecompressAll(src, isXZ)
			if err != nil {
				return 0, errors.WrapE(err, "decompress shard")
			}
		}

		img, err := iso9660.OpenImage(bytes.NewReader(data))
		if err != nil {
			return 0, errors.WrapE(err, "open iso image")
		}

		root, err := img.RootDir()
		if err != nil {
			return 0, errors.WrapE(err, "read iso root dir")
		}

		return extractISO(root, targetRoot, "", isSegmentCompress, isXZ)
	}

	// bin format
	var decompressed []byte
	if isShardCompress {
		decompressed, err = DecompressAll(src, isXZ)
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
			data, err = DecompressAll(src[info.Offset:info.Offset+info.Csize], isXZ)
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

func extractISO(dir *iso9660.File, targetRoot, prefix string, isSegmentCompress, isXZ bool) (int, error) {
	count := 0
	children, err := dir.GetChildren()
	if err != nil {
		return 0, errors.WrapE(err, "read iso dir", "name", dir.Name())
	}
	for _, child := range children {
		childRel := filepath.Join(prefix, child.Name())
		if child.IsDir() {
			subCount, err := extractISO(child, targetRoot, childRel, isSegmentCompress, isXZ)
			if err != nil {
				return count, err
			}
			count += subCount
			continue
		}

		outPath := filepath.Join(targetRoot, childRel)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return count, err
		}

		content, err := io.ReadAll(child.Reader())
		if err != nil {
			return count, errors.WrapE(err, "read iso file", "name", childRel)
		}

		if isSegmentCompress {
			content, err = DecompressAll(content, isXZ)
			if err != nil {
				return count, errors.WrapE(err, "decompress segment", "file", childRel)
			}
		}

		if err := os.WriteFile(outPath, content, 0644); err != nil {
			return count, errors.WrapE(err, "create output file", "path", outPath)
		}
		count++
	}
	return count, nil
}
