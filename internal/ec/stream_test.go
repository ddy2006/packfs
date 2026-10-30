package ec

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writePattern(t *testing.T, path string, size int, seed byte) []byte {
	t.Helper()
	data := make([]byte, size)
	for i := range data {
		data[i] = seed + byte(i%251)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}

// The shard sizes span several 1MiB chunks, so this exercises the streaming
// loop (not a whole-file encode) for encode, verify, and reconstruct.
func TestStreamingEncodeVerifyReconstruct(t *testing.T) {
	dir := t.TempDir()
	k, m := 4, 2
	sizes := []int{3<<20 + 17, 2 << 20, 1<<20 + 5, 4<<20 + 3}
	chunk := MinChunkSize

	dataPaths := make([]string, k)
	data := make([][]byte, k)
	streamFiles := make([]StreamFile, k+m)
	for i := 0; i < k; i++ {
		dataPaths[i] = filepath.Join(dir, "d"+string(rune('0'+i)))
		data[i] = writePattern(t, dataPaths[i], sizes[i], byte(17*i+1))
		streamFiles[i] = StreamFile{Path: dataPaths[i], Size: int64(sizes[i]), Type: 'D'}
	}
	ecPaths := make([]string, m)
	for i := 0; i < m; i++ {
		ecPaths[i] = filepath.Join(dir, "e"+string(rune('0'+i)))
	}
	padded, err := EncodeStripeStreaming(dataPaths, []int64{
		int64(sizes[0]), int64(sizes[1]), int64(sizes[2]), int64(sizes[3]),
	}, ecPaths, k, m, chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if want := int64(sizes[3]); padded != want {
		t.Fatalf("padded=%d, want %d", padded, want)
	}
	for i, p := range ecPaths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		streamFiles[k+i] = StreamFile{Path: p, Size: info.Size(), Type: 'E'}
	}
	ok, err := VerifyStripeStreaming(streamFiles, k, m, padded, chunk)
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}

	// Delete one DATA and one EC shard; recover both.
	if err := os.Remove(dataPaths[1]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ecPaths[0]); err != nil {
		t.Fatal(err)
	}
	if err := ReconstructStripeStreaming(streamFiles, []int64{
		int64(sizes[0]), int64(sizes[1]), int64(sizes[2]), int64(sizes[3]),
	}, k, m, padded, chunk, "all"); err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	got, err := os.ReadFile(dataPaths[1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[1]) {
		t.Fatalf("recovered DATA bytes differ")
	}
	info, err := os.Stat(ecPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != padded {
		t.Fatalf("recovered EC size=%d, want %d", info.Size(), padded)
	}
	ok, err = VerifyStripeStreaming(streamFiles, k, m, padded, chunk)
	if err != nil || !ok {
		t.Fatalf("verify after reconstruct: ok=%v err=%v", ok, err)
	}
}

func TestStreamingReconstructDataOnly(t *testing.T) {
	dir := t.TempDir()
	k, m := 4, 2
	sizes := []int{MinChunkSize + 7, MinChunkSize + 3, MinChunkSize, MinChunkSize + 11}
	chunk := MinChunkSize
	dataPaths := make([]string, k)
	for i := 0; i < k; i++ {
		dataPaths[i] = filepath.Join(dir, "d"+string(rune('0'+i)))
		writePattern(t, dataPaths[i], sizes[i], byte(i+1))
	}
	ecPaths := make([]string, m)
	for i := 0; i < m; i++ {
		ecPaths[i] = filepath.Join(dir, "e"+string(rune('0'+i)))
	}
	padded, err := EncodeStripeStreaming(dataPaths, []int64{
		int64(sizes[0]), int64(sizes[1]), int64(sizes[2]), int64(sizes[3]),
	}, ecPaths, k, m, chunk)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]StreamFile, k+m)
	for i := 0; i < k; i++ {
		files[i] = StreamFile{Path: dataPaths[i], Size: int64(sizes[i]), Type: 'D'}
	}
	for i := 0; i < m; i++ {
		files[k+i] = StreamFile{Path: ecPaths[i], Size: padded, Type: 'E'}
	}
	if err := os.Remove(dataPaths[2]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ecPaths[1]); err != nil {
		t.Fatal(err)
	}
	if err := ReconstructStripeStreaming(files, []int64{
		int64(sizes[0]), int64(sizes[1]), int64(sizes[2]), int64(sizes[3]),
	}, k, m, padded, chunk, "data"); err != nil {
		t.Fatalf("reconstruct data: %v", err)
	}
	got, err := os.ReadFile(dataPaths[2])
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) != int64(sizes[2]) {
		t.Fatalf("recovered DATA size=%d, want %d", len(got), sizes[2])
	}
	if _, err := os.Stat(ecPaths[1]); !os.IsNotExist(err) {
		t.Fatalf("target=data should not recreate EC, stat err=%v", err)
	}
}
