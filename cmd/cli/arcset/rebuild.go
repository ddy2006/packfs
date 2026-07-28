package arcset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func rebuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild EC shards after changing arcset parameters",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}
			ecStr, _ := cmd.Flags().GetString("ec")
			tapeCount, _ := cmd.Flags().GetInt("tape-count")

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			arcStore := arcset.NewSQLiteStore(sqlDB)
			a, err := arcStore.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}
			if a.Status != "building" && a.Status != "ready" {
				return errors.E("arcset status must be building or ready", "status", a.Status)
			}

			// Update metadata if new params provided
			meta := a.Metadata
			if meta == nil {
				meta = make(map[string]any)
			}
			if ecStr != "" {
				ecCfg, err := ec.ParseConfig(ecStr)
				if err != nil {
					return errors.NewUsage(fmt.Sprintf("invalid --ec: %v", err))
				}
				if err := ecCfg.Validate(); err != nil {
					return errors.NewUsage(fmt.Sprintf("invalid --ec: %v", err))
				}
				meta["ec"] = ecStr
			}
			if tapeCount > 0 {
				meta["tape_count"] = tapeCount
			}
			if ecStr != "" || tapeCount > 0 {
				if err := arcStore.Update(context.Background(), a.Name, arcset.Update{Metadata: meta}); err != nil {
					return errors.WrapE(err, "update arcset metadata")
				}
			}

			// Delete old EC/PAD shard records and their DB entries
			shardStore := shard.NewSQLiteStore(sqlDB)
			allShards, err := shardStore.FindByArcset(context.Background(), a.ID)
			if err != nil {
				return errors.WrapE(err, "find arcset shards")
			}
			for _, sh := range allShards {
				if sh.Type == "EC" || sh.Type == "PAD" {
					// Delete physical file
					absPath := filepath.Join(a.CurrentPath, sh.FilePath)
					if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
						logrus.Warnf("remove EC/PAD file %s: %v", absPath, err)
					}
					// Delete DB record
					if _, err := sqlDB.ExecContext(context.Background(),
						`DELETE FROM t_shard WHERE id = ?`, sh.ID); err != nil {
						return errors.WrapE(err, "delete EC/PAD shard record", "id", sh.ID)
					}
				}
			}

			// Reset arcset status to building if it was ready
			if a.Status == "ready" {
				building := "building"
				if err := arcStore.Update(context.Background(), a.Name, arcset.Update{Status: &building}); err != nil {
					return errors.WrapE(err, "reset arcset status")
				}
			}

			// Re-run EC
			if err := shard.MakeECShard(context.Background(), sqlDB, a.ID); err != nil {
				return err
			}

			fmt.Printf("rebuild complete for arcset %s\n", a.Name)
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID")
	cmd.Flags().String("ec", "", "new EC config (k+m, e.g. 8+4)")
	cmd.Flags().Int("tape-count", 0, "new tape count")
	return cmd
}
