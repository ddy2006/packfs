package shard

import (
	"archive/tar"
	"bytes"
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
	"github.com/kdomanski/iso9660"
	"github.com/klauspost/compress/zstd"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/ulikunitz/xz"
)

func makeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Make shard",
		RunE: func(cmd *cobra.Command, args []string) error {
			defFile, _ := cmd.Flags().GetString("def-file")
			if defFile == "" {
				return errors.NewUsage("--def-file is required")
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

	compress, _ := a.Metadata["compress"].(string)
	format, _ := a.Metadata["format"].(string)
	if format == "" {
		format = "bin"
	}
	if format == "tar" {
		return doMakeTarShard(sqlDB, a, meta, segs, defFile)
	}
	if format == "iso" {
		return doMakeIsoShard(sqlDB, a, meta, segs, defFile)
	}

	isSegmentCompress := compress == "segment:zstd" || compress == "segment:xz"
	isShardCompress := (compress == "zstd" || compress == "xz") && !isSegmentCompress
	isXZ := compress == "xz" || compress == "segment:xz"

	targetRoot := a.CurrentPath
	outName := defFile[:len(defFile)-4]
	outPath := filepath.Join(targetRoot, filepath.Base(outName))

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return errors.WrapE(err, "create output directory")
	}

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
	type segInfo struct {
		offset   int64
		size     int64
		csize    int64
		fileID   int
		fileSize int64
	}
	var segInfos []segInfo
	var totalSize int64
	shardHash := sha256.New()

	var shardCompressor io.WriteCloser
	if isShardCompress {
		shardCompressor, err = newCompressor(out, isXZ)
		if err != nil {
			return err
		}
	}

	for _, seg := range segs {
		srcPath := seg.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(sourceRoot, seg.Path)
		}

		var fileID int
		var dbSize int64
		_ = sqlDB.QueryRowContext(context.Background(),
			`SELECT id, file_size FROM t_file WHERE file_path = ? AND dataset = ?`,
			seg.Path, meta.DatasetID).Scan(&fileID, &dbSize)
		if info, err := os.Stat(srcPath); err == nil {
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

		var rawData []byte
		if seg.Size <= 0 {
			rawData, err = io.ReadAll(f)
		} else {
			rawData = make([]byte, seg.Size)
			_, err = io.ReadFull(f, rawData)
		}
		f.Close()
		if err != nil {
			return errors.WrapE(err, "read source file", "path", seg.Path)
		}

		offset := totalSize
		writeData := rawData
		var csize int64

		if isSegmentCompress {
			writeData, err = compressBytes(rawData, isXZ)
			if err != nil {
				return errors.WrapE(err, "compress segment", "path", seg.Path)
			}
			csize = int64(len(writeData))
		}

		if isShardCompress {
			shardCompressor.Write(rawData)
			totalSize += int64(len(rawData))
		} else {
			out.Write(writeData)
			shardHash.Write(writeData)
			totalSize += int64(len(writeData))
		}

		segInfos = append(segInfos, segInfo{
			offset:   offset,
			size:     int64(len(rawData)),
			csize:    csize,
			fileID:   fileID,
			fileSize: dbSize,
		})
	}

	shardFileSize := totalSize
	if isShardCompress {
		if err := shardCompressor.Close(); err != nil {
			return errors.WrapE(err, "close compressor")
		}
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return errors.WrapE(err, "seek shard file for hash")
		}
		if _, err := io.Copy(shardHash, out); err != nil {
			return errors.WrapE(err, "hash compressed shard")
		}
		stat, _ := out.Stat()
		shardFileSize = stat.Size()
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))

	relPath := filepath.Base(outPath)
	sh := &shard.Shard{
		FilePath: relPath,
		FileSize: shardFileSize,
		Type:     "DATA",
		Checksum: shardChecksum,
		Arcset:   meta.ArcsetID,
		Dataset:  meta.DatasetID,
	}
	if err := shardStore.CreateShard(context.Background(), sh); err != nil {
		return err
	}

	var segments []*shard.Segment
	for _, si := range segInfos {
		segments = append(segments, &shard.Segment{
			Offset:     si.offset,
			Size:       si.size,
			Csize:      si.csize,
			Shard:      sh.ID,
			File:       si.fileID,
			FileOffset: 0,
			FileSize:   si.fileSize,
		})
	}
	if err := shardStore.ReplaceSegments(context.Background(), sh.ID, segments); err != nil {
		return err
	}

	fmt.Printf("created shard %s (%d bytes, sha256=%s, %d segments)\n", outPath, shardFileSize, shardChecksum, len(segInfos))
	return nil
}

