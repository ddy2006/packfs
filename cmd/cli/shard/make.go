package shard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func makeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Make shard",
		RunE: func(cmd *cobra.Command, args []string) error {
			defFile, _ := cmd.Flags().GetString("def-file")
			if defFile == "" {
				return errors.E("--def-file is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			return doMakeShard(sqlDB, defFile)
		},
	}
	cmd.Flags().String("def-file", "", "absolute path to shard definition file")
	return cmd
}

func doMakeShard(sqlDB *sql.DB, defFile string) error {
	_, meta, segs, err := shard.ReadDefFileMeta(defFile)
	if err != nil {
		return errors.WrapE(err, "read def file")
	}
	if meta.ArcsetID <= 0 {
		return errors.E("arcset_id not found in def file, run gen-def first")
	}
	if meta.DatasetID <= 0 {
		return errors.E("dataset_id not found in def file, run gen-def first")
	}

	arcStore := arcset.NewSQLiteStore(sqlDB)
	a, err := arcStore.FindByID(context.Background(), meta.ArcsetID)
	if err != nil {
		return errors.WrapE(err, "find arcset")
	}

	// target-root = arcset.current_path
	targetRoot := a.CurrentPath
	outName := defFile[:len(defFile)-4] // strip ".def"
	outPath := filepath.Join(targetRoot, filepath.Base(outName))

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return errors.WrapE(err, "create output directory")
	}

	// source-root = dataset.current_path
	sourceRoot, err := arcStore.FindDatasetPath(context.Background(), meta.DatasetID)
	if err != nil {
		return errors.WrapE(err, "find dataset path")
	}

	out, err := os.Create(outPath)
	if err != nil {
		return errors.WrapE(err, "create shard file", "path", outPath)
	}
	defer out.Close()

	shardStore := shard.NewSQLiteStore(sqlDB)
	var totalSize int64
	shardHash := sha256.New()
	mw := io.MultiWriter(out, shardHash)

	for _, seg := range segs {
		srcPath := seg.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(sourceRoot, seg.Path)
		}

		// 校验文件大小是否与 DB 记录一致
		if info, err := os.Stat(srcPath); err == nil {
			var dbSize int64
			_ = sqlDB.QueryRowContext(context.Background(),
				`SELECT file_size FROM t_file WHERE file_path = ? AND dataset = ?`,
				seg.Path, meta.DatasetID).Scan(&dbSize)
			if dbSize > 0 && info.Size() != dbSize {
				logrus.Warnf("%s: size changed since dataset creation (db=%d, disk=%d)",
					seg.Path, dbSize, info.Size())
			}
		}

		f, err := os.Open(srcPath)
		if err != nil {
			return errors.WrapE(err, "open source file", "path", seg.Path)
		}

		if seg.Offset > 0 {
			if _, err := f.Seek(seg.Offset, io.SeekStart); err != nil {
				f.Close()
				return errors.WrapE(err, "seek source file", "path", seg.Path)
			}
		}

		if seg.Size <= 0 {
			n, err := io.Copy(mw, f)
			f.Close()
			if err != nil {
				return errors.WrapE(err, "copy file", "path", seg.Path)
			}
			totalSize += n
		} else {
			n, err := io.CopyN(mw, f, seg.Size)
			f.Close()
			if err != nil && err != io.EOF {
				return errors.WrapE(err, "copy segment", "path", seg.Path)
			}
			totalSize += n
		}
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))

	relPath := filepath.Base(outPath)
	sh := &shard.Shard{
		FilePath: relPath,
		FileSize: totalSize,
		Type:     "DATA",
		Checksum: shardChecksum,
		Arcset:   meta.ArcsetID,
	}
	if err := shardStore.CreateShard(context.Background(), sh); err != nil {
		return err
	}

	fmt.Printf("created shard %s (%d bytes, sha256=%s)\n", outPath, totalSize, shardChecksum)
	return nil
}
