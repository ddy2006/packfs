package shard

import (
	"context"
	"database/sql"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
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

			outputDir, _ := cmd.Flags().GetString("output-dir")
			return doMakeShard(sqlDB, defFile, outputDir)
		},
	}
	cmd.Flags().String("def-file", "", "absolute path to shard definition file")
	cmd.Flags().String("output-dir", "", "override output directory for shard file")
	return cmd
}

func doMakeShard(sqlDB *sql.DB, defFile, outputDir string) error {
	_, meta, segs, err := shard.ReadDefFileMeta(defFile)
	if err != nil {
		return errors.WrapE(err, "read def file")
	}
	if meta.DatasetID <= 0 {
		return errors.E("dataset_id not found in def file, run gen-def first")
	}

	var format, compress, sourceRoot, targetRoot string
	var arcsetID int

	if meta.ArcsetID > 0 {
		// arcset 模式：从 arcset metadata 读取配置
		arcStore := arcset.NewSQLiteStore(sqlDB)
		a, err := arcStore.FindByID(context.Background(), meta.ArcsetID)
		if err != nil {
			return errors.WrapE(err, "find arcset")
		}
		compress, _ = a.Metadata["compress"].(string)
		format, _ = a.Metadata["format"].(string)
		targetRoot = a.CurrentPath
		arcsetID = meta.ArcsetID

		sourceRoot, err = arcStore.FindDatasetPath(context.Background(), meta.DatasetID)
		if err != nil {
			return errors.WrapE(err, "find dataset path")
		}
	} else {
		// dataset-only 模式：从 dataset metadata 读取配置
		dsStore := dataset.NewSQLiteStore(sqlDB)
		ds, err := dsStore.FindByID(context.Background(), meta.DatasetID)
		if err != nil {
			return errors.WrapE(err, "find dataset")
		}
		compress, _ = ds.Metadata["compress"].(string)
		format, _ = ds.Metadata["format"].(string)
		targetRoot = ds.CurrentPath
		sourceRoot = ds.CurrentPath
	}

	if format == "" {
		format = "bin"
	}

	if outputDir != "" {
		targetRoot = outputDir
	}

	outName := defFile[:len(defFile)-4]
	outPath := filepath.Join(targetRoot, filepath.Base(outName))

	cfg := shard.MakeConfig{
		Format:     format,
		Compress:   compress,
		SourceRoot: sourceRoot,
		ArcsetID:   arcsetID,
		DatasetID:  meta.DatasetID,
	}
	_, err = shard.MakeShard(context.Background(), sqlDB, cfg, segs, outPath)
	return err
}
