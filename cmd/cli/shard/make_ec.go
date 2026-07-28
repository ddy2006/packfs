package shard

import (
	"context"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func makeECCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make-ec",
		Short: "Make erasure-coded shards for an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			arcsetID, _ := cmd.Flags().GetInt("arcset-id")
			if arcsetID <= 0 {
				return errors.NewUsage("--arcset-id is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			return shard.MakeECShard(context.Background(), sqlDB, arcsetID)
		},
	}
	cmd.Flags().Int("arcset-id", 0, "arcset ID")
	return cmd
}
