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

var makeShardCmd = &cobra.Command{
	Use:   "make-shard",
	Short: "Make shard",
	RunE: func(cmd *cobra.Command, args []string) error {
		sourcePath, err := param.GetString(cmd, "source-path", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter source-path failed")
		}
		targetPath, err := param.GetString(cmd, "target-path", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter target-path failed")
		}
		defFile, err := param.GetString(cmd, "def-file", param.WithRequired())
		if err != nil {
			return errors.WrapE(err, 1, "get parameter def-file failed")
		}

		err = doMakeShard(sourcePath, targetPath, defFile)
		return errors.WrapE(err, "doMakeShard()")
	},
}

// 依照defFile的内容，将源路径sourcePath中多个文件，合并为目标路径targetPath下单个文件（Shard），文件名defFile去掉后缀def。
// sourcePath
// targetPath
// defFile格式： 文本文件
// 文件名：xxxx.bin.def
// 每行内容：相对sourcePath的相对路径文件名
func doMakeShard(sourcePath, targetPath, defFile string) error {
	fmt.Println(sourcePath, targetPath, defFile)

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
	shardPath := filepath.Join(targetPath, shardName)

	out, err := os.Create(shardPath)
	if err != nil {
		return errors.WrapE(err, "create shard file failed")
	}
	defer out.Close()

	for _, relPath := range lines {
		srcPath := filepath.Join(sourcePath, relPath)
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

func init() {
	rootCmd.AddCommand(makeShardCmd)
	makeShardCmd.Flags().String("source-path", "", "source-path for make-shard")
	makeShardCmd.Flags().String("target-path", "", "target-path for make-shard")
	makeShardCmd.Flags().String("def-file", "", "def-file for make-shard")
}
