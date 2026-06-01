package shard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate shard checksum",
		RunE: func(cmd *cobra.Command, args []string) error {
			shardFile, _ := cmd.Flags().GetString("shard-file")
			if shardFile == "" {
				return errors.NewUsage("--shard-file is required")
			}
			arcsetID, _ := cmd.Flags().GetInt("arcset-id")

			f, err := os.Open(shardFile)
			if err != nil {
				return errors.WrapE(err, "open shard file")
			}
			defer f.Close()

			h := sha256.New()
			fileSize, err := io.Copy(h, f)
			if err != nil {
				return errors.WrapE(err, "read shard file")
			}
			actual := fmt.Sprintf("%x", h.Sum(nil))

			ext := strings.ToLower(filepath.Ext(shardFile))
			if ext == ".bin" || ext == ".pak" {
				if arcsetID <= 0 {
					return errors.NewUsage("--arcset-id is required for bin format shards")
				}
				sqlDB, err := db.OpenSQLite()
				if err != nil {
					return errors.WrapE(err, "open database")
				}
				defer sqlDB.Close()

				store := shard.NewSQLiteStore(sqlDB)
				sh, err := store.FindByArcsetAndFilePath(context.Background(),
					arcsetID, filepath.Base(shardFile))
				if err != nil {
					return errors.WrapE(err, "find shard record")
				}

				fmt.Printf("file_size:    %d (db) / %d (disk)\n", sh.FileSize, fileSize)
				fmt.Printf("sha256 (db):  %s\n", sh.Checksum)
				fmt.Printf("sha256 (disk): %s\n", actual)

				if sh.Checksum != actual {
					return errors.E("checksum mismatch",
						"expected", sh.Checksum, "actual", actual)
				}
				fmt.Println("OK: checksum matches")
			} else {
				fmt.Printf("sha256 (disk): %s (%d bytes)\n", actual, fileSize)
			}

			return nil
		},
	}
	cmd.Flags().String("shard-file", "", "shard file to validate")
	cmd.Flags().Int("arcset-id", 0, "arcset ID (required for bin format)")
	return cmd
}
