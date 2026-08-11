// Package simulate 生成仿真数据文件，用于存储系统测试。
// 根据给定的时间范围、channel 范围、文件大小等参数，
// 在磁盘上生成一批随机内容的 .dat 文件。
package simulate

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaichao/gopkg/errors"
)

// Config 仿真数据生成参数。
// 字段与 examples/astro/dataset.def 中的 simulation 节对应。
type Config struct {
	// Name 数据集名称，文件将生成在 output_root/name/ 下。
	Name string `json:"name"`

	// StartTS / EndTS 时间片范围（包含两端）。
	StartTS int `json:"start_ts"`
	EndTS   int `json:"end_ts"`

	// ChStart / ChEnd channel 范围（包含两端）。
	ChStart int `json:"ch_start"`
	ChEnd   int `json:"ch_end"`

	// FileBytes 每个文件大小（字节），使用 crypto/rand 生成随机内容。
	FileBytes int `json:"file_bytes"`

	// OutputRoot 数据根目录，文件生成到 output_root/name/ 下。
	OutputRoot string `json:"output_root"`
}

// DefaultConfig 返回一个 SKA 天体物理默认预设配置。
func DefaultConfig() Config {
	return Config{
		Name:       "1177938016",
		StartTS:    1177940019,
		EndTS:      1177940098,
		ChStart:    133,
		ChEnd:      156,
		FileBytes:  10240,
		OutputRoot: "./data/dat",
	}
}

// Stats 仿真生成结果统计。
type Stats struct {
	FileCount int    `json:"file_count"`
	TotalBytes int64 `json:"total_bytes"`
	OutputDir  string `json:"output_dir"`
}

// Generate 根据配置在磁盘上生成仿真数据文件。
//
// 文件命名规则：{ts}_{next_ts}_ch{ch}.dat（与 simulate.sh 一致）。
// 所有文件内容相同（单次 crypto/rand 生成），保证速度。
func Generate(cfg Config) (Stats, error) {
	if cfg.Name == "" {
		return Stats{}, errors.E("name is required")
	}
	if cfg.FileBytes <= 0 {
		cfg.FileBytes = 1024
	}
	if cfg.OutputRoot == "" {
		cfg.OutputRoot = "./data/dat"
	}

	outputDir := filepath.Join(cfg.OutputRoot, cfg.Name)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return Stats{}, errors.WrapE(err, "create output dir", "path", outputDir)
	}

	// 预生成随机数据，所有文件共用
	data := make([]byte, cfg.FileBytes)
	if _, err := rand.Read(data); err != nil {
		return Stats{}, errors.WrapE(err, "generate random data")
	}

	var totalFiles int
	for ch := cfg.ChStart; ch <= cfg.ChEnd; ch++ {
		for ts := cfg.StartTS; ts <= cfg.EndTS; ts++ {
			nextTS := ts + 1
			fname := fmt.Sprintf("%d_%d_ch%d.dat", ts, nextTS, ch)
			fpath := filepath.Join(outputDir, fname)
			if err := os.WriteFile(fpath, data, 0644); err != nil {
				return Stats{}, errors.WrapE(err, "write file", "path", fpath)
			}
			totalFiles++
		}
	}

	return Stats{
		FileCount:  totalFiles,
		TotalBytes: int64(totalFiles) * int64(cfg.FileBytes),
		OutputDir:  outputDir,
	}, nil
}
