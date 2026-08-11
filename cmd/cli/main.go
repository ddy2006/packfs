package main

import (
	"os"

	"github.com/ddy2006/packfs/cmd/cli/arcset"
	"github.com/ddy2006/packfs/cmd/cli/dataset"
	"github.com/ddy2006/packfs/cmd/cli/fs"
	"github.com/ddy2006/packfs/cmd/cli/shard"
	"github.com/ddy2006/packfs/cmd/cli/webui"
)

func main() {
	rootCmd.AddCommand(dataset.Command())
	rootCmd.AddCommand(arcset.Command())
	rootCmd.AddCommand(shard.Command())
	rootCmd.AddCommand(fs.Command())
	rootCmd.AddCommand(webui.Command())
	os.Exit(executeRoot())
}
