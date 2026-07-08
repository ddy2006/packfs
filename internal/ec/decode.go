package ec

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/klauspost/reedsolomon"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ReconstructStripe repairs missing shards in one stripe.
//
// files must have length k+m. A shard is considered missing when its NewPath
// is "" or the file does not exist on disk. Missing files are reconstructed
// and written back to NewPath.
//
// All surviving data shards are temporarily zero-padded to paddedSize before
// reconstruction and trimmed back to originalSizes afterwards.
func ReconstructStripe(files []StripeFile, cfg Config, originalSizes []int64, paddedSize int64) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if len(files) != cfg.Total() {
		return fmt.Errorf("expected %d files, got %d", cfg.Total(), len(files))
	}

	// Read surviving shards; track which positions need reconstruction.
	shards := make([][]byte, cfg.Total())
	missing := make([]bool, cfg.Total())

	for i, sf := range files {
		if sf.NewPath == "" {
			shards[i] = nil
			missing[i] = true
			continue
		}
		b, err := os.ReadFile(sf.NewPath)
		if err != nil {
			if os.IsNotExist(err) {
				shards[i] = nil
				missing[i] = true
				continue
			}
			return fmt.Errorf("read shard %q: %w", sf.NewPath, err)
		}
		// Data shards may need padding to match parity size.
		if i < cfg.K && paddedSize > 0 && len(b) < int(paddedSize) {
			padded := make([]byte, paddedSize)
			copy(padded, b)
			b = padded
		}
		shards[i] = b
	}

	// Reconstruct.
	if err := reconstructBytes(shards, cfg); err != nil {
		return err
	}

	// Trim reconstructed data shards.
	if len(originalSizes) > 0 {
		TrimData(shards[:cfg.K], originalSizes)
	}

	// Write back only the positions that were missing.
	for i, sf := range files {
		if !missing[i] || sf.NewPath == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(sf.NewPath), 0755); err != nil {
			return fmt.Errorf("create output dir for %q: %w", sf.NewPath, err)
		}
		if err := os.WriteFile(sf.NewPath, shards[i], 0644); err != nil {
			return fmt.Errorf("write reconstructed file %q: %w", sf.NewPath, err)
		}
	}

	return nil
}

// VerifyStripe checks parity consistency for a complete stripe.
// All k+m files must exist and be readable. Data shards are automatically
// zero-padded to match parity size before verification.
//
// Returns true when the stripe is consistent, false when corruption is detected.
func VerifyStripe(files []StripeFile, cfg Config) (bool, error) {
	if err := cfg.Validate(); err != nil {
		return false, err
	}
	if len(files) != cfg.Total() {
		return false, fmt.Errorf("expected %d files, got %d", cfg.Total(), len(files))
	}

	shards := make([][]byte, cfg.Total())
	var maxLen int
	for i, sf := range files {
		b, err := os.ReadFile(sf.NewPath)
		if err != nil {
			return false, fmt.Errorf("read shard %q: %w", sf.NewPath, err)
		}
		shards[i] = b
		if len(b) > maxLen {
			maxLen = len(b)
		}
	}

	// Pad data shards to equal length (EC files are already at padded size).
	for i := 0; i < cfg.K; i++ {
		shards[i] = padTo(shards[i], maxLen)
	}

	return verifyBytes(shards, cfg)
}

// TrimData truncates each data shard to the length recorded in originalSizes.
// Use after reconstruction to strip zero-padding that was added before encoding.
func TrimData(shards [][]byte, sizes []int64) {
	for i := range sizes {
		if i < len(shards) && int64(len(shards[i])) > sizes[i] {
			shards[i] = shards[i][:sizes[i]]
		}
	}
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

// reconstructBytes repairs missing shards in memory.
// shards must have length k+m. Missing positions are nil.
// All non-nil shards must share the same byte length.
func reconstructBytes(shards [][]byte, cfg Config) error {
	if len(shards) != cfg.Total() {
		return fmt.Errorf("expected %d shards, got %d", cfg.Total(), len(shards))
	}

	avail := 0
	for _, s := range shards {
		if s != nil {
			avail++
		}
	}
	if avail == 0 {
		return fmt.Errorf("no shards available for reconstruction")
	}
	if avail < cfg.K {
		return fmt.Errorf("need at least %d shards, got %d", cfg.K, avail)
	}

	enc, err := reedsolomon.New(cfg.K, cfg.M)
	if err != nil {
		return fmt.Errorf("create RS encoder: %w", err)
	}
	if err := enc.Reconstruct(shards); err != nil {
		return fmt.Errorf("reconstruct stripe: %w", err)
	}
	return nil
}

// verifyBytes checks parity consistency in memory.
func verifyBytes(shards [][]byte, cfg Config) (bool, error) {
	if len(shards) != cfg.Total() {
		return false, fmt.Errorf("expected %d shards, got %d", cfg.Total(), len(shards))
	}
	enc, err := reedsolomon.New(cfg.K, cfg.M)
	if err != nil {
		return false, fmt.Errorf("create RS encoder: %w", err)
	}
	return enc.Verify(shards)
}
