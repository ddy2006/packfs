package ec

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/reedsolomon"
)

const (
	DefaultChunkSize = 8 << 20
	MinChunkSize     = 1 << 20
	MaxChunkSize     = 64 << 20
)

// StreamFile describes one position in a stripe for streaming operations.
// Size is the original DATA size; for EC it is padded_size. A missing file
// has Path=="" or does not exist on disk.
type StreamFile struct {
	Path string
	Size int64
	Type byte // 'D', 'E', or 'P'
}

// ValidateChunkSize checks the EC chunk size.
func ValidateChunkSize(n int) error {
	if n < MinChunkSize || n > MaxChunkSize {
		return fmt.Errorf("ec chunk size must be %d..%d bytes, got %d", MinChunkSize, MaxChunkSize, n)
	}
	return nil
}

// EncodeStripeStreaming encodes k DATA files into m EC files without reading
// the whole shard into memory. paths may contain empty entries for missing
// DATA positions; they are encoded as zero-filled data.
func EncodeStripeStreaming(dataPaths []string, dataSizes []int64, ecPaths []string, k, m, chunkSize int) (int64, error) {
	if m <= 0 {
		return 0, fmt.Errorf("m must be positive for EC encoding")
	}
	if len(dataPaths) != k || len(dataSizes) != k || len(ecPaths) != m {
		return 0, fmt.Errorf("invalid stripe shape: data=%d sizes=%d ec=%d k=%d m=%d",
			len(dataPaths), len(dataSizes), len(ecPaths), k, m)
	}
	if err := ValidateChunkSize(chunkSize); err != nil {
		return 0, err
	}

	var padded int64
	for _, n := range dataSizes {
		if n > padded {
			padded = n
		}
	}

	enc, err := reedsolomon.New(k, m)
	if err != nil {
		return 0, fmt.Errorf("create RS encoder: %w", err)
	}

	dataFiles := make([]*os.File, k)
	defer func() {
		for _, f := range dataFiles {
			if f != nil {
				f.Close()
			}
		}
	}()
	for i, p := range dataPaths {
		if p == "" {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return 0, fmt.Errorf("open data shard %q: %w", p, err)
		}
		dataFiles[i] = f
	}

	tmpPaths := make([]string, m)
	outFiles := make([]*os.File, m)
	defer func() {
		for _, f := range outFiles {
			if f != nil {
				f.Close()
			}
		}
	}()
	for i, p := range ecPaths {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return 0, err
		}
		tmp := p + ".tmp"
		tmpPaths[i] = tmp
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return 0, fmt.Errorf("create EC temp %q: %w", tmp, err)
		}
		outFiles[i] = f
	}

	bufs := make([][]byte, k+m)
	for off := int64(0); off < padded; off += int64(chunkSize) {
		n := int64(chunkSize)
		if padded-off < n {
			n = padded - off
		}
		for i := 0; i < k; i++ {
			buf := make([]byte, int(n))
			if f := dataFiles[i]; f != nil {
				start := off
				if start < dataSizes[i] {
					want := n
					if dataSizes[i]-start < want {
						want = dataSizes[i] - start
					}
					if _, err := f.ReadAt(buf[:want], start); err != nil && err != io.EOF {
						return 0, fmt.Errorf("read data shard %q: %w", dataPaths[i], err)
					}
				}
			}
			bufs[i] = buf
		}
		for i := 0; i < m; i++ {
			bufs[k+i] = make([]byte, int(n))
		}
		if err := enc.Encode(bufs); err != nil {
			return 0, fmt.Errorf("encode chunk at %d: %w", off, err)
		}
		for i := 0; i < m; i++ {
			if _, err := outFiles[i].Write(bufs[k+i]); err != nil {
				return 0, fmt.Errorf("write EC temp: %w", err)
			}
		}
	}

	for i, f := range outFiles {
		if err := f.Sync(); err != nil {
			return 0, fmt.Errorf("sync EC temp: %w", err)
		}
		if err := f.Close(); err != nil {
			return 0, fmt.Errorf("close EC temp: %w", err)
		}
		outFiles[i] = nil
		if err := os.Rename(tmpPaths[i], ecPaths[i]); err != nil {
			return 0, fmt.Errorf("rename EC temp: %w", err)
		}
	}
	return padded, nil
}

