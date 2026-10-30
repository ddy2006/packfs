package arcset

import (
	"context"
	"fmt"
	"os"

	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/makeec"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func makeECCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make-ec",
		Short: "Build EC and PAD shards for an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			chunkStr, _ := cmd.Flags().GetString("ec-chunk-size")
			chunk, err := arcsetops.ParseBytes(chunkStr)
			if err != nil {
				return errors.NewUsage(fmt.Sprintf("invalid --ec-chunk-size: %v", err))
			}
			if chunk > int64(^uint(0)>>1) {
				return errors.NewUsage("--ec-chunk-size is too large")
			}
			group, _ := cmd.Flags().GetInt("group")
			stripe, _ := cmd.Flags().GetInt("seq")
			parallel, _ := cmd.Flags().GetInt("ec-parallel")
			if parallel < 1 {
				return errors.NewUsage("--ec-parallel must be positive")
			}
			var groupPtr, stripePtr *int
			if group >= 0 {
				groupPtr = &group
			}
			if stripe >= 0 {
				stripePtr = &stripe
			}
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			if err := makeec.Run(ctx, pool, makeec.Options{
				ArcsetID:  id,
				ChunkSize: int(chunk),
				Group:     groupPtr,
				Stripe:    stripePtr,
				Parallel:  parallel,
			}); err != nil {
				return errors.WrapE(err, "make-ec")
			}
			fmt.Fprintln(os.Stdout, `{"status":"completed"}`)
			return nil
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID")
	cmd.Flags().String("ec-chunk-size", "8MiB", "EC chunk size (1MiB..64MiB)")
	cmd.Flags().Int("group", -1, "only process one partition group")
	cmd.Flags().Int("seq", -1, "only process one stripe seq")
	cmd.Flags().Int("ec-parallel", 1, "number of stripes to encode in parallel")
	_ = ec.DefaultChunkSize
	return cmd
}