func doMakeTarShard(sqlDB *sql.DB, a *arcset.Arcset, meta shard.DefFileMeta, segs []shard.SegmentDef, defFile string) error {
	compress, _ := a.Metadata["compress"].(string)
	isSegmentCompress := compress == "segment:zstd" || compress == "segment:xz"
	isShardCompress := (compress == "zstd" || compress == "xz") && !isSegmentCompress
	isXZ := compress == "xz" || compress == "segment:xz"

	targetRoot := a.CurrentPath
	outName := defFile[:len(defFile)-4]
	outPath := filepath.Join(targetRoot, filepath.Base(outName))

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return errors.WrapE(err, "create output directory")
	}

	sourceRoot, err := arcset.NewSQLiteStore(sqlDB).FindDatasetPath(context.Background(), meta.DatasetID)
	if err != nil {
		return errors.WrapE(err, "find dataset path")
	}

	type segInfo struct {
		offset   int64
		size     int64
		csize    int64
		fileID   int
		fileSize int64
	}
	var segInfos []segInfo
	shardHash := sha256.New()

	// Build tar archive in buffer to compute offsets.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	for _, seg := range segs {
		srcPath := seg.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(sourceRoot, seg.Path)
		}

		var fileID int
		var dbSize int64
		_ = sqlDB.QueryRowContext(context.Background(),
			`SELECT id, file_size FROM t_file WHERE file_path = ? AND dataset = ?`,
			seg.Path, meta.DatasetID).Scan(&fileID, &dbSize)
		if info, err := os.Stat(srcPath); err == nil {
			if dbSize > 0 && info.Size() != dbSize {
				logrus.Warnf("%s: size changed since dataset creation (db=%d, disk=%d)",
					seg.Path, dbSize, info.Size())
			}
		}

		f, err := os.Open(srcPath)
		if err != nil {
			return errors.WrapE(err, "open source file", "path", seg.Path)
		}

		var rawData []byte
		if seg.Size <= 0 {
			rawData, err = io.ReadAll(f)
		} else {
			rawData = make([]byte, seg.Size)
			_, err = io.ReadFull(f, rawData)
		}
		f.Close()
		if err != nil {
			return errors.WrapE(err, "read source file", "path", seg.Path)
		}

		writeData := rawData
		var csize int64
		if isSegmentCompress {
			writeData, err = compressBytes(rawData, isXZ)
			if err != nil {
				return errors.WrapE(err, "compress segment", "path", seg.Path)
			}
			csize = int64(len(writeData))
		}

		offset := int64(tarBuf.Len())
		hdr := &tar.Header{
			Name: seg.Path,
			Size: int64(len(writeData)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return errors.WrapE(err, "write tar header", "path", seg.Path)
		}
		if _, err := tw.Write(writeData); err != nil {
			return errors.WrapE(err, "write tar entry", "path", seg.Path)
		}

		segInfos = append(segInfos, segInfo{
			offset:   offset,
			size:     int64(len(rawData)),
			csize:    csize,
			fileID:   fileID,
			fileSize: dbSize,
		})
	}

	if err := tw.Close(); err != nil {
		return errors.WrapE(err, "close tar writer")
	}

	tarData := tarBuf.Bytes()
	var shardFileSize int64

	out, err := os.Create(outPath)
	if err != nil {
		return errors.WrapE(err, "create shard file", "path", outPath)
	}
	defer out.Close()

	if isShardCompress {
		compressed, err := compressBytes(tarData, isXZ)
		if err != nil {
			return errors.WrapE(err, "compress shard")
		}
		if _, err := out.Write(compressed); err != nil {
			return errors.WrapE(err, "write compressed shard")
		}
		shardHash.Write(compressed)
		shardFileSize = int64(len(compressed))
	} else {
		if _, err := out.Write(tarData); err != nil {
			return errors.WrapE(err, "write shard")
		}
		shardHash.Write(tarData)
		shardFileSize = int64(len(tarData))
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))

	relPath := filepath.Base(outPath)
	shardStore := shard.NewSQLiteStore(sqlDB)
	sh := &shard.Shard{
		FilePath: relPath,
		FileSize: shardFileSize,
		Type:     "DATA",
		Checksum: shardChecksum,
		Arcset:   meta.ArcsetID,
		Dataset:  meta.DatasetID,
	}
	if err := shardStore.CreateShard(context.Background(), sh); err != nil {
		return err
	}

	var segments []*shard.Segment
	for _, si := range segInfos {
		segments = append(segments, &shard.Segment{
			Offset:     si.offset,
			Size:       si.size,
			Csize:      si.csize,
			Shard:      sh.ID,
			File:       si.fileID,
			FileOffset: 0,
			FileSize:   si.fileSize,
		})
	}
	if err := shardStore.ReplaceSegments(context.Background(), sh.ID, segments); err != nil {
		return err
	}

	fmt.Printf("created shard %s (%d bytes, sha256=%s, %d segments)\n", outPath, shardFileSize, shardChecksum, len(segInfos))
	return nil
}

