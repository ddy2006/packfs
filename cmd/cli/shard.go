package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/param"
	"github.com/spf13/cobra"
)

var shardCmd = &cobra.Command{
	Use:   "shard",
	Short: "Manage shards",
}

// packfs shard make --source-root=/absolute-path --target-root=/absolute-path --def-file=def-file
var makeShardCmd = &cobra.Command{
	Use:   "make",
	Short: "Make shard",
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceRoot, err := param.GetString(cmd, "source-root", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter source-root failed")
		}
		targetRoot, err := param.GetString(cmd, "target-root", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter target-root failed")
		}
		defFile, err := param.GetString(cmd, "def-file", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter def-file failed")
		}

		err = doMakeShard(sourceRoot, targetRoot, defFile)
		return errors.WrapE(err, "doMakeShard()")
	},
}

// defFile格式：文本文件，文件名 xxxx.bin.def，每行为相对 sourceRoot 的文件路径。
// 将 defFile 中列出的文件合并打包到 targetRoot 下的单个 shard 文件中。
func doMakeShard(sourceRoot, targetRoot, defFile string) error {
	fmt.Println(sourceRoot, targetRoot, defFile)

	f, err := os.Open(defFile)
	if err != nil {
		return errors.WrapE(err, "open def-file failed")
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.WrapE(err, "read def-file failed")
	}

	base := filepath.Base(defFile)
	shardName := strings.TrimSuffix(base, ".def")
	shardPath := filepath.Join(targetRoot, shardName)

	out, err := os.Create(shardPath)
	if err != nil {
		return errors.WrapE(err, "create shard file failed")
	}
	defer out.Close()

	for _, relPath := range lines {
		srcPath := filepath.Join(sourceRoot, relPath)
		src, err := os.Open(srcPath)
		if err != nil {
			return errors.WrapE(err, "open source file failed: %s", relPath)
		}
		_, err = io.Copy(out, src)
		src.Close()
		if err != nil {
			return errors.WrapE(err, "copy file failed: %s", relPath)
		}
	}

	return nil
}

// packfs shard unpack --shard-file=/absolute-path --target-root=/absolute-path
var unpackShardCmd = &cobra.Command{
	Use:   "unpack",
	Short: "Unpack shard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// packfs shard make-ec --def-file=yaml-file
var makeECShardCmd = &cobra.Command{
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

// packfs shard recover --ec-shard-file=/absolute-path --target-root=/absolute-path
var recoverShardCmd = &cobra.Command{
	Use:   "recover",
	Short: "Recover shard from EC",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	shardCmd.AddCommand(makeShardCmd)
	makeShardCmd.Flags().String("source-root", "", "source root directory")
	makeShardCmd.Flags().String("target-root", "", "target root directory")
	makeShardCmd.Flags().String("def-file", "", "shard definition file")

	shardCmd.AddCommand(unpackShardCmd)
	unpackShardCmd.Flags().String("shard-file", "", "shard file to unpack")
	unpackShardCmd.Flags().String("target-root", "", "target root directory")

	shardCmd.AddCommand(makeECShardCmd)
	makeECShardCmd.Flags().String("def-file", "", "EC definition YAML file")

	shardCmd.AddCommand(recoverShardCmd)
	recoverShardCmd.Flags().String("ec-shard-file", "", "EC shard file to recover from")
	recoverShardCmd.Flags().String("target-root", "", "target root directory")

	rootCmd.AddCommand(shardCmd)
}
