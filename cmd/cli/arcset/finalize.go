package arcset

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	datasetpkg "github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func finalizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "finalize",
		Short: "Finalize arcset: seal DB and mark ready",
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

			arcStore := arcset.NewSQLiteStore(sqlDB)
			a, err := arcStore.FindByID(context.Background(), id)
			if err != nil {
				return errors.WrapE(err, "find arcset")
			}

			// 校验所有 shard
			shardStore := shard.NewSQLiteStore(sqlDB)
			shards, err := shardStore.FindByArcset(context.Background(), a.ID)
			if err != nil {
				return errors.WrapE(err, "find shards")
			}

			for _, sh := range shards {
				if err := validateShardFile(a.CurrentPath, sh.FilePath, sh.Checksum); err != nil {
					return err
				}
			}
			fmt.Printf("all %d shards validated\n", len(shards))

			// 复制 SQLite DB 到 current_path
			dbPath := os.Getenv("SQLITE_DB")
			if dbPath == "" {
				home, _ := os.UserHomeDir()
				dbPath = filepath.Join(home, "data", "packfs.db")
			}
			targetDB := filepath.Join(a.CurrentPath, "packfs.db")
			if err := copyFile(dbPath, targetDB); err != nil {
				return errors.WrapE(err, "copy database to arcset dir")
			}

			// 在新 DB 中归一 arcset ID
			newDB, err := sql.Open("sqlite3", targetDB)
			if err != nil {
				return errors.WrapE(err, "open target database")
			}
			if _, err := newDB.ExecContext(context.Background(),
				`UPDATE t_arcset SET id = 1, current_path = '.' WHERE id = ?`, a.ID); err != nil {
				newDB.Close()
				return errors.WrapE(err, "normalize arcset id")
			}
			newDB.Close()

			// 标记 arcset 状态
			ready := "ready"
			if err := arcStore.Update(context.Background(), a.Name, arcset.Update{Status: &ready}); err != nil {
				return errors.WrapE(err, "update arcset status")
			}

			// 标记关联 dataset 为 archived
			dsStore := datasetpkg.NewSQLiteStore(sqlDB)
			refs, err := arcStore.ListDatasetRefs(context.Background(), a.ID)
			if err != nil {
				return errors.WrapE(err, "list dataset refs")
			}
			for _, ref := range refs {
				if err := dsStore.UpdateStatus(context.Background(), ref.ID, "archived"); err != nil {
					return errors.WrapE(err, "update dataset status", "dataset", ref.Name)
				}
			}

			fmt.Printf("arcset %s finalized: db=%s, %d shards, %d datasets archived\n",
				a.Name, targetDB, len(shards), len(refs))
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID")
	return cmd
}

func validateShardFile(currentPath, filePath, expected string) error {
	absPath := filepath.Join(currentPath, filePath)
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
		return errors.E("shard checksum mismatch", "file", filePath,
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
