package shard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaichao/gopkg/errors"
)

// ShardDefName 解析后的 shard 定义文件名。
//
// 文件名格式：<id>[.<compress>].<format>.def
//
//	0001.bin.def           → ID="0001", Compress="", Format="bin"
//	0002.zst.bin.def       → ID="0002", Compress="zst", Format="bin"
//	0003.tar.def           → ID="0003", Compress="", Format="tar"
type ShardDefName struct {
	ID       string
	Compress string
	Format   string
}

// SegmentDef 定义文件中的一行，描述一个要打包的片段。
//
// 不以 '{' 开头 → 相对路径，完整文件（offset=0, size=0=整个文件）。
// 以 '{' 开头 → JSON 行：{"path":"...","offset":0,"size":1024}。
type SegmentDef struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	FileID int    `json:"file_id"`
}

// ParseDefName 解析 .def 文件名。
func ParseDefName(filename string) (ShardDefName, error) {
	base := filepath.Base(filename)
	if !strings.HasSuffix(base, ".def") {
		return ShardDefName{}, errors.E("not a .def file", "name", base)
	}
	core := strings.TrimSuffix(base, ".def")

	// 倒数第一个 . → format, 倒数第二个 .(可选) → compress
	parts := strings.Split(core, ".")
	if len(parts) < 2 {
		return ShardDefName{}, errors.E("invalid def filename, expected <id>.<format>.def", "name", base)
	}
	format := parts[len(parts)-1]
	id := parts[0]
	compress := ""
	if len(parts) == 3 {
		compress = parts[1]
	}
	return ShardDefName{ID: id, Compress: compress, Format: format}, nil
}

// DefFileMeta holds metadata extracted from .def comment lines.
type DefFileMeta struct {
	ArcsetID  int
	DatasetID int
}

// ParseDefFile 解析 .def 文件内容，跳过空行和 # 注释行，返回 SegmentDef 列表。
func ParseDefFile(r io.Reader) ([]SegmentDef, error) {
	_, defs, err := parseDefContent(r)
	return defs, err
}

// ParseDefFileMeta 解析 .def 文件内容，同时返回注释行中的元数据和 SegmentDef 列表。
func ParseDefFileMeta(r io.Reader) (DefFileMeta, []SegmentDef, error) {
	return parseDefContent(r)
}

func parseDefContent(r io.Reader) (DefFileMeta, []SegmentDef, error) {
	var meta DefFileMeta
	var defs []SegmentDef
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if idStr, ok := strings.CutPrefix(line, "# arcset_id: "); ok {
				fmt.Sscanf(idStr, "%d", &meta.ArcsetID)
			}
			if idStr, ok := strings.CutPrefix(line, "# dataset_id: "); ok {
				fmt.Sscanf(idStr, "%d", &meta.DatasetID)
			}
			continue
		}
		if strings.HasPrefix(line, "{") {
			var d SegmentDef
			if err := json.Unmarshal([]byte(line), &d); err != nil {
				return meta, nil, errors.WrapE(err, "parse segment json", "line", line)
			}
			if d.Path == "" {
				return meta, nil, errors.E("segment path is required", "line", line)
			}
			defs = append(defs, d)
		} else {
			defs = append(defs, SegmentDef{Path: line})
		}
	}
	if err := scanner.Err(); err != nil {
		return meta, nil, errors.WrapE(err, "read def file")
	}
	return meta, defs, nil
}

// ReadDefFile opens and parses a .def file by path.
// Deprecated: use ReadDefFileMeta for arcset_id support.
func ReadDefFile(path string) (ShardDefName, []SegmentDef, error) {
	_, _, segs, err := ParseDefFileMetaPath(path)
	return ShardDefName{}, segs, err
}

// ReadDefFileMeta opens a .def file and returns name, meta, and segments.
func ReadDefFileMeta(path string) (ShardDefName, DefFileMeta, []SegmentDef, error) {
	return ParseDefFileMetaPath(path)
}

// ParseDefFileMetaPath opens and parses a .def file, returning name + meta + segments.
func ParseDefFileMetaPath(path string) (ShardDefName, DefFileMeta, []SegmentDef, error) {
	name, err := ParseDefName(path)
	if err != nil {
		return name, DefFileMeta{}, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return name, DefFileMeta{}, nil, errors.WrapE(err, "open def file", "path", path)
	}
	defer f.Close()
	meta, defs, err := parseDefContent(f)
	return name, meta, defs, err
}
