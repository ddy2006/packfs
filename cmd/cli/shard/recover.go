package shard

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func recoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Recover a lost shard from EC stripe",
		RunE: func(cmd *cobra.Command, args []string) error {
			shardFile, _ := cmd.Flags().GetString("shard-file")
			arcsetID, _ := cmd.Flags().GetInt("arcset-id")
			if arcsetID <= 0 {
				return errors.NewUsage("--arcset-id is required")
			}
			if shardFile == "" {
				return errors.NewUsage("--shard-file is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			return doRecoverShard(sqlDB, arcsetID, shardFile)
		},
	}
	cmd.Flags().Int("arcset-id", 0, "arcset ID")
	cmd.Flags().String("shard-file", "", "relative file path of lost shard")
	return cmd
}

func doRecoverShard(sqlDB *sql.DB, arcsetID int, lostPath string) error {
	ctx := context.Background()
	arcStore := arcset.NewSQLiteStore(sqlDB)
	shardStore := shard.NewSQLiteStore(sqlDB)

	// 1. Load arcset and EC config
	a, err := arcStore.FindByID(ctx, arcsetID)
	if err != nil {
		return errors.WrapE(err, "find arcset")
	}
	ecStr, ok := a.Metadata["ec"].(string)
	if !ok || ecStr == "" {
		return errors.E("arcset has no EC config")
	}
	ecCfg, err := ec.ParseConfig(ecStr)
	if err != nil {
		return errors.WrapE(err, "parse EC config")
	}

	// 2. Find the lost shard record
	lostShard, err := shardStore.FindByArcsetAndFilePath(ctx, arcsetID, lostPath)
	if err != nil {
		return errors.WrapE(err, "find lost shard record")
	}
	stripe := getMetaInt(lostShard.Metadata, "stripe")
	position := getMetaInt(lostShard.Metadata, "position")
	if stripe <= 0 || position <= 0 {
		return errors.E("lost shard has no stripe/position metadata", "path", lostPath)
	}
	paddedSize := getMetaInt64(lostShard.Metadata, "padded_size")

	// 3. Find all shards in the same arcset, filter by same stripe
	allShards, err := shardStore.FindByArcset(ctx, arcsetID)
	if err != nil {
		return errors.WrapE(err, "find arcset shards")
	}

	// Collect shards in the same stripe
	stripeFiles := make([]ec.StripeFile, ecCfg.Total())
	var originalSizes []int64
	for _, sh := range allShards {
		s := getMetaInt(sh.Metadata, "stripe")
		p := getMetaInt(sh.Metadata, "position")
		if s != stripe || p <= 0 || p > ecCfg.Total() {
			continue
		}
		idx := p - 1
		stripeFiles[idx] = ec.StripeFile{
			Type:     string(sh.Type[0]), // "DATA"→"D", "EC"→"E", "PAD"→"P"
			NewPath:  filepath.Join(a.CurrentPath, sh.FilePath),
			Stripe:   stripe,
			Position: p,
		}
		if sh.Type == "DATA" {
			os := getMetaInt64(sh.Metadata, "original_size")
			if originalSizes == nil {
				originalSizes = make([]int64, ecCfg.K)
			}
			if idx < ecCfg.K {
				originalSizes[idx] = os
			}
		}
	}

	// Mark lost shard as missing (keep NewPath so ReconstructStripe
	// knows where to write the recovered file; the file doesn't exist
	// on disk so ReconstructStripe's os.ReadFile will detect it as missing).
	lostIdx := position - 1
	stripeFiles[lostIdx].Type = string(lostShard.Type[0])

	fmt.Printf("recovering stripe %d position %d (file: %s), %d surviving shards found\n",
		stripe, position, lostPath, ecCfg.Total()-1)

	// 4. Reconstruct
	if err := ec.ReconstructStripe(stripeFiles, ecCfg, originalSizes, paddedSize); err != nil {
		return errors.WrapE(err, "reconstruct stripe")
	}

	// 5. Verify the recovered file
	recoveredPath := filepath.Join(a.CurrentPath, lostPath)
	ok2, err := ec.VerifyStripe(stripeFiles, ecCfg)
	if err != nil {
		return errors.WrapE(err, "verify stripe after recovery")
	}
	if !ok2 {
		return errors.E("stripe verification failed after recovery")
	}

	fmt.Printf("recovered %s successfully\n", recoveredPath)
	return nil
}

func getMetaInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func getMetaInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
