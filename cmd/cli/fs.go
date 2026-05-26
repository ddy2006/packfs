package main

import "github.com/spf13/cobra"

var fsCmd = &cobra.Command{
	Use:   "fs",
	Short: "Manage filesystems",
}

// packfs fs mount --mount-point=<dir> --arcset-id=<int>
var mountCmd = &cobra.Command{
	Use:   "mount",
	Short: "Mount packfs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// packfs fs umount --mount-point=<dir>
var umountCmd = &cobra.Command{
	Use:   "umount",
	Short: "Unmount packfs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// packfs fs fsck --arcset-id=<int>
var fsckCmd = &cobra.Command{
	Use:   "fsck",
	Short: "Check filesystem integrity",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	fsCmd.AddCommand(mountCmd)
	mountCmd.Flags().String("mount-point", "", "mount point directory")
	mountCmd.Flags().Int("arcset-id", 0, "arcset ID to mount")

	fsCmd.AddCommand(umountCmd)
	umountCmd.Flags().String("mount-point", "", "mount point directory")

	fsCmd.AddCommand(fsckCmd)
	fsckCmd.Flags().Int("arcset-id", 0, "arcset ID to check")

	rootCmd.AddCommand(fsCmd)
}
