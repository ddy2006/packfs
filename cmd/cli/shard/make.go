package shard

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"

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
			outputDir, _ := cmd.Flags().GetString("output-dir")

			_, meta, segs, err := shard.ReadDefFileMeta(defFile)
			if err != nil {
				return errors.WrapE(err, "read def file")
			}
			if meta.DatasetDB != "" {
				return doMakeShardDataset(defFile, meta, segs, outputDir)
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()
			return doMakeShard(sqlDB, defFile, outputDir)
		},
	}
	cmd.Flags().String("def-file", "", "absolute path to shard definition file")
	cmd.Flags().String("output-dir", "", "override output directory for shard file")
	return cmd
}

// doMakeShardDataset is the per-dataset SQLite path. The .def file carries
// "# dataset_db: <path>"; no global SQLite is opened.
func doMakeShardDataset(defFile string, meta shard.DefFileMeta, segs []shard.SegmentDef, outputDir string) error {
	dbPath := meta.DatasetDB
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(filepath.Dir(defFile), dbPath)
	}
	datasetDB, err := db.OpenDatasetSQLite(dbPath, false)
	if err != nil {
		return errors.WrapE(err, "open dataset sqlite", "path", dbPath)
	}
	defer datasetDB.Close()

	ctx := context.Background()
	dsStore := dataset.NewDatasetStore(datasetDB)
	ds, err := findDatasetForDef(ctx, dsStore, meta.DatasetID)
	if err != nil {
		return err
	}

	compress, _ := ds.Metadata["compress"].(string)
	format, _ := ds.Metadata["format"].(string)
	if format == "" {
		format = "bin"
	}
	sourceRoot := ds.CurrentPath

	targetRoot := outputDir
	if targetRoot == "" {
		targetRoot, _ = ds.Metadata["shard_root"].(string)
	}
	if targetRoot == "" {
		targetRoot = ds.CurrentPath
	}

	outName := strings.TrimSuffix(filepath.Base(defFile), ".def")
	outPath := filepath.Join(targetRoot, outName)

	cfg := shard.MakeConfig{
		Format:     format,
		Compress:   compress,
		SourceRoot: sourceRoot,
		DatasetID:  ds.ID,
		Seq:        -1,
	}
	_, err = shard.MakeShardWithWriter(ctx, datasetDB, shard.NewDatasetStore(datasetDB),
		cfg, segs, outPath)
	return err
}

func findDatasetForDef(ctx context.Context, store *dataset.DatasetStore, id int) (*dataset.Dataset, error) {
	if id > 0 {
		return store.FindByID(ctx, id)
	}
	limit := 1
	all, err := store.Find(ctx, dataset.Filter{Limit: &limit})
	if err != nil {
		return nil, errors.WrapE(err, "list datasets in sqlite")
	}
	if len(all) == 0 {
		return nil, errors.E("dataset sqlite contains no dataset")
	}
	return all[0], nil
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

	outName := strings.TrimSuffix(filepath.Base(defFile), ".def")
	outPath := filepath.Join(targetRoot, outName)

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
