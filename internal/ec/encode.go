package ec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/reedsolomon"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// PlanStripes takes n*k data file paths and generates n groups of k+m StripeFile
// entries with EC naming. Data files are assigned to stripes in sequential order
// (first k -> stripe 1, next k -> stripe 2, ...).
//
// If len(dataFiles) is not a multiple of K, the last stripe is padded with empty
// data positions (OrigPath == ""). Padding positions get a generated NewPath
// (e.g. "2D2_pad.bin") so EncodeStripe can write empty files for them.
//
// If outputDir is empty, files are planned in the same directory as the first
// data file.
func PlanStripes(dataFiles []string, cfg Config, outputDir string) ([][]StripeFile, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(dataFiles) == 0 {
		return nil, fmt.Errorf("no data files provided")
	}

	dir := outputDir
	if dir == "" {
		dir = filepath.Dir(dataFiles[0])
	}

	ext := extractExt(dataFiles[0])

	n := (len(dataFiles) + cfg.K - 1) / cfg.K // ceil division
	groups := make([][]StripeFile, n)

	for s := 0; s < n; s++ {
		files := make([]StripeFile, 0, cfg.Total())

		// Data positions 1..K.
		for pos := 0; pos < cfg.K; pos++ {
			idx := s*cfg.K + pos
			sf := StripeFile{
				Type:     "D",
				Stripe:   s + 1,
				Position: pos + 1,
			}
			if idx < len(dataFiles) {
				sf.OrigPath = dataFiles[idx]
				base := filepath.Base(dataFiles[idx])
				sf.NewPath = filepath.Join(dir, fmt.Sprintf("%dD%d_%s", s+1, pos+1, base))
			} else {
				sf.NewPath = filepath.Join(dir, fmt.Sprintf("%dD%d_pad.%s", s+1, pos+1, ext))
			}
			files = append(files, sf)
		}

		// EC positions K+1..K+M.
		for pos := 0; pos < cfg.M; pos++ {
			sf := StripeFile{
				Type:     "E",
				Stripe:   s + 1,
				Position: cfg.K + pos + 1,
			}
			sf.NewPath = filepath.Join(dir, fmt.Sprintf("%dE%d.%s", s+1, sf.Position, ext))
			files = append(files, sf)
		}

		groups[s] = files
	}

	return groups, nil
}

// EncodeStripe encodes one stripe: reads k data files, produces m parity files,
// and renames data files to their EC names.
//
// Execution order (resilient to mid-flight failures):
//  1. Read k data files (fallback to NewPath for idempotent re-runs)
//  2. RS-encode -> m parity shards
//  3. Write m EC files
//  4. Write empty files for padding data shards + rename real data files
func EncodeStripe(files []StripeFile, cfg Config) (*StripeResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(files) != cfg.Total() {
		return nil, fmt.Errorf("expected %d files, got %d", cfg.Total(), len(files))
	}

	// 1. Read data shards.
	data := make([][]byte, cfg.K)
	for i, sf := range files[:cfg.K] {
		if sf.Type != "D" {
			return nil, fmt.Errorf("position %d: expected type D, got %s", i+1, sf.Type)
		}
		if sf.OrigPath == "" {
			data[i] = []byte{} // padding empty shard
			continue
		}
		b, err := os.ReadFile(sf.OrigPath)
		if os.IsNotExist(err) && sf.NewPath != "" && sf.NewPath != sf.OrigPath {
			// Already renamed (idempotent re-run): read from NewPath.
			b, err = os.ReadFile(sf.NewPath)
		}
		if err != nil {
			return nil, fmt.Errorf("read data file %q: %w", sf.OrigPath, err)
		}
		data[i] = b
	}

	// 2. RS-encode.
	res, err := encodeBytes(data, cfg)
	if err != nil {
		return nil, err
	}

	// 3. Write EC files.
	for i, sf := range files[cfg.K:] {
		if sf.Type != "E" {
			return nil, fmt.Errorf("position %d: expected type E, got %s", cfg.K+i+1, sf.Type)
		}
		if err := os.MkdirAll(filepath.Dir(sf.NewPath), 0755); err != nil {
			return nil, fmt.Errorf("create output dir for %q: %w", sf.NewPath, err)
		}
		if err := os.WriteFile(sf.NewPath, res.Parity[i], 0644); err != nil {
			return nil, fmt.Errorf("write EC file %q: %w", sf.NewPath, err)
		}
	}

	// 4. Write empty files for padding data shards + rename real data files.
	for _, sf := range files[:cfg.K] {
		if sf.OrigPath == "" && sf.NewPath != "" {
			// Padding shard: write empty file.
			if _, err := os.Stat(sf.NewPath); err == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(sf.NewPath), 0755); err != nil {
				return nil, fmt.Errorf("create dir for padding shard %q: %w", sf.NewPath, err)
			}
			if err := os.WriteFile(sf.NewPath, nil, 0644); err != nil {
				return nil, fmt.Errorf("write padding shard %q: %w", sf.NewPath, err)
			}
			continue
		}
		if sf.OrigPath == "" || sf.OrigPath == sf.NewPath {
			continue
		}
		if _, err := os.Stat(sf.NewPath); err == nil {
			continue // already renamed
		}
		if _, err := os.Stat(sf.OrigPath); os.IsNotExist(err) {
			continue // already renamed (raced)
		}
		if err := os.MkdirAll(filepath.Dir(sf.NewPath), 0755); err != nil {
			return nil, fmt.Errorf("create output dir for %q: %w", sf.NewPath, err)
		}
		if err := os.Rename(sf.OrigPath, sf.NewPath); err != nil {
			return nil, fmt.Errorf("rename %q -> %q: %w", sf.OrigPath, sf.NewPath, err)
		}
	}

	return &StripeResult{
		OriginalSizes: res.OriginalSizes,
		PaddedSize:    res.PaddedSize,
	}, nil
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

type stripeResult struct {
	Parity        [][]byte
	OriginalSizes []int64
	PaddedSize    int64
}

func encodeBytes(data [][]byte, cfg Config) (*stripeResult, error) {
	if len(data) != cfg.K {
		return nil, fmt.Errorf("expected %d data shards, got %d", cfg.K, len(data))
	}

	sizes := make([]int64, cfg.K)
	var maxLen int
	for i, d := range data {
		sizes[i] = int64(len(d))
		if len(d) > maxLen {
			maxLen = len(d)
		}
	}

	if maxLen == 0 {
		parity := make([][]byte, cfg.M)
		for i := range parity {
			parity[i] = []byte{}
		}
		return &stripeResult{Parity: parity, OriginalSizes: sizes, PaddedSize: 0}, nil
	}

	shards := make([][]byte, cfg.Total())
	for i := 0; i < cfg.K; i++ {
		shards[i] = padTo(data[i], maxLen)
	}
	for i := cfg.K; i < cfg.Total(); i++ {
		shards[i] = make([]byte, maxLen)
	}

	enc, err := reedsolomon.New(cfg.K, cfg.M)
	if err != nil {
		return nil, fmt.Errorf("create RS encoder: %w", err)
	}
	if err := enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("encode stripe: %w", err)
	}

	return &stripeResult{
		Parity:        shards[cfg.K:],
		OriginalSizes: sizes,
		PaddedSize:    int64(maxLen),
	}, nil
}

func padTo(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	padded := make([]byte, n)
	copy(padded, b)
	return padded
}

func extractExt(filename string) string {
	base := filepath.Base(filename)
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		return base[idx+1:]
	}
	return "bin"
}
