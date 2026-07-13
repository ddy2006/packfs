package dataset

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func finalizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "finalize",
		Short: "Finalize dataset: seal DB and mark archived",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("id")
			if id <= 0 {
				return errors.NewUsage("--id is required")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			dsStore := dataset.NewSQLiteStore(sqlDB)
			ds, err := dsStore.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find dataset")
			}

			sourceRoot, _ := cmd.Flags().GetString("source-root")
			if sourceRoot == "" {
				sourceRoot = ds.CurrentPath
			}

			// Validate all shards
			shardStore := shard.NewSQLiteStore(sqlDB)
			shards, err := shardStore.FindByDataset(context.Background(), ds.ID)
			if err != nil {
				return errors.WrapE(err, "find shards")
			}

			for _, sh := range shards {
				absPath := filepath.Join(sourceRoot, sh.FilePath)
				if err := validateShardFile(absPath, sh.Checksum); err != nil {
					return err
				}
			}
			fmt.Printf("all %d shards validated\n", len(shards))

			// Copy SQLite DB to current_path
			dbPath := os.Getenv("SQLITE_DB")
			if dbPath == "" {
				home, _ := os.UserHomeDir()
				dbPath = filepath.Join(home, "data", "packfs.db")
			}
			targetDB := filepath.Join(ds.CurrentPath, "packfs.db")
			if err := copyFile(dbPath, targetDB); err != nil {
				return errors.WrapE(err, "copy database to dataset dir")
			}

			// Normalize dataset ID in new DB
			newDB, err := sql.Open("sqlite3", targetDB)
			if err != nil {
				return errors.WrapE(err, "open target database")
			}
			if _, err := newDB.ExecContext(context.Background(),
				`UPDATE t_dataset SET id = 1, current_path = '.' WHERE id = ?`, ds.ID); err != nil {
				newDB.Close()
				return errors.WrapE(err, "normalize dataset id")
			}
			newDB.Close()

			// Mark dataset as archived
			if err := dsStore.UpdateStatus(context.Background(), ds.ID, "archived"); err != nil {
				return errors.WrapE(err, "update dataset status")
			}

			fmt.Printf("dataset %s finalized: db=%s, %d shards\n", ds.Name, targetDB, len(shards))
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "dataset ID")
	cmd.Flags().String("source-root", "", "root directory where shard files reside (default: dataset current_path)")
	return cmd
}

func validateShardFile(absPath, expected string) error {
	f, err := os.Open(absPath)
	if err != nil {
		return errors.WrapE(err, "open shard", "path", absPath)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.WrapE(err, "read shard", "path", absPath)
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return errors.E("shard checksum mismatch", "file", absPath,
			"expected", expected, "actual", actual)
	}
	return nil
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Sync()
}
