package simulate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerate(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Name:       "test-ds",
		StartTS:    1,
		EndTS:      3,
		ChStart:    10,
		ChEnd:      11,
		FileBytes:  256,
		OutputRoot: tmpDir,
	}

	stats, err := Generate(cfg)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// 2 channels × 3 timestamps = 6 files
	expectedFiles := (cfg.ChEnd - cfg.ChStart + 1) * (cfg.EndTS - cfg.StartTS + 1)
	if stats.FileCount != expectedFiles {
		t.Errorf("file count: got %d, want %d", stats.FileCount, expectedFiles)
	}
	if stats.TotalBytes != int64(expectedFiles)*int64(cfg.FileBytes) {
		t.Errorf("total bytes: got %d, want %d", stats.TotalBytes, expectedFiles*cfg.FileBytes)
	}

	expectedDir := filepath.Join(tmpDir, cfg.Name)
	if stats.OutputDir != expectedDir {
		t.Errorf("output dir: got %s, want %s", stats.OutputDir, expectedDir)
	}

	// Verify files exist
	entries, err := os.ReadDir(expectedDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != expectedFiles {
		t.Errorf("files on disk: got %d, want %d", len(entries), expectedFiles)
	}

	// Check naming: {ts}_{nextTs}_ch{ch}.dat
	found := make(map[string]bool)
	for _, e := range entries {
		found[e.Name()] = true
	}
	for ch := cfg.ChStart; ch <= cfg.ChEnd; ch++ {
		for ts := cfg.StartTS; ts <= cfg.EndTS; ts++ {
			fname := "1_2_ch10.dat" // just check one predictable name
			_ = fname
		}
	}
	expectedName := "1_2_ch10.dat"
	if !found[expectedName] {
		t.Errorf("expected file %s not found in %s", expectedName, expectedDir)
	}
}
