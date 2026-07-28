package arcset

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func appendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "append",
		Short: "Append a dataset to an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			datasetID, _ := cmd.Flags().GetInt("dataset-id")
			if datasetID <= 0 {
				return errors.NewUsage("--dataset-id is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			arcStore := arcset.NewSQLiteStore(sqlDB)
			dsStore := dataset.NewSQLiteStore(sqlDB)

			a, err := arcStore.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}
			if a.Status != "building" {
				return errors.E("arcset is not in building state", "status", a.Status)
			}

			ds, err := dsStore.FindByID(context.Background(), datasetID)
			if err != nil {
				return errors.WrapE(err, "find dataset")
			}

			// Verify config compatibility
			if err := checkCompatibility(sqlDB, a, ds); err != nil {
				return err
			}

			// Inherit shard_max_bytes from first dataset if not set
			if err := inheritShardMaxBytes(arcStore, a, ds); err != nil {
				return err
			}

			// Link dataset to arcset
			if err := arcStore.AddDataset(context.Background(), a.ID, ds.ID); err != nil {
				return errors.WrapE(err, "link dataset to arcset")
			}

			fmt.Printf("appended dataset %s (id=%d) to arcset %s (id=%d)\n", ds.Name, ds.ID, a.Name, a.ID)
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID")
	cmd.Flags().Int("dataset-id", 0, "dataset ID")
	return cmd
}

func checkCompatibility(sqlDB *sql.DB, a *arcset.Arcset, ds *dataset.Dataset) error {
	// Check shard_max_bytes consistency
	arcShardMax := getInt64Meta(a.Metadata, "shard_max_bytes")
	dsShardMax := getInt64Meta(ds.Metadata, "shard_max_bytes")
	if arcShardMax > 0 && dsShardMax > 0 && arcShardMax != dsShardMax {
		return errors.E("shard_max_bytes mismatch",
			"arcset", arcShardMax, "dataset", dsShardMax)
	}

	// Check format/compress against existing linked datasets
	dsStore := dataset.NewSQLiteStore(sqlDB)
	arcStore := arcset.NewSQLiteStore(sqlDB)
	refs, err := arcStore.ListDatasetRefs(context.Background(), a.ID)
	if err != nil {
		return errors.WrapE(err, "list linked datasets")
	}
	if len(refs) > 0 {
		ref, err := dsStore.FindByID(context.Background(), refs[0].ID)
		if err != nil {
			return errors.WrapE(err, "find linked dataset")
		}
		if refFmt, _ := ref.Metadata["format"].(string); refFmt != "" {
			if dsFmt, _ := ds.Metadata["format"].(string); dsFmt != "" && dsFmt != refFmt {
				return errors.E("format mismatch", "arcset", refFmt, "dataset", dsFmt)
			}
		}
		if refComp, _ := ref.Metadata["compress"].(string); refComp != "" {
			if dsComp, _ := ds.Metadata["compress"].(string); dsComp != "" && dsComp != refComp {
				return errors.E("compress mismatch", "arcset", refComp, "dataset", dsComp)
			}
		}
	}
	return nil
}

func inheritShardMaxBytes(arcStore arcset.Store, a *arcset.Arcset, ds *dataset.Dataset) error {
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}
	if _, ok := a.Metadata["shard_max_bytes"]; !ok {
		if smb, ok := ds.Metadata["shard_max_bytes"]; ok {
			a.Metadata["shard_max_bytes"] = smb
			return arcStore.Update(context.Background(), a.Name, arcset.Update{Metadata: a.Metadata})
		}
	}
	return nil
}

func getInt64Meta(m map[string]any, key string) int64 {
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
