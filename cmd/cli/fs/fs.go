// Package fs provides CLI commands for FUSE filesystem management.
package fs

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	pfuse "github.com/ddy2006/packfs/internal/fuse"
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
	cmd.AddCommand(fsckCmd())
	return cmd
}

func mountCmd() *cobra.Command {
	var (
		datasetID  int
		mountPoint string
	)

	cmd := &cobra.Command{
		Use:   "mount",
		Short: "Mount a dataset as a read-only FUSE filesystem",
		Long: `Mount a packfs dataset as a read-only virtual filesystem via FUSE.

Files are served directly from shard files using the DB index.
Only bin format without shard-level compression supports efficient random access.

Examples:
  packfs fs mount --dataset-id=1 --mount-point=/mnt/packfs
  packfs fs mount -d 1 -m /tmp/pk`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if datasetID <= 0 {
				return errors.E("--dataset-id is required")
			}
			if mountPoint == "" {
				return errors.E("--mount-point is required")
			}

			if err := os.MkdirAll(mountPoint, 0755); err != nil {
				return errors.WrapE(err, "create mount point")
			}

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			ctx := context.Background()
			dsStore := dataset.NewSQLiteStore(sqlDB)
			ds, err := dsStore.FindByID(ctx, datasetID)
			if err != nil {
				return errors.WrapE(err, "find dataset", "id", datasetID)
			}

			compress, _ := ds.Metadata["compress"].(string)
			format, _ := ds.Metadata["format"].(string)
			if format == "" {
				format = "bin"
			}

			if format != "bin" {
				fmt.Fprintf(os.Stderr, "Warning: format=%s, FUSE optimized for bin\n", format)
			}
			if compress == "zstd" || compress == "xz" {
				fmt.Fprintf(os.Stderr, "Warning: shard-level compression (%s) requires full decompression per read\n", compress)
			}

			fmt.Fprintf(os.Stderr, "Mounting dataset %d (%s) at %s\n", ds.ID, ds.Name, mountPoint)
			fmt.Fprintf(os.Stderr, "  format=%s, compress=%s, current_path=%s\n", format, compress, ds.CurrentPath)

			server, err := pfuse.Mount(sqlDB, ds.ID, ds.CurrentPath, compress, mountPoint)
			if err != nil {
				return errors.WrapE(err, "mount FUSE")
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			fmt.Fprintf(os.Stderr, "Mounted. Press Ctrl+C to unmount.\n")

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

	cmd.Flags().IntVarP(&datasetID, "dataset-id", "d", 0, "dataset ID to mount")
	cmd.Flags().StringVarP(&mountPoint, "mount-point", "m", "", "mount point directory")
	return cmd
}

func umountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "umount",
		Short: "Unmount a packfs filesystem",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.E("not implemented: use fusermount -u <mount-point> or umount <mount-point>")
		},
	}
	return cmd
}

func fsckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fsck",
		Short: "Check filesystem integrity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.E("not implemented")
		},
	}
	cmd.Flags().Int("arcset-id", 0, "arcset ID to check")
	return cmd
}
