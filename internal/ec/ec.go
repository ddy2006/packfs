// Package ec provides Reed-Solomon erasure code encoding, decoding, and verification.
//
// It wraps klauspost/reedsolomon with project-specific parameter constraints:
// k+m ≤ 255 (RS algorithm limit), k ≤ 24 (engineering limit), m ∈ {2,4,6}.
//
// # Batch-oriented API
//
// The public API is file-based: PlanStripes plans file names, EncodeStripe
// encodes one stripe at a time. The caller iterates over n batches.
//
//	groups := ec.PlanStripes(dataFiles, cfg, outputDir)
//	for _, files := range groups {
//	    result, _ := ec.EncodeStripe(files, cfg)
//	    // store result.OriginalSizes, result.PaddedSize to DB
//	}
package ec

import (
	"fmt"
	"strconv"
	"strings"
)

// Config holds Reed-Solomon erasure code parameters.
// K is the number of data shards, M is the number of parity shards per stripe.
type Config struct {
	K int
	M int
}

// ParseConfig parses an EC config string in "k+m" format (e.g. "8+4").
func ParseConfig(s string) (Config, error) {
	parts := strings.SplitN(s, "+", 2)
	if len(parts) != 2 {
		return Config{}, fmt.Errorf("invalid ec config %q: expected format k+m", s)
	}
	k, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return Config{}, fmt.Errorf("invalid ec config %q: k is not an integer", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return Config{}, fmt.Errorf("invalid ec config %q: m is not an integer", s)
	}
	return Config{K: k, M: m}, nil
}

// Total returns k+m, the total number of shards in one stripe.
func (c Config) Total() int { return c.K + c.M }

// Validate checks parameter constraints.
func (c Config) Validate() error {
	if c.K <= 0 || c.M <= 0 {
		return fmt.Errorf("k and m must be positive, got k=%d m=%d", c.K, c.M)
	}
	if c.K+c.M > 255 {
		return fmt.Errorf("k+m must not exceed 255 (RS hard limit), got %d", c.K+c.M)
	}
	if c.K > 24 {
		return fmt.Errorf("k must not exceed 24 (engineering limit), got %d", c.K)
	}
	if c.M != 2 && c.M != 4 && c.M != 6 {
		return fmt.Errorf("m must be 2, 4, or 6, got %d", c.M)
	}
	return nil
}

// String returns the config in "k+m" format.
func (c Config) String() string {
	return fmt.Sprintf("%d+%d", c.K, c.M)
}

// TapeLayout specifies how shards are distributed across physical tapes.
type TapeLayout int

const (
	// LayoutFixed maps Position directly to tape number.
	// All shards with the same Position (across all stripes) go to the same tape.
	// Analogous to RAID 3: data tapes and parity tapes are separate.
	LayoutFixed TapeLayout = 0

	// LayoutRotation rotates EC positions per stripe so that data and parity
	// are evenly distributed across all tapes. Analogous to RAID 5.
	// Formula: tape = ((Position - 1 + (Stripe-1) * M) % (K+M)) + 1
	LayoutRotation TapeLayout = 1
)

// StripeFile describes the naming of one shard file within a stripe.
type StripeFile struct {
	// OrigPath is the original data file path (empty for EC / padding shards).
	OrigPath string
	// NewPath is the full path after EC renaming (e.g. "<dir>/1D1_0000.bin").
	NewPath string
	// Type is "D" for data or "E" for erasure-coded parity.
	Type string
	// Stripe is the 1-based stripe index.
	Stripe int
	// Position is the 1-based position within the stripe (1..K for data, K+1..K+M for EC).
	Position int
}

// IsData returns true for data shards.
func (sf StripeFile) IsData() bool { return sf.Type == "D" }

// StripeResult holds the output of EncodeStripe.
type StripeResult struct {
	// OriginalSizes stores the unpadded byte length of each data shard.
	OriginalSizes []int64
	// PaddedSize is the length all shards were zero-padded to before encoding.
	PaddedSize int64
}
