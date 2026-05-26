package shard_test

import (
	"strings"
	"testing"

	"github.com/ddy2006/packfs/internal/shard"
)

func TestParseDefName(t *testing.T) {
	tests := []struct {
		filename string
		want     shard.ShardDefName
		wantErr  bool
	}{
		{"0001.bin.def", shard.ShardDefName{ID: "0001", Compress: "", Format: "bin"}, false},
		{"0002.zst.bin.def", shard.ShardDefName{ID: "0002", Compress: "zst", Format: "bin"}, false},
		{"0003.tar.def", shard.ShardDefName{ID: "0003", Compress: "", Format: "tar"}, false},
		{"/abs/path/001.zst.bin.def", shard.ShardDefName{ID: "001", Compress: "zst", Format: "bin"}, false},
		{"not-a-def.txt", shard.ShardDefName{}, true},
		{"a.def", shard.ShardDefName{}, true}, // only one part
	}
	for _, tt := range tests {
		got, err := shard.ParseDefName(tt.filename)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseDefName(%q) error=%v wantErr=%v", tt.filename, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseDefName(%q)=%+v want %+v", tt.filename, got, tt.want)
		}
	}
}

func TestParseDefFile(t *testing.T) {
	input := `
file_a.txt
{"path":"file_b.txt","offset":1024,"size":4096}
{"path":"file_c.txt","offset":0,"size":2048}
file_d.txt
`
	defs, err := shard.ParseDefFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseDefFile: %v", err)
	}
	if len(defs) != 4 {
		t.Fatalf("expected 4, got %d", len(defs))
	}
	if defs[0].Path != "file_a.txt" || defs[0].Offset != 0 || defs[0].Size != 0 {
		t.Errorf("seg 0: %+v", defs[0])
	}
	if defs[1].Path != "file_b.txt" || defs[1].Offset != 1024 || defs[1].Size != 4096 {
		t.Errorf("seg 1: %+v", defs[1])
	}
	if defs[2].Path != "file_c.txt" || defs[2].Offset != 0 || defs[2].Size != 2048 {
		t.Errorf("seg 2: %+v", defs[2])
	}
	if defs[3].Path != "file_d.txt" || defs[3].Offset != 0 || defs[3].Size != 0 {
		t.Errorf("seg 3: %+v", defs[3])
	}
}

func TestReadDefFile(t *testing.T) {
	t.Skip("requires actual file on disk")
}
