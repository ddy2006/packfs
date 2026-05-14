package main

import (
	"fmt"

	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/param"
	"github.com/spf13/cobra"
)

// 		make-ecshard json-file
// json file:
//		checksum-file:
//		algorithm: xxxx
//		archive-files
//			- archive-file-0
//			- archive-file-1
//			- archive-file-2

var makeECShardCmd = &cobra.Command{
	Use:   "make-ecshard",
	Short: "Make ecshard",
	RunE: func(cmd *cobra.Command, args []string) error {
		defFile, err := param.GetString(cmd, "def-file", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter def-file failed")
		}

		err = doMakeECShard(defFile)
		return errors.WrapE(err, "doMakeECShard()")
	},
}

func doMakeECShard(defFile string) error {
	fmt.Println(defFile)

	return nil
}

func init() {
	rootCmd.AddCommand(makeECShardCmd)
}
