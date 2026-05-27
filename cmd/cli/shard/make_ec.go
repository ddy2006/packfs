package shard

import (
	"fmt"

	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/param"
	"github.com/spf13/cobra"
)

func makeECCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make-ec",
		Short: "Make erasure-coded shard",
		RunE: func(cmd *cobra.Command, args []string) error {
			defFile, err := param.GetString(cmd, "def-file", param.WithRequired())
			if err != nil {
				return errors.WrapE(err, 1, "get parameter def-file failed")
			}
			fmt.Println(defFile)
			return errors.WrapE(err, "doMakeECShard()")
		},
	}
	cmd.Flags().String("def-file", "", "EC definition YAML file")
	return cmd
}
