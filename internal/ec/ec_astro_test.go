package ec_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ddy2006/packfs/internal/ec"
)

// TestEC_AstroData runs EC encode + verify + recover on ASTRO shard files.
//
// Prerequisites:
//
//	cd examples/astro
//	bash simulate.sh
//	SQLITE_DB=./packfs.db PACKFS_BIN=./packfs bash pack-serial.sh
//
// Run:
//
//	ASTRO_SHARD_DIR=examples/astro/data/shard go test -v -run TestEC_AstroData ./internal/ec/
func TestEC_AstroData(t *testing.T) {
	shardDir := os.Getenv("ASTRO_SHARD_DIR")
	if shardDir == "" {
		t.Skip("ASTRO_SHARD_DIR not set")
	}

	files := listShardFiles(t, shardDir)
	if len(files) == 0 {
		t.Fatalf("no shard files in %s", shardDir)
	}
	t.Logf("Found %d shard files", len(files))

	cfg, _ := ec.ParseConfig("2+2")

	outDir := filepath.Join(shardDir, "ec-out")
	os.MkdirAll(outDir, 0755)

	groups, err := ec.PlanStripes(files, cfg, outDir)
	if err != nil {
		t.Fatalf("PlanStripes: %v", err)
	}
	n := len(groups)
	t.Logf("PlanStripes: %d shards -> %d stripes (k=%d,m=%d)", len(files), n, cfg.K, cfg.M)

	// Encode + Verify
	var results []*ec.StripeResult
	var firstErr error
	for i, g := range groups {
		res, err := ec.EncodeStripe(g, cfg)
		if err != nil {
			firstErr = fmt.Errorf("stripe %d EncodeStripe: %w", i+1, err)
			break
		}
		results = append(results, res)
		ok, _ := ec.VerifyStripe(g, cfg)
		if !ok {
			t.Errorf("stripe %d VerifyStripe failed", i+1)
		}
	}
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	t.Logf("Encode+Verify: %d stripes OK, PaddedSize=%d", n, results[0].PaddedSize)

	// Simulate loss of position-1 data shard for each stripe, then recover
	for j := range groups {
		os.Remove(groups[j][0].NewPath)
	}
	for j, g := range groups {
		if err := ec.ReconstructStripe(g, cfg, results[j].OriginalSizes, results[j].PaddedSize); err != nil {
			t.Fatalf("stripe %d Reconstruct: %v", j+1, err)
		}
		ok, _ := ec.VerifyStripe(g, cfg)
		if !ok {
			t.Errorf("stripe %d verify after recover failed", j+1)
		}
	}
	t.Logf("Recover: all %d stripes OK", n)

	// Summary
	var dataBytes, ecBytes int64
	ents, _ := os.ReadDir(outDir)
	for _, e := range ents {
		info, _ := e.Info()
		if len(e.Name()) > 1 && e.Name()[1] == 'E' {
			ecBytes += info.Size()
		} else {
			dataBytes += info.Size()
		}
	}
	t.Logf("Output: %d files, data=%d KB, EC=%d KB, overhead=%.1f%%",
		len(ents), dataBytes>>10, ecBytes>>10, float64(ecBytes)/float64(dataBytes+1)*100)

	fmt.Printf("\nASTRO EC test PASSED: %d shards -> %d stripes (%d+%d)\n", len(files), n, cfg.K, cfg.M)
}

func listShardFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
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
