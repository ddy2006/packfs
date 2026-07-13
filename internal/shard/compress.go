package shard

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// NewCompressor creates a compressor writer for the given algorithm.
// isXZ=true uses XZ, otherwise uses zstd.
func NewCompressor(w io.Writer, isXZ bool) (io.WriteCloser, error) {
	if isXZ {
		return xz.NewWriter(w)
	}
	return zstd.NewWriter(w)
}

// CompressBytes compresses data using the given algorithm.
func CompressBytes(data []byte, isXZ bool) ([]byte, error) {
	var buf bytes.Buffer
	w, err := NewCompressor(&buf, isXZ)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
