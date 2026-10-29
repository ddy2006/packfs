package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir, _ := cmd.Flags().GetString("root-dir")
			if rootDir == "" {
				return errors.NewUsage("--root-dir is required")
			}
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				name = filepath.Base(rootDir)
			}
			format, _ := cmd.Flags().GetString("format")
			if format == "" {
				format = "bin"
			}
			compress, _ := cmd.Flags().GetString("compress")
			shardMaxBytes, _ := cmd.Flags().GetInt64("shard-max-bytes")
			genOnly, _ := cmd.Flags().GetBool("gen-only")
			dbDir, _ := cmd.Flags().GetString("db-dir")
			targetRoot, _ := cmd.Flags().GetString("target-root")
			sortRegex, _ := cmd.Flags().GetString("shard-sort-regex")
			sortIndex, _ := cmd.Flags().GetString("shard-sort-index")

			if dbDir == "" {
				dbDir = os.Getenv("PACKFS_DATASET_DB_DIR")
			}
			if dbDir == "" {
				home, _ := os.UserHomeDir()
				dbDir = filepath.Join(home, "data", "packfs", "datasets")
			}
			if err := os.MkdirAll(dbDir, 0755); err != nil {
				return errors.WrapE(err, "create dataset db directory", "path", dbDir)
			}

			uid, err := dataset.NewUID()
			if err != nil {
				return err
			}
			dbPath := filepath.Join(dbDir, uid+".db")
			datasetDB, err := db.OpenDatasetSQLite(dbPath, false)
			if err != nil {
				return errors.WrapE(err, "open dataset sqlite", "path", dbPath)
			}
			defer datasetDB.Close()

			ctx := context.Background()
			store := dataset.NewDatasetStore(datasetDB)
			ds, err := dataset.CreateFromDirWithOptions(ctx, store, rootDir, name, dataset.CreateOptions{
				UID:    uid,
				Status: "building",
			})
			if err != nil {
				return err
			}

			// Re-fetch to get metadata set by CreateFromDir (num_files, total_bytes)
			ds, err = store.FindByID(ctx, ds.ID)
			if err != nil {
				return errors.WrapE(err, "re-fetch dataset")
			}
			if targetRoot == "" {
				targetRoot = ds.CurrentPath
			}
			if err := os.MkdirAll(targetRoot, 0755); err != nil {
				return errors.WrapE(err, "create shard root", "path", targetRoot)
			}
			meta := ds.Metadata
			if meta == nil {
				meta = make(map[string]any)
			}
			meta["format"] = format
			meta["compress"] = compress
			meta["shard_max_bytes"] = shardMaxBytes
			meta["shard_root"] = targetRoot
			if sortRegex != "" {
				meta["shard_sort_regex"] = sortRegex
			}
			if sortIndex != "" {
				meta["shard_sort_index"] = sortIndex
			}
			if err := store.UpdateMetadata(ctx, ds.ID, meta); err != nil {
				return errors.WrapE(err, "update dataset metadata")
			}

			if genOnly {
				fmt.Fprintf(os.Stderr, "created dataset %s (scan only, no shard generation)\n", name)
				return writeCreateResult(ds, dbPath, 0)
			}

			files, err := store.ListFiles(ctx, ds.ID)
			if err != nil {
				return errors.WrapE(err, "list dataset files")
			}
			shardDefs := dataset.GenerateShardDefs(files, shardMaxBytes, ds.ID)

			shardStore := shard.NewDatasetStore(datasetDB)
			for _, sd := range shardDefs {
				outName := fmt.Sprintf("%04d.%s", sd.Seq, format)
				outPath := filepath.Join(targetRoot, outName)
				cfg := shard.MakeConfig{
					Format:     format,
					Compress:   compress,
					SourceRoot: ds.CurrentPath,
					DatasetID:  ds.ID,
					Seq:        sd.Seq,
				}
				segs := convertSegments(sd.Segments)
				if _, err := shard.MakeShardWithWriter(ctx, datasetDB, shardStore, cfg, segs, outPath); err != nil {
					return errors.WrapE(err, "make shard", "seq", sd.Seq)
				}
			}

			ds.Metadata["shard_count"] = len(shardDefs)
			if err := store.UpdateMetadata(ctx, ds.ID, ds.Metadata); err != nil {
				return errors.WrapE(err, "update shard_count")
			}
			if err := store.UpdateStatus(ctx, ds.ID, "built"); err != nil {
				return errors.WrapE(err, "mark dataset built")
			}

			fmt.Fprintf(os.Stderr, "created dataset %s (%d files, %d shards)\n", name, len(files), len(shardDefs))
			return writeCreateResult(ds, dbPath, len(shardDefs))
		},
	}
	cmd.Flags().String("root-dir", "", "root directory to scan recursively")
	cmd.Flags().String("name", "", "dataset name (default: last component of root-dir)")
	cmd.Flags().String("db-dir", "", "directory for per-dataset SQLite files")
	cmd.Flags().String("target-root", "", "output directory for DATA shards")
	cmd.Flags().String("format", "bin", "shard format: bin / tar / iso")
	cmd.Flags().String("compress", "", "compression: zstd / xz / segment:zstd / segment:xz")
	cmd.Flags().Int64("shard-max-bytes", 0, "max shard size in bytes (0=unlimited)")
	cmd.Flags().String("shard-sort-regex", "", "regex extracting shard sort keys")
	cmd.Flags().String("shard-sort-index", "", "comma-separated capture group indexes")
	cmd.Flags().Bool("gen-only", false, "scan only, skip shard generation")
	return cmd
}

func writeCreateResult(ds *dataset.Dataset, sqlitePath string, shardCount int) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"dataset_uid":  ds.UID,
		"sqlite_path":  sqlitePath,
		"shard_count":  shardCount,
		"dataset_name": ds.Name,
	})
}

// convertSegments converts dataset.SegmentDesc to shard.SegmentDef for MakeShard.
func convertSegments(descs []dataset.SegmentDesc) []shard.SegmentDef {
	defs := make([]shard.SegmentDef, len(descs))
	for i, d := range descs {
		defs[i] = shard.SegmentDef{
			Path:   d.FilePath,
			Offset: d.FileOffset,
			Size:   d.SegmentSize,
			FileID: d.FileID,
		}
	}
	return defs
}
