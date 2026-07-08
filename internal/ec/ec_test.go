package ec

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseConfig
// ---------------------------------------------------------------------------

func TestParseConfig_Success(t *testing.T) {
	tests := []struct {
		input string
		k, m  int
	}{
		{"8+4", 8, 4},
		{"4+2", 4, 2},
		{"12+6", 12, 6},
		{" 8 + 4 ", 8, 4},
	}
	for _, tt := range tests {
		c, err := ParseConfig(tt.input)
		if err != nil {
			t.Errorf("ParseConfig(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if c.K != tt.k || c.M != tt.m {
			t.Errorf("ParseConfig(%q) = (%d,%d), want (%d,%d)", tt.input, c.K, c.M, tt.k, tt.m)
		}
	}
}

func TestParseConfig_Invalid(t *testing.T) {
	for _, s := range []string{"", "8", "8-4", "abc+4", "8+xyz"} {
		if _, err := ParseConfig(s); err == nil {
			t.Errorf("ParseConfig(%q) expected error, got nil", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestConfig_Validate_OK(t *testing.T) {
	for _, c := range []Config{{8, 4}, {4, 2}, {12, 6}, {24, 2}, {1, 2}} {
		if err := c.Validate(); err != nil {
			t.Errorf("Config%+v.Validate() = %v, want nil", c, err)
		}
	}
}

func TestConfig_Validate_Fail(t *testing.T) {
	fail := []Config{
		{0, 4}, {8, 0}, {-1, 4}, {25, 4}, {8, 3}, {8, 8}, {200, 60},
	}
	for _, c := range fail {
		if err := c.Validate(); err == nil {
			t.Errorf("Config%+v.Validate() should fail", c)
		}
	}
}

func TestConfig_Total(t *testing.T) {
	if c := (Config{K: 8, M: 4}); c.Total() != 12 {
		t.Errorf("Total() = %d, want 12", c.Total())
	}
}

func TestConfig_String(t *testing.T) {
	if s := (Config{K: 8, M: 4}).String(); s != "8+4" {
		t.Errorf("String() = %q, want %q", s, "8+4")
	}
}

// ---------------------------------------------------------------------------
// PlanStripes
// ---------------------------------------------------------------------------

func TestPlanStripes_Basic(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	dataFiles := []string{
		"/data/0000.bin", "/data/0001.bin", "/data/0002.bin",
		"/data/0003.bin", "/data/0004.bin", "/data/0005.bin",
	}

	groups, err := PlanStripes(dataFiles, cfg, "/out")
	if err != nil {
		t.Fatalf("PlanStripes: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	g0 := groups[0]
	if len(g0) != 5 {
		t.Fatalf("group 0: expected 5 files, got %d", len(g0))
	}
	checkFile(t, g0[0], "/data/0000.bin", "/out/1D1_0000.bin", "D", 1, 1)
	checkFile(t, g0[1], "/data/0001.bin", "/out/1D2_0001.bin", "D", 1, 2)
	checkFile(t, g0[2], "/data/0002.bin", "/out/1D3_0002.bin", "D", 1, 3)
	checkFile(t, g0[3], "", "/out/1E4.bin", "E", 1, 4)
	checkFile(t, g0[4], "", "/out/1E5.bin", "E", 1, 5)

	g1 := groups[1]
	checkFile(t, g1[0], "/data/0003.bin", "/out/2D1_0003.bin", "D", 2, 1)
	checkFile(t, g1[1], "/data/0004.bin", "/out/2D2_0004.bin", "D", 2, 2)
	checkFile(t, g1[2], "/data/0005.bin", "/out/2D3_0005.bin", "D", 2, 3)
	checkFile(t, g1[3], "", "/out/2E4.bin", "E", 2, 4)
	checkFile(t, g1[4], "", "/out/2E5.bin", "E", 2, 5)
}

func TestPlanStripes_DefaultDir(t *testing.T) {
	cfg := Config{K: 2, M: 2}
	dataFiles := []string{"/a/b/0000.bin", "/a/b/0001.bin"}

	groups, err := PlanStripes(dataFiles, cfg, "")
	if err != nil {
		t.Fatalf("PlanStripes: %v", err)
	}
	g0 := groups[0]
	if dir := filepath.Dir(g0[0].NewPath); dir != "/a/b" {
		t.Errorf("default dir = %q, want /a/b", dir)
	}
}

func TestPlanStripes_Padding(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	dataFiles := []string{"/d/0.bin", "/d/1.bin", "/d/2.bin", "/d/3.bin"} // 4 files, k=3

	groups, err := PlanStripes(dataFiles, cfg, "/out")
	if err != nil {
		t.Fatalf("PlanStripes: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	g1 := groups[1]
	if g1[0].OrigPath != "/d/3.bin" {
		t.Error("stripe 2 pos 1 should be /d/3.bin")
	}
	if g1[1].OrigPath != "" {
		t.Error("stripe 2 pos 2 should be padding (empty OrigPath)")
	}
	if g1[1].NewPath == "" {
		t.Error("stripe 2 pos 2 padding should have NewPath")
	}
	if g1[2].OrigPath != "" {
		t.Error("stripe 2 pos 3 should be padding (empty OrigPath)")
	}
	if g1[2].NewPath == "" {
		t.Error("stripe 2 pos 3 padding should have NewPath")
	}
}

func TestPlanStripes_ExtractExt(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"0000.bin", "bin"},
		{"0000.tar.zst", "tar.zst"},
		{"0000.iso", "iso"},
	}
	for _, tt := range tests {
		if got := extractExt(tt.filename); got != tt.want {
			t.Errorf("extractExt(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// EncodeStripe + VerifyStripe
// ---------------------------------------------------------------------------

func TestEncodeStripe_Basic(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	dir := t.TempDir()

	dataFiles := makeDataFiles(t, dir, []string{"a.bin", "b.bin", "c.bin"}, 1024)

	groups, _ := PlanStripes(dataFiles, cfg, dir)
	res, err := EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("EncodeStripe: %v", err)
	}
	if len(res.OriginalSizes) != 3 {
		t.Fatalf("expected 3 sizes, got %d", len(res.OriginalSizes))
	}
	if res.PaddedSize != 1024 {
		t.Errorf("PaddedSize = %d, want 1024", res.PaddedSize)
	}

	// Data files should be renamed.
	for _, sf := range groups[0][:3] {
		if _, err := os.Stat(sf.NewPath); err != nil {
			t.Errorf("data file not renamed: %q", sf.NewPath)
		}
		if _, err := os.Stat(sf.OrigPath); !os.IsNotExist(err) {
			t.Errorf("orig path should not exist after rename: %q", sf.OrigPath)
		}
	}
	// EC files should exist.
	for _, sf := range groups[0][3:] {
		if _, err := os.Stat(sf.NewPath); err != nil {
			t.Errorf("EC file missing: %q", sf.NewPath)
		}
	}

	ok, err := VerifyStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("VerifyStripe: %v", err)
	}
	if !ok {
		t.Error("VerifyStripe returned false after encode")
	}
}

func TestEncodeStripe_UnequalSizes(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	dir := t.TempDir()

	// Use ordered names to control sizes: a=512, b=2048, c=0.
	dataFiles := makeDataFilesSized(t, dir, []string{"a.bin", "b.bin", "c.bin"}, []int{512, 2048, 0})

	groups, _ := PlanStripes(dataFiles, cfg, dir)
	res, err := EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("EncodeStripe: %v", err)
	}
	if res.OriginalSizes[2] != 0 {
		t.Errorf("empty file orig size = %d, want 0", res.OriginalSizes[2])
	}
	if res.PaddedSize != 2048 {
		t.Errorf("PaddedSize = %d, want 2048", res.PaddedSize)
	}

	ok, _ := VerifyStripe(groups[0], cfg)
	if !ok {
		t.Error("VerifyStripe returned false after unequal-size encode")
	}
}

func TestEncodeStripe_Idempotent(t *testing.T) {
	cfg := Config{K: 2, M: 2}
	dir := t.TempDir()

	dataFiles := makeDataFiles(t, dir, []string{"x.bin", "y.bin"}, 100)

	groups, _ := PlanStripes(dataFiles, cfg, dir)
	_, err := EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("first EncodeStripe: %v", err)
	}

	// Second call: OrigPath no longer exists, should fall back to NewPath.
	_, err = EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("second EncodeStripe: %v", err)
	}

	ok, _ := VerifyStripe(groups[0], cfg)
	if !ok {
		t.Error("VerifyStripe returned false after idempotent re-run")
	}
}

func TestEncodeStripe_WrongLength(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	_, err := EncodeStripe(make([]StripeFile, 3), cfg)
	if err == nil {
		t.Error("expected error for 3 files when k+m=5")
	}
}

// ---------------------------------------------------------------------------
// ReconstructStripe
// ---------------------------------------------------------------------------

func TestReconstructStripe_MissingDataShard(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	dir := t.TempDir()

	dataFiles := makeDataFiles(t, dir, []string{"a.bin", "b.bin", "c.bin"}, 1024)
	original := readFiles(t, dataFiles)

	groups, _ := PlanStripes(dataFiles, cfg, dir)
	res, err := EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("EncodeStripe: %v", err)
	}

	// Delete data shard #2 (keep NewPath intact).
	os.Remove(groups[0][1].NewPath)

	if err := ReconstructStripe(groups[0], cfg, res.OriginalSizes, res.PaddedSize); err != nil {
		t.Fatalf("ReconstructStripe: %v", err)
	}

	recovered, err := os.ReadFile(groups[0][1].NewPath)
	if err != nil {
		t.Fatalf("read recovered file: %v", err)
	}
	if !bytes.Equal(recovered, original[1]) {
		t.Error("recovered shard does not match original")
	}
}

func TestReconstructStripe_MultipleMissing(t *testing.T) {
	cfg := Config{K: 4, M: 2}
	dir := t.TempDir()

	dataFiles := makeDataFiles(t, dir, []string{"a.bin", "b.bin", "c.bin", "d.bin"}, 1024)
	original := readFiles(t, dataFiles)

	groups, _ := PlanStripes(dataFiles, cfg, dir)
	res, err := EncodeStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("EncodeStripe: %v", err)
	}

	// Delete 2 data shards (keep NewPath intact).
	os.Remove(groups[0][0].NewPath)
	os.Remove(groups[0][3].NewPath)

	if err := ReconstructStripe(groups[0], cfg, res.OriginalSizes, res.PaddedSize); err != nil {
		t.Fatalf("ReconstructStripe: %v", err)
	}

	r0, _ := os.ReadFile(groups[0][0].NewPath)
	r3, _ := os.ReadFile(groups[0][3].NewPath)
	if !bytes.Equal(r0, original[0]) {
		t.Error("recovered shard #1 does not match")
	}
	if !bytes.Equal(r3, original[3]) {
		t.Error("recovered shard #4 does not match")
	}
}

func TestReconstructStripe_TooManyMissing(t *testing.T) {
	cfg := Config{K: 4, M: 2}
	dir := t.TempDir()

	dataFiles := makeDataFiles(t, dir, []string{"a.bin", "b.bin", "c.bin", "d.bin"}, 100)
	groups, _ := PlanStripes(dataFiles, cfg, dir)
	EncodeStripe(groups[0], cfg)

	// Delete 3 data shards (> m=2).
	os.Remove(groups[0][0].NewPath)
	os.Remove(groups[0][1].NewPath)
	os.Remove(groups[0][2].NewPath)

	err := ReconstructStripe(groups[0], cfg, nil, 0)
	if err == nil {
		t.Error("expected error with 3 missing data shards when m=2")
	}
}

// ---------------------------------------------------------------------------
// VerifyStripe (corruption)
// ---------------------------------------------------------------------------

func TestVerifyStripe_Corrupted(t *testing.T) {
	cfg := Config{K: 3, M: 2}
	dir := t.TempDir()

	dataFiles := makeDataFiles(t, dir, []string{"a.bin", "b.bin", "c.bin"}, 1024)
	groups, _ := PlanStripes(dataFiles, cfg, dir)
	EncodeStripe(groups[0], cfg)

	// Corrupt a data shard.
	b, _ := os.ReadFile(groups[0][0].NewPath)
	b[0] ^= 0xFF
	os.WriteFile(groups[0][0].NewPath, b, 0644)

	ok, err := VerifyStripe(groups[0], cfg)
	if err != nil {
		t.Fatalf("VerifyStripe: %v", err)
	}
	if ok {
		t.Error("VerifyStripe should return false for corrupted shard")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: encode → kill → reconstruct → compare original
// ---------------------------------------------------------------------------

func TestRoundTrip_File(t *testing.T) {
	cfgs := []Config{{4, 2}, {8, 4}}
	for _, cfg := range cfgs {
		t.Run(cfg.String(), func(t *testing.T) {
			dir := t.TempDir()
			names := make([]string, cfg.K)
			for i := range names {
				names[i] = randomName(i, "bin")
			}
			dataFiles := makeDataFiles(t, dir, names, 1+iRand(t)*512) // unequal
			original := readFiles(t, dataFiles)

			groups, _ := PlanStripes(dataFiles, cfg, dir)
			res, err := EncodeStripe(groups[0], cfg)
			if err != nil {
				t.Fatalf("EncodeStripe: %v", err)
			}

			for missingCount := 1; missingCount <= cfg.M; missingCount++ {
				// Fresh copy of file list for each sub-test.
				files := copyStripeFiles(groups[0])
				for i := 0; i < missingCount; i++ {
					os.Remove(files[i].NewPath)
				}

				if err := ReconstructStripe(files, cfg, res.OriginalSizes, res.PaddedSize); err != nil {
					t.Fatalf("ReconstructStripe missing=%d: %v", missingCount, err)
				}

				for i := 0; i < missingCount; i++ {
					got, _ := os.ReadFile(files[i].NewPath)
					expected := padTo(original[i], int(res.PaddedSize))
					expected = expected[:res.OriginalSizes[i]]
					if !bytes.Equal(got, expected) {
						t.Errorf("missing=%d: recovered shard #%d does not match", missingCount, i)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// encodeBytes / reconstructBytes (memory-based, for private API coverage)
// ---------------------------------------------------------------------------

func TestEncodeBytes_ReconstructBytes(t *testing.T) {
	cfg := Config{K: 4, M: 2}
	data := make([][]byte, 4)
	for i := range data {
		data[i] = randBytes(1024)
	}

	res, err := encodeBytes(data, cfg)
	if err != nil {
		t.Fatalf("encodeBytes: %v", err)
	}

	shards := make([][]byte, cfg.Total())
	copy(shards[:4], data)
	copy(shards[4:], res.Parity)
	shards[1] = nil
	shards[3] = nil

	if err := reconstructBytes(shards, cfg); err != nil {
		t.Fatalf("reconstructBytes: %v", err)
	}

	TrimData(shards[:4], res.OriginalSizes)

	if !bytes.Equal(shards[1], data[1]) {
		t.Error("recovered #1 mismatch")
	}
	if !bytes.Equal(shards[3], data[3]) {
		t.Error("recovered #3 mismatch")
	}
}

func TestEncodeBytes_AllEmpty(t *testing.T) {
	cfg := Config{K: 4, M: 2}
	data := make([][]byte, 4)
	for i := range data {
		data[i] = []byte{}
	}
	res, err := encodeBytes(data, cfg)
	if err != nil {
		t.Fatalf("encodeBytes: %v", err)
	}
	for i, p := range res.Parity {
		if len(p) != 0 {
			t.Errorf("parity[%d] len = %d, want 0", i, len(p))
		}
	}
}

func TestEncodeBytes_Large(t *testing.T) {
	cfg := Config{K: 8, M: 4}
	data := make([][]byte, 8)
	for i := range data {
		data[i] = randBytes(16 << 20) // 16 MiB
	}
	res, err := encodeBytes(data, cfg)
	if err != nil {
		t.Fatalf("encodeBytes: %v", err)
	}
	if len(res.Parity) != 4 {
		t.Fatalf("expected 4 parity, got %d", len(res.Parity))
	}
}

// ---------------------------------------------------------------------------
// StripeFile helpers
// ---------------------------------------------------------------------------

func TestStripeFile_IsData(t *testing.T) {
	d := StripeFile{Type: "D"}
	e := StripeFile{Type: "E"}
	if !d.IsData() {
		t.Error("D should be data")
	}
	if e.IsData() {
		t.Error("E should not be data")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func checkFile(t *testing.T, sf StripeFile, wantOrig, wantNew, wantType string, wantStripe, wantPos int) {
	t.Helper()
	if sf.OrigPath != wantOrig {
		t.Errorf("OrigPath = %q, want %q", sf.OrigPath, wantOrig)
	}
	if sf.NewPath != wantNew {
		t.Errorf("NewPath = %q, want %q", sf.NewPath, wantNew)
	}
	if sf.Type != wantType {
		t.Errorf("Type = %q, want %q", sf.Type, wantType)
	}
	if sf.Stripe != wantStripe {
		t.Errorf("Stripe = %d, want %d", sf.Stripe, wantStripe)
	}
	if sf.Position != wantPos {
		t.Errorf("Position = %d, want %d", sf.Position, wantPos)
	}
}

func makeDataFiles(t *testing.T, dir string, names []string, size int) []string {
	t.Helper()
	var paths []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, randBytes(size), 0644); err != nil {
			t.Fatalf("create test file: %v", err)
		}
		paths = append(paths, path)
	}
	return paths
}

func makeDataFilesSized(t *testing.T, dir string, names []string, sizes []int) []string {
	t.Helper()
	var paths []string
	for i, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, randBytes(sizes[i]), 0644); err != nil {
			t.Fatalf("create test file: %v", err)
		}
		paths = append(paths, path)
	}
	return paths
}

func readFiles(t *testing.T, paths []string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		out = append(out, b)
	}
	return out
}

func copyStripeFiles(src []StripeFile) []StripeFile {
	dst := make([]StripeFile, len(src))
	copy(dst, src)
	return dst
}

func randomName(i int, ext string) string {
	letters := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	if i < len(letters) {
		return letters[i] + "." + ext
	}
	return string(rune('a'+i)) + "." + ext
}

func iRand(t *testing.T) int {
	t.Helper()
	return int(t.Name()[0]) // deterministic "random" per test name
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