// VerifyStripeStreaming verifies a complete stripe chunk by chunk.
func VerifyStripeStreaming(files []StreamFile, k, m int, paddedSize int64, chunkSize int) (bool, error) {
	if len(files) != k+m {
		return false, fmt.Errorf("expected %d files, got %d", k+m, len(files))
	}
	if err := ValidateChunkSize(chunkSize); err != nil {
		return false, err
	}
	if m == 0 {
		return true, nil
	}
	enc, err := reedsolomon.New(k, m)
	if err != nil {
		return false, fmt.Errorf("create RS encoder: %w", err)
	}
	open := make([]*os.File, k+m)
	defer func() {
		for _, f := range open {
			if f != nil {
				f.Close()
			}
		}
	}()
	for i, sf := range files {
		f, err := os.Open(sf.Path)
		if err != nil {
			return false, fmt.Errorf("open shard %q: %w", sf.Path, err)
		}
		open[i] = f
	}

	bufs := make([][]byte, k+m)
	for off := int64(0); off < paddedSize; off += int64(chunkSize) {
		n := int64(chunkSize)
		if paddedSize-off < n {
			n = paddedSize - off
		}
		for i := 0; i < k+m; i++ {
			buf := make([]byte, int(n))
			if _, err := open[i].ReadAt(buf, off); err != nil && err != io.EOF {
				return false, fmt.Errorf("read shard %q: %w", files[i].Path, err)
			}
			bufs[i] = buf
		}
		ok, err := enc.Verify(bufs)
		if err != nil {
			return false, fmt.Errorf("verify chunk at %d: %w", off, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// ReconstructStripeStreaming rebuilds missing shards chunk by chunk.
// files has k+m entries; a missing entry has an empty Path or a nonexistent
// file. Missing files are written to their Path. target is "data" or "all";
// "data" writes only DATA positions, "all" writes DATA and EC.
func ReconstructStripeStreaming(files []StreamFile, originalSizes []int64, k, m int,
	paddedSize int64, chunkSize int, target string) error {
	if len(files) != k+m {
		return fmt.Errorf("expected %d files, got %d", k+m, len(files))
	}
	if err := ValidateChunkSize(chunkSize); err != nil {
		return err
	}
	if target != "data" && target != "all" {
		return fmt.Errorf("target must be data or all")
	}

	open := make([]*os.File, k+m)
	missing := make([]bool, k+m)
	defer func() {
		for _, f := range open {
			if f != nil {
				f.Close()
			}
		}
	}()
	avail := 0
	for i, sf := range files {
		if sf.Path == "" {
			missing[i] = true
			continue
		}
		f, err := os.Open(sf.Path)
		if err != nil {
			if os.IsNotExist(err) {
				missing[i] = true
				continue
			}
			return fmt.Errorf("open shard %q: %w", sf.Path, err)
		}
		open[i] = f
		avail++
	}
	if avail < k {
		return fmt.Errorf("need at least %d shards, got %d", k, avail)
	}

	enc, err := reedsolomon.New(k, m)
	if err != nil {
		return fmt.Errorf("create RS encoder: %w", err)
	}

	tmpPaths := make([]string, k+m)
	tmpFiles := make([]*os.File, k+m)
	defer func() {
		for _, f := range tmpFiles {
			if f != nil {
				f.Close()
			}
		}
	}()
	for i := range files {
		if !missing[i] {
			continue
		}
		if i >= k && target != "all" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(files[i].Path), 0755); err != nil {
			return err
		}
		tmp := files[i].Path + ".tmp"
		tmpPaths[i] = tmp
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("create recovery temp: %w", err)
		}
		tmpFiles[i] = f
	}

	bufs := make([][]byte, k+m)
	for off := int64(0); off < paddedSize; off += int64(chunkSize) {
		n := int64(chunkSize)
		if paddedSize-off < n {
			n = paddedSize - off
		}
		for i := 0; i < k+m; i++ {
			if missing[i] {
				bufs[i] = nil
				continue
			}
			buf := make([]byte, int(n))
			if _, err := open[i].ReadAt(buf, off); err != nil && err != io.EOF {
				return fmt.Errorf("read shard %q: %w", files[i].Path, err)
			}
			bufs[i] = buf
		}
		if err := enc.Reconstruct(bufs); err != nil {
			return fmt.Errorf("reconstruct chunk at %d: %w", off, err)
		}
		for i := 0; i < k+m; i++ {
			if !missing[i] || tmpFiles[i] == nil {
				continue
			}
			buf := bufs[i]
			if i < k && off+n > originalSizes[i] {
				buf = buf[:max64(0, originalSizes[i]-off)]
			}
			if _, err := tmpFiles[i].Write(buf); err != nil {
				return fmt.Errorf("write recovered shard: %w", err)
			}
		}
	}

	for i, f := range tmpFiles {
		if f == nil {
			continue
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		tmpFiles[i] = nil
		if err := os.Rename(tmpPaths[i], files[i].Path); err != nil {
			return err
		}
	}
	return nil
}

// SHA256File returns the streaming SHA-256 of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