func doMakeIsoShard(sqlDB *sql.DB, a *arcset.Arcset, meta shard.DefFileMeta, segs []shard.SegmentDef, defFile string) error {
	compress, _ := a.Metadata["compress"].(string)
	isSegmentCompress := compress == "segment:zstd" || compress == "segment:xz"
	isShardCompress := (compress == "zstd" || compress == "xz") && !isSegmentCompress
	isXZ := compress == "xz" || compress == "segment:xz"

	targetRoot := a.CurrentPath
	outName := defFile[:len(defFile)-4]
	outPath := filepath.Join(targetRoot, filepath.Base(outName))

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return errors.WrapE(err, "create output directory")
	}

	sourceRoot, err := arcset.NewSQLiteStore(sqlDB).FindDatasetPath(context.Background(), meta.DatasetID)
	if err != nil {
		return errors.WrapE(err, "find dataset path")
	}

	type segInfo struct {
		offset   int64
		size     int64
		csize    int64
		fileID   int
		fileSize int64
	}
	var segInfos []segInfo
	shardHash := sha256.New()

	w, err := iso9660.NewWriter()
	if err != nil {
		return errors.WrapE(err, "create iso writer")
	}
	defer w.Cleanup()

	for _, seg := range segs {
		srcPath := seg.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(sourceRoot, seg.Path)
		}

		var fileID int
		var dbSize int64
		_ = sqlDB.QueryRowContext(context.Background(),
			`SELECT id, file_size FROM t_file WHERE file_path = ? AND dataset = ?`,
			seg.Path, meta.DatasetID).Scan(&fileID, &dbSize)
		if info, err := os.Stat(srcPath); err == nil {
			if dbSize > 0 && info.Size() != dbSize {
				logrus.Warnf("%s: size changed since dataset creation (db=%d, disk=%d)",
					seg.Path, dbSize, info.Size())
			}
		}

		f, err := os.Open(srcPath)
		if err != nil {
			return errors.WrapE(err, "open source file", "path", seg.Path)
		}

		var rawData []byte
		if seg.Size <= 0 {
			rawData, err = io.ReadAll(f)
		} else {
			rawData = make([]byte, seg.Size)
			_, err = io.ReadFull(f, rawData)
		}
		f.Close()
		if err != nil {
			return errors.WrapE(err, "read source file", "path", seg.Path)
		}

		writeData := rawData
		var csize int64
		if isSegmentCompress {
			writeData, err = compressBytes(rawData, isXZ)
			if err != nil {
				return errors.WrapE(err, "compress segment", "path", seg.Path)
			}
			csize = int64(len(writeData))
		}

		if err := w.AddFile(bytes.NewReader(writeData), seg.Path); err != nil {
			return errors.WrapE(err, "add file to iso", "path", seg.Path)
		}

		segInfos = append(segInfos, segInfo{
			size:     int64(len(rawData)),
			csize:    csize,
			fileID:   fileID,
			fileSize: dbSize,
		})
	}

	var isoBuf bytes.Buffer
	if err := w.WriteTo(&isoBuf, "PACKFS"); err != nil {
		return errors.WrapE(err, "write iso")
	}

	isoData := isoBuf.Bytes()
	var shardFileSize int64

	out, err := os.Create(outPath)
	if err != nil {
		return errors.WrapE(err, "create shard file", "path", outPath)
	}
	defer out.Close()

	if isShardCompress {
		compressed, err := compressBytes(isoData, isXZ)
		if err != nil {
			return errors.WrapE(err, "compress shard")
		}
		if _, err := out.Write(compressed); err != nil {
			return errors.WrapE(err, "write compressed shard")
		}
		shardHash.Write(compressed)
		shardFileSize = int64(len(compressed))
	} else {
		if _, err := out.Write(isoData); err != nil {
			return errors.WrapE(err, "write shard")
		}
		shardHash.Write(isoData)
		shardFileSize = int64(len(isoData))
	}

	shardChecksum := fmt.Sprintf("%x", shardHash.Sum(nil))

	relPath := filepath.Base(outPath)
	shardStore := shard.NewSQLiteStore(sqlDB)
	sh := &shard.Shard{
		FilePath: relPath,
		FileSize: shardFileSize,
		Type:     "DATA",
		Checksum: shardChecksum,
		Arcset:   meta.ArcsetID,
		Dataset:  meta.DatasetID,
	}
	if err := shardStore.CreateShard(context.Background(), sh); err != nil {
		return err
	}

	var segments []*shard.Segment
	for _, si := range segInfos {
		segments = append(segments, &shard.Segment{
			Size:       si.size,
			Csize:      si.csize,
			Shard:      sh.ID,
			File:       si.fileID,
			FileOffset: 0,
			FileSize:   si.fileSize,
		})
	}
	if err := shardStore.ReplaceSegments(context.Background(), sh.ID, segments); err != nil {
		return err
	}

	fmt.Printf("created shard %s (%d bytes, sha256=%s, %d segments)\n", outPath, shardFileSize, shardChecksum, len(segInfos))
	return nil
}

func newCompressor(w io.Writer, isXZ bool) (io.WriteCloser, error) {
	if isXZ {
		return xz.NewWriter(w)
	}
	return zstd.NewWriter(w)
}

func compressBytes(data []byte, isXZ bool) ([]byte, error) {
	var buf bytes.Buffer
	w, err := newCompressor(&buf, isXZ)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
