package shard

import (
	"bufio"
	"encoding/json"
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

// ParseDefFile 解析 .def 文件，返回 SegmentDef 列表。
func ParseDefFile(r io.Reader) ([]SegmentDef, error) {
	var defs []SegmentDef
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") {
			var d SegmentDef
			if err := json.Unmarshal([]byte(line), &d); err != nil {
				return nil, errors.WrapE(err, "parse segment json", "line", line)
			}
			if d.Path == "" {
				return nil, errors.E("segment path is required", "line", line)
			}
			defs = append(defs, d)
		} else {
			defs = append(defs, SegmentDef{Path: line})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.WrapE(err, "read def file")
	}
	return defs, nil
}

// ReadDefFile opens and parses a .def file by path.
func ReadDefFile(path string) (ShardDefName, []SegmentDef, error) {
	name, err := ParseDefName(path)
	if err != nil {
		return name, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return name, nil, errors.WrapE(err, "open def file", "path", path)
	}
	defer f.Close()
	defs, err := ParseDefFile(f)
	return name, defs, err
}
