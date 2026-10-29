package arcset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/recoverec"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func recoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Recover a missing shard from an EC stripe",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			shardFile, _ := cmd.Flags().GetString("shard-file")
			if shardFile == "" {
				return errors.NewUsage("--shard-file is required")
			}
			target, _ := cmd.Flags().GetString("target")
			chunkStr, _ := cmd.Flags().GetString("ec-chunk-size")
			chunk, err := parseBytes(chunkStr)
			if err != nil {
				return errors.NewUsage(fmt.Sprintf("invalid --ec-chunk-size: %v", err))
			}
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			if err := recoverec.Run(ctx, pool, recoverec.Options{
				ArcsetID:  id,
				ShardFile: shardFile,
				Target:    target,
				ChunkSize: int(chunk),
			}); err != nil {
				return errors.WrapE(err, "recover")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "recovered"})
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("shard-file", "", "relative shard path to recover")
	cmd.Flags().String("target", "all", "recover data or all")
	cmd.Flags().String("ec-chunk-size", "8MiB", "EC chunk size (1MiB..64MiB)")
	_ = ec.DefaultChunkSize
	return cmd
}
