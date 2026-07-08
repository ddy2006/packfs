package ec_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ddy2006/packfs/internal/ec"
)

// TestECWorkflow demonstrates the complete EC generation workflow:
// PlanStripes → EncodeStripe (per stripe) → VerifyStripe.
//
// Scenario: 6 data shard files, k=3, m=2 → 2 stripes, each with 3 data + 2 EC.
func TestECWorkflow(t *testing.T) {
	cfg, _ := ec.ParseConfig("3+2")
	dir := t.TempDir()

	// ========================================================================
	// Step 0: 准备 6 个 data 文件（模拟 gen-def + shard make 的输出）
	// ========================================================================
	dataFiles := writeDataFiles(t, dir, []string{
		"0000.bin", "0001.bin", "0002.bin",
		"0003.bin", "0004.bin", "0005.bin",
	})
	t.Logf("Step 0: created %d data files in %s", len(dataFiles), dir)

	// ========================================================================
	// Step 1: PlanStripes — 文件命名规划
	// ========================================================================
	groups, err := ec.PlanStripes(dataFiles, cfg, dir)
	if err != nil {
		t.Fatalf("PlanStripes: %v", err)
	}
	t.Logf("\nStep 1: PlanStripes — %d data files → %d stripe(s)\n", len(dataFiles), len(groups))

	for i, g := range groups {
		t.Logf("  Stripe %d (k=%d, m=%d):", i+1, cfg.K, cfg.M)
		for _, sf := range g {
			action := "[NEW]   "
			if sf.OrigPath != "" {
				action = "[RENAME]"
			}
			t.Logf("    %s Pos %2d  %s → %s",
				action, sf.Position,
				filepath.Base(sf.OrigPath),
				filepath.Base(sf.NewPath))
		}
	}

	// ========================================================================
	// Step 2: EncodeStripe — 逐 stripe 编码
	// ========================================================================
	for i, g := range groups {
		res, err := ec.EncodeStripe(g, cfg)
		if err != nil {
			t.Fatalf("EncodeStripe[%d]: %v", i, err)
		}
		t.Logf("\nStep 2: EncodeStripe[%d] done", i+1)
		t.Logf("  PaddedSize:    %d", res.PaddedSize)
		t.Logf("  OriginalSizes: %v", res.OriginalSizes)

		// 每个 data 文件应该已从 OrigPath rename 到 NewPath
		for _, sf := range g[:cfg.K] {
			if sf.OrigPath == "" {
				continue
			}
			_, errNew := os.Stat(sf.NewPath)
			_, errOld := os.Stat(sf.OrigPath)
			t.Logf("  %s: exists=%v  orig-gone=%v",
				filepath.Base(sf.NewPath), errNew == nil, os.IsNotExist(errOld))
		}
		// EC 文件应该存在
		for _, sf := range g[cfg.K:] {
			info, err := os.Stat(sf.NewPath)
			if err != nil {
				t.Errorf("EC file missing: %s", sf.NewPath)
			} else {
				t.Logf("  %s: %d bytes", filepath.Base(sf.NewPath), info.Size())
			}
		}

		// ====================================================================
		// Step 3: VerifyStripe — 逐 stripe 校验
		// ====================================================================
		ok, err := ec.VerifyStripe(g, cfg)
		if err != nil {
			t.Fatalf("VerifyStripe[%d]: %v", i, err)
		}
		t.Logf("  Verify: %v\n", ok)
	}

	// ========================================================================
	// 最终输出目录一览
	// ========================================================================
	t.Log("Step 4: output directory listing:")
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		info, _ := e.Info()
		t.Logf("  %-25s %8d bytes", e.Name(), info.Size())
	}
}

// ---------------------------------------------------------------------------
// 模拟 shard make 产生的故障：删除一个 data 文件，用 EC 恢复
// ---------------------------------------------------------------------------

func TestECWorkflow_Recover(t *testing.T) {
	cfg, _ := ec.ParseConfig("4+2")
	dir := t.TempDir()

	// Step 0: 准备 4 个 data 文件，大小不均。
	var dataFiles []string
	for i, sz := range []int{512, 2048, 1024, 768} {
		name := fmt.Sprintf("s%02d.bin", i)
		path := filepath.Join(dir, name)
		os.WriteFile(path, make([]byte, sz), 0644)
		dataFiles = append(dataFiles, path)
	}

	// Step 1: PlanStripes + EncodeStripe。
	groups, _ := ec.PlanStripes(dataFiles, cfg, dir)
	res, err := ec.EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("EncodeStripe: %v", err)
	}
	t.Logf("Encoded: OriginalSizes=%v, PaddedSize=%d", res.OriginalSizes, res.PaddedSize)

	// Step 2: 模拟丢失 data shard #2。
	lost := groups[0][1]
	t.Logf("Simulating loss of: %s", filepath.Base(lost.NewPath))
	os.Remove(lost.NewPath)

	// Step 3: ReconstructStripe 恢复。
	if err := ec.ReconstructStripe(groups[0], cfg, res.OriginalSizes, res.PaddedSize); err != nil {
		t.Fatalf("ReconstructStripe: %v", err)
	}

	// Step 4: 验证恢复的文件内容。
	recovered, _ := os.ReadFile(lost.NewPath)
	t.Logf("Recovered %s: %d bytes", filepath.Base(lost.NewPath), len(recovered))

	// Step 5: 全局校验。
	ok, _ := ec.VerifyStripe(groups[0], cfg)
	t.Logf("Verify after recover: %v", ok)
}

// ---------------------------------------------------------------------------
// 模拟 gen-def 产生的 data 文件数不能被 k 整除的场景（自动补齐空 shard）
// ---------------------------------------------------------------------------

func TestECWorkflow_Padding(t *testing.T) {
	cfg, _ := ec.ParseConfig("3+2")
	dir := t.TempDir()

	// 只有 4 个 data 文件，k=3 → n=2 stripes，最后一个 stripe 有 2 个真实 data + 1 个空 padding。
	dataFiles := writeDataFiles(t, dir, []string{
		"a.bin", "b.bin", "c.bin", "d.bin",
	})
	t.Logf("%d data files with k=%d → %d stripe(s)", len(dataFiles), cfg.K, (len(dataFiles)+cfg.K-1)/cfg.K)

	groups, _ := ec.PlanStripes(dataFiles, cfg, dir)

	for i, g := range groups {
		t.Logf("\nStripe %d:", i+1)
		for _, sf := range g {
			if sf.OrigPath == "" && sf.IsData() {
				t.Logf("  [PAD]  Pos %d  (empty padding)", sf.Position)
			} else if sf.IsData() {
				t.Logf("  [DATA] Pos %d  %s", sf.Position, filepath.Base(sf.OrigPath))
			} else {
				t.Logf("  [EC]   Pos %d  %s", sf.Position, filepath.Base(sf.NewPath))
			}
		}

		res, err := ec.EncodeStripe(g, cfg)
		if err != nil {
			t.Fatalf("EncodeStripe[%d]: %v", i, err)
		}
		t.Logf("  → PaddedSize=%d, Sizes=%v", res.PaddedSize, res.OriginalSizes)

		ok, _ := ec.VerifyStripe(g, cfg)
		t.Logf("  → Verify: %v", ok)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeDataFiles(tb testing.TB, dir string, names []string) []string {
	tb.Helper()
	var paths []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		content := make([]byte, 1024)
		copy(content, name) // deterministic content
		if err := os.WriteFile(path, content, 0644); err != nil {
			tb.Fatalf("write test file: %v", err)
		}
		paths = append(paths, path)
	}
	return paths
}
