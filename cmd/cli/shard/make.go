package shard

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/param"
	"github.com/spf13/cobra"
)

func makeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Make shard",
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceRoot, err := param.GetString(cmd, "source-root", param.WithRequired())
			if err != nil {
				return errors.WrapE(err, 1, "get parameter source-root failed")
			}
			targetRoot, err := param.GetString(cmd, "target-root", param.WithRequired())
			if err != nil {
				return errors.WrapE(err, 1, "get parameter target-root failed")
			}
			defFile, err := param.GetString(cmd, "def-file", param.WithRequired())
			if err != nil {
				return errors.WrapE(err, 1, "get parameter def-file failed")
			}
			return doMakeShard(sourceRoot, targetRoot, defFile)
		},
	}
	cmd.Flags().String("source-root", "", "source root directory")
	cmd.Flags().String("target-root", "", "target root directory")
	cmd.Flags().String("def-file", "", "shard definition file")
	return cmd
}

func doMakeShard(sourceRoot, targetRoot, defFile string) error {
	defName, segs, err := shard.ReadDefFile(defFile)
	if err != nil {
		return errors.WrapE(err, "read def file")
	}
	_ = defName

	outName := defFile[:len(defFile)-4] // strip ".def"
	outPath := filepath.Join(targetRoot, filepath.Base(outName))

	out, err := os.Create(outPath)
	if err != nil {
		return errors.WrapE(err, "create shard file", "path", outPath)
	}
	defer out.Close()

	var totalSize int64
	shardHash := sha256.New()
	mw := io.MultiWriter(out, shardHash)

	for _, seg := range segs {
		srcPath := seg.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(sourceRoot, seg.Path)
		}
		src, err := os.Open(srcPath)
		if err != nil {
			return errors.WrapE(err, "open source file", "path", seg.Path)
		}

		if seg.Offset > 0 {
			if _, err := src.Seek(seg.Offset, io.SeekStart); err != nil {
				src.Close()
				return errors.WrapE(err, "seek source file", "path", seg.Path)
			}
		}

		if seg.Size <= 0 {
			n, err := io.Copy(mw, src)
			src.Close()
			if err != nil {
				return errors.WrapE(err, "copy file", "path", seg.Path)
			}
			totalSize += n
		} else {
			n, err := io.CopyN(mw, src, seg.Size)
			src.Close()
			if err != nil && err != io.EOF {
				return errors.WrapE(err, "copy segment", "path", seg.Path)
			}
			totalSize += n
		}
	}

	checksum := fmt.Sprintf("%x", shardHash.Sum(nil))
	fmt.Printf("created shard %s (%d bytes, sha256=%s)\n", outPath, totalSize, checksum)
	return nil
}
