// EC App: 对 shard 文件执行纠删码编码、校验和恢复测试。
//
// 默认读取同目录下的 ec.def 配置文件，命令行参数可覆盖。
//
// Usage:
//
//	go run .                        # 使用 ec.def 中的默认参数
//	go run . --ec=8+4 --recover     # 命令行覆盖 k/m
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ddy2006/packfs/internal/ec"
	"gopkg.in/yaml.v3"
)

type defConfig struct {
	EC       string `yaml:"ec"`
	ShardDir string `yaml:"shard-dir"`
	Recover  bool   `yaml:"recover"`
}

func main() {
	ecConfig := flag.String("ec", "", "EC 参数 k+m (覆盖 ec.def)")
	shardDir := flag.String("shard-dir", "", "shard 文件目录 (覆盖 ec.def)")
	recover := flag.Bool("recover", false, "故障恢复测试")
	defFile := flag.String("def", "ec.def", "配置文件路径")
	flag.Parse()

	// 读配置文件。
	var def defConfig
	data, err := os.ReadFile(*defFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read def file: %v\n", err)
		os.Exit(1)
	}
	if err := yaml.Unmarshal(data, &def); err != nil {
		fmt.Fprintf(os.Stderr, "parse def file: %v\n", err)
		os.Exit(1)
	}

	// 命令行覆盖。
	if *ecConfig == "" {
		*ecConfig = def.EC
	}
	if *shardDir == "" {
		*shardDir = def.ShardDir
	}
	if !*recover {
		*recover = def.Recover
	}

	cfg, err := ec.ParseConfig(*ecConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "EC config %q: %v\n", *ecConfig, err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "EC config %q: %v\n", *ecConfig, err)
		os.Exit(1)
	}

	// 发现 shard 文件。
	dir := *shardDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(*defFile), dir)
	}
	files := listShardFiles(dir)
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no shard files in %s\n", dir)
		os.Exit(1)
	}

	fmt.Printf("EC Config:    k=%d, m=%d (%s)\n", cfg.K, cfg.M, *ecConfig)
	fmt.Printf("Shard files:  %d in %s\n", len(files), dir)
	if len(files)%cfg.K != 0 {
		fmt.Printf("  (not divisible by k=%d, last stripe auto-padded)\n", cfg.K)
	}

	output := filepath.Join(dir, "ec-out")
	os.MkdirAll(output, 0755)

	// ── PlanStripes ──
	groups, err := ec.PlanStripes(files, cfg, output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "PlanStripes: %v\n", err)
		os.Exit(1)
	}
	n := len(groups)
	fmt.Printf("Stripes:      %d (%d shards/stripe)\n\n", n, cfg.Total())

	// ── EncodeStripe + VerifyStripe ──
	var results []*ec.StripeResult
	okCount := 0
	for i, g := range groups {
		res, err := ec.EncodeStripe(g, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stripe %d/%d: %v\n", i+1, n, err)
			os.Exit(1)
		}
		results = append(results, res)
		if v, _ := ec.VerifyStripe(g, cfg); v {
			okCount++
		} else {
			fmt.Fprintf(os.Stderr, "stripe %d/%d Verify FAILED\n", i+1, n)
		}
	}
	fmt.Printf("Encode+Verify: %d/%d stripes OK (PaddedSize=%d)\n",
		okCount, n, results[0].PaddedSize)

	// ── Recover (可选) ──
	if *recover {
		fmt.Println("\n--- Recovery: deleting position-1 data shard from each stripe ---")
		for i := range groups {
			os.Remove(groups[i][0].NewPath)
		}
		recoverOK := 0
		for i, g := range groups {
			if err := ec.ReconstructStripe(g, cfg, results[i].OriginalSizes, results[i].PaddedSize); err != nil {
				fmt.Fprintf(os.Stderr, "stripe %d/%d Reconstruct: %v\n", i+1, n, err)
				continue
			}
			if v, _ := ec.VerifyStripe(g, cfg); v {
				recoverOK++
			}
		}
		fmt.Printf("Recover: %d/%d stripes OK\n", recoverOK, n)
	}

	// ── 统计 ──
	var dataBytes, ecBytes int64
	ents, _ := os.ReadDir(output)
	for _, e := range ents {
		info, _ := e.Info()
		if len(e.Name()) > 1 && e.Name()[1] == 'E' {
			ecBytes += info.Size()
		} else {
			dataBytes += info.Size()
		}
	}
	fmt.Printf("\n%s: %d files | Data: %d KB | EC: %d KB | Overhead: %.1f%%\n",
		output, len(ents), dataBytes>>10, ecBytes>>10,
		float64(ecBytes)/float64(dataBytes+1)*100)
}

func listShardFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) == ".def" {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files
}
