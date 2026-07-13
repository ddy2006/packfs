package dataset

import (
	"context"
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

		sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := dataset.NewSQLiteStore(sqlDB)
			ds, err := dataset.CreateFromDir(context.Background(), store, rootDir, name)
			if err != nil {
				return err
			}

			// Re-fetch to get metadata set by CreateFromDir (num_files, total_bytes)
			ds, err = store.FindByID(context.Background(), ds.ID)
			if err != nil {
				return errors.WrapE(err, "re-fetch dataset")
			}
			meta := ds.Metadata
			if meta == nil {
				meta = make(map[string]any)
			}
			meta["format"] = format
			meta["compress"] = compress
			meta["shard_max_bytes"] = shardMaxBytes
			if err := store.UpdateMetadata(context.Background(), ds.ID, meta); err != nil {
				return errors.WrapE(err, "update dataset metadata")
			}

			if genOnly {
				fmt.Printf("created dataset %s (scan only, no shard generation)\n", name)
				fmt.Fprintf(os.Stdout, `{"dataset_id":%d}`+"\n", ds.ID)
				return nil
			}

			// List files and generate shard defs
			files, err := store.ListFiles(context.Background(), ds.ID)
			if err != nil {
				return errors.WrapE(err, "list dataset files")
			}
			shardDefs := dataset.GenerateShardDefs(files, shardMaxBytes, ds.ID)

			// Make shards directly
			for _, sd := range shardDefs {
				outName := fmt.Sprintf("%04d.%s", sd.Seq, format)
				outPath := filepath.Join(ds.CurrentPath, outName)
				cfg := shard.MakeConfig{
					Format:     format,
					Compress:   compress,
					SourceRoot: ds.CurrentPath,
					DatasetID:  ds.ID,
				}
				segs := convertSegments(sd.Segments)
				if _, err := shard.MakeShard(context.Background(), sqlDB, cfg, segs, outPath); err != nil {
					return errors.WrapE(err, "make shard", "seq", sd.Seq)
				}
			}

			// Update shard count (merge into existing metadata)
			ds.Metadata["shard_count"] = len(shardDefs)
			if err := store.UpdateMetadata(context.Background(), ds.ID, ds.Metadata); err != nil {
				return errors.WrapE(err, "update shard_count")
			}

			fmt.Printf("created dataset %s (%d files, %d shards)\n", name, len(files), len(shardDefs))
			fmt.Fprintf(os.Stdout, `{"dataset_id":%d}`+"\n", ds.ID)
			return nil
		},
	}
	cmd.Flags().String("root-dir", "", "root directory to scan recursively")
	cmd.Flags().String("name", "", "dataset name (default: last component of root-dir)")
	cmd.Flags().String("format", "bin", "shard format: bin / tar / iso")
	cmd.Flags().String("compress", "", "compression: zstd / xz / segment:zstd / segment:xz")
	cmd.Flags().Int64("shard-max-bytes", 0, "max shard size in bytes (0=unlimited)")
	cmd.Flags().Bool("gen-only", false, "scan only, skip shard generation")
	return cmd
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
