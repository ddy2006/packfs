// Package fs provides CLI commands for filesystem management.
package fs

import "github.com/spf13/cobra"

// Command returns the fs parent command with all subcommands registered.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fs",
		Short: "Manage filesystems",
	}

	cmd.AddCommand(mountCmd())
	cmd.AddCommand(umountCmd())
	cmd.AddCommand(fsckCmd())
	return cmd
}

func mountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mount",
		Short: "Mount packfs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().String("mount-point", "", "mount point directory")
	cmd.Flags().Int("arcset-id", 0, "arcset ID to mount")
	return cmd
}

func umountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "umount",
		Short: "Unmount packfs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().String("mount-point", "", "mount point directory")
	return cmd
}

func fsckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fsck",
		Short: "Check filesystem integrity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().Int("arcset-id", 0, "arcset ID to check")
	return cmd
}
