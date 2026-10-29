// Package fs provides CLI commands for FUSE filesystem management.
package fs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	pfuse "github.com/ddy2006/packfs/internal/fuse"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// Command returns the fs parent command with all subcommands registered.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fs",
		Short: "Manage FUSE filesystems",
	}
	cmd.AddCommand(mountCmd())
	cmd.AddCommand(umountCmd())
	cmd.AddCommand(listCmd())
	cmd.AddCommand(fsckCmd())
	return cmd
}

func mountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mount",
		Short: "Mount a dataset or arcset as a read-only filesystem",
		RunE: func(cmd *cobra.Command, args []string) error {
			mountPoint, _ := cmd.Flags().GetString("mount-point")
			if mountPoint == "" {
				return errors.NewUsage("--mount-point is required")
			}
			datasetDBPath, _ := cmd.Flags().GetString("dataset-db")
			arcsetID, _ := cmd.Flags().GetInt64("arcset-id")
			if (datasetDBPath == "") == (arcsetID == 0) {
				return errors.NewUsage("exactly one of --dataset-db or --arcset-id is required")
			}
			absMount, err := filepath.Abs(mountPoint)
			if err != nil {
				return errors.WrapE(err, "resolve mount point")
			}
			if err := os.MkdirAll(absMount, 0755); err != nil {
				return errors.WrapE(err, "create mount point")
			}

			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			store := arcset.NewPGStore(pool)

			var (
				server     *gofuse.Server
				mountRow   *arcset.MountRow
				datasetDB  *sql.DB
				cleanupErr error
			)
			if datasetDBPath != "" {
				absDB, err := filepath.Abs(datasetDBPath)
				if err != nil {
					return errors.WrapE(err, "resolve dataset sqlite path")
				}
				datasetDB, err = db.OpenDatasetSQLite(absDB, true)
				if err != nil {
					return errors.WrapE(err, "open dataset sqlite")
				}
				defer datasetDB.Close()
				ds, err := findDataset(ctx, datasetDB)
				if err != nil {
					return err
				}
				rel, _, err := store.FindRelationByDatasetUID(ctx, ds.UID)
				if err != nil {
					return errors.WrapE(err, "find dataset relation")
				}
				if rel != nil {
					return errors.E("dataset is absorbed; mount the arcset instead",
						"arcset_id", rel.ArcsetID)
				}
				shardRoot, _ := ds.Metadata["shard_root"].(string)
				if shardRoot == "" {
					shardRoot = ds.CurrentPath
				}
				compress, _ := ds.Metadata["compress"].(string)
				mountRow, err = store.AddMount(ctx, absMount, "dataset", &ds.UID, nil)
				if err != nil {
					return errors.WrapE(err, "record fs mount")
				}
				server, err = pfuse.Mount(datasetDB, ds.ID, shardRoot, compress, absMount)
			} else {
				a, err := store.FindByID(ctx, arcsetID)
				if err != nil {
					return errors.WrapE(err, "find arcset")
				}
				arcID := a.ID
				mountRow, err = store.AddMount(ctx, absMount, "arcset", nil, int64Ptr(int64(arcID)))
				if err != nil {
					return errors.WrapE(err, "record fs mount")
				}
				server, err = pfuse.MountArcset(ctx, pool, arcsetID, absMount)
			}
			if err != nil {
				if mountRow != nil {
					cleanupErr = store.DeleteMount(ctx, absMount)
				}
				if cleanupErr != nil {
					logrus.Warnf("cleanup mount record: %v", cleanupErr)
				}
				return errors.WrapE(err, "mount FUSE")
			}
			defer func() {
				if err := store.DeleteMount(context.Background(), absMount); err != nil {
					logrus.Warnf("delete mount record: %v", err)
				}
			}()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				logrus.Info("unmounting...")
				server.Unmount()
			}()
			server.Wait()
			fmt.Fprintf(os.Stderr, "Unmounted.\n")
			return nil
		},
	}
	cmd.Flags().String("dataset-db", "", "dataset SQLite to mount (unabsorbed)")
	cmd.Flags().Int64("arcset-id", 0, "arcset ID to mount")
	cmd.Flags().String("mount-point", "", "mount point directory")
	return cmd
}

func umountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "umount",
		Short: "Unmount a packfs filesystem",
		RunE: func(cmd *cobra.Command, args []string) error {
			mountPoint, _ := cmd.Flags().GetString("mount-point")
			if mountPoint == "" {
				return errors.NewUsage("--mount-point is required")
			}
			abs, err := filepath.Abs(mountPoint)
			if err != nil {
				return err
			}
			var cmdErr error
			if _, lookErr := exec.LookPath("fusermount"); lookErr == nil {
				cmdErr = exec.Command("fusermount", "-u", abs).Run()
			} else {
				cmdErr = exec.Command("umount", abs).Run()
			}
			if cmdErr != nil {
				return errors.WrapE(cmdErr, "OS unmount", "mount_point", abs)
			}
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			if err := arcset.NewPGStore(pool).DeleteMount(ctx, abs); err != nil {
				return errors.WrapE(err, "delete mount record")
			}
			return nil
		},
	}
	cmd.Flags().String("mount-point", "", "mount point directory")
	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recorded mounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			mounts, err := arcset.NewPGStore(pool).ListMounts(ctx)
			if err != nil {
				return err
			}
			for _, m := range mounts {
				fmt.Printf("%s\t%s\t", m.MountPoint, m.Type)
				if m.DatasetUID != nil {
					fmt.Printf("dataset=%s", *m.DatasetUID)
				}
				if m.ArcsetID != nil {
					fmt.Printf("arcset=%d", *m.ArcsetID)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

func fsckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fsck",
		Short: "Check filesystem integrity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.E("not implemented")
		},
	}
	cmd.Flags().Int64("arcset-id", 0, "arcset ID to check")
	return cmd
}

func findDataset(ctx context.Context, sqlDB *sql.DB) (*dataset.Dataset, error) {
	store := dataset.NewDatasetStore(sqlDB)
	limit := 1
	all, err := store.Find(ctx, dataset.Filter{Limit: &limit})
	if err != nil {
		return nil, errors.WrapE(err, "list datasets")
	}
	if len(all) == 0 {
		return nil, errors.E("dataset sqlite contains no dataset")
	}
	return all[0], nil
}

func int64Ptr(v int64) *int64 { return &v }
