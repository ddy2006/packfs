package main

import "github.com/spf13/cobra"

// data-dir is in cluster storage.
// single-node version:
// 		make-archive -i /data-dir -o /archive-files-dir
// scalebox-based : send messages to scalebox server
//		make-archive -i /data-dir --scalebox-server=server-ip:port --sink-job-id=nn

// func main() {

// 扫描data-dir，获得所有文件的文件名、字节数、时间戳等

// 计算data-dir下的总文件数、总字节数

// 依据unit_bytes、fragment_bytes、redundancy，生成待处理的archive文件列表
//		实体文件通常不跨archive_unit

// 生成DATA类型的archive文件

// 生成CHECKSUM类型的archive文件

// 生成sqlite数据库文件到输出目录（元数据）

// }

var makeArcSetCmd = &cobra.Command{
	Use:   "make-arcset",
	Short: "Make arcset",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(makeArcSetCmd)
}
