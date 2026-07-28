package shard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// MakeECShard runs EC encoding for all data shards linked to an arcset.
//
// Flow:
//  1. Load arcset EC config
//  2. Collect data shard paths from all linked datasets
//  3. Plan EC stripes (naming + grouping)
//  4. Encode each stripe (read data, write EC, rename data files)
//  5. Update t_shard records (data shard → arcset FK + new path; EC/PAD → new records)
//  6. Mark linked datasets as "absorbed"
func MakeECShard(ctx context.Context, db *sql.DB, arcsetID int) error {
	arcStore := arcset.NewSQLiteStore(db)
	dsStore := dataset.NewSQLiteStore(db)
	shardStore := NewSQLiteStore(db)

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

	// 2. Collect data shard paths from linked datasets
	refs, err := arcStore.ListDatasetRefs(ctx, a.ID)
	if err != nil {
		return errors.WrapE(err, "list dataset refs")
	}
	if len(refs) == 0 {
		return errors.E("arcset has no datasets, run arcset append first")
	}

	type shardRef struct {
		ShardID int
	}
	var dataFiles []string
	var shardRefs []shardRef

	for _, ref := range refs {
		ds, err := dsStore.FindByID(ctx, ref.ID)
		if err != nil {
			return errors.WrapE(err, "find dataset", "id", ref.ID)
		}
		shards, err := shardStore.FindByDataset(ctx, ds.ID)
		if err != nil {
			return errors.WrapE(err, "find shards for dataset", "id", ds.ID)
		}
		for _, sh := range shards {
			if sh.Type != "DATA" {
				continue
			}
			// After a previous make-ec, shard files live in arcset.CurrentPath.
			// For first make-ec, shard files are still in ds.CurrentPath.
			srcDir := ds.CurrentPath
			if sh.Arcset.Valid {
				srcDir = a.CurrentPath
			}
			absPath := filepath.Join(srcDir, sh.FilePath)
			dataFiles = append(dataFiles, absPath)
			shardRefs = append(shardRefs, shardRef{ShardID: sh.ID})
		}
	}
	if len(dataFiles) == 0 {
		return errors.E("no data shards found in linked datasets")
	}

	logrus.Infof("make-ec: %d data shards, EC config %s", len(dataFiles), ecCfg)

	// 3. Plan EC stripes
	groups, err := ec.PlanStripes(dataFiles, ecCfg, a.CurrentPath)
	if err != nil {
		return errors.WrapE(err, "plan EC stripes")
	}

	// 4. Encode each stripe and update DB
	fileIdx := 0
	for _, stripeFiles := range groups {
		result, err := ec.EncodeStripe(stripeFiles, ecCfg)
		if err != nil {
			return errors.WrapE(err, "encode stripe", "stripe", stripeFiles[0].Stripe)
		}

		for _, sf := range stripeFiles {
			relPath := filepath.Base(sf.NewPath)

			if sf.IsData() && sf.OrigPath != "" {
				ref := shardRefs[fileIdx]
				fileIdx++

				meta := map[string]any{
					"stripe":        sf.Stripe,
					"position":      sf.Position,
					"padded_size":   result.PaddedSize,
					"original_size": result.OriginalSizes[sf.Position-1],
				}
				metaJSON, _ := json.Marshal(meta)
				if _, err := db.ExecContext(ctx,
					`UPDATE t_shard SET file_path = ?, arcset = ?, metadata = ? WHERE id = ?`,
					relPath, arcsetID, metaJSON, ref.ShardID); err != nil {
					return errors.WrapE(err, "update data shard", "id", ref.ShardID)
				}
			} else {
				shType := "EC"
				if sf.Type == "D" && sf.OrigPath == "" {
					shType = "PAD"
				}
				sh := &Shard{
					FilePath: relPath,
					Type:     shType,
					Arcset:   sql.NullInt64{Int64: int64(arcsetID), Valid: true},
					Metadata: map[string]any{
						"stripe":        sf.Stripe,
						"position":      sf.Position,
						"padded_size":   result.PaddedSize,
						"original_size": int64(0),
					},
				}
				if err := shardStore.CreateShard(ctx, sh); err != nil {
					return errors.WrapE(err, "create EC/PAD shard record", "path", relPath)
				}
			}
		}
	}

	// 5. Mark linked datasets as absorbed
	for _, ref := range refs {
		if err := dsStore.UpdateStatus(ctx, ref.ID, "absorbed"); err != nil {
			logrus.Warnf("update dataset %d status to absorbed: %v", ref.ID, err)
		}
	}

	fmt.Printf("make-ec complete: %d stripes, %d data shards, arcset %s\n",
		len(groups), len(dataFiles), a.Name)
	return nil
}
