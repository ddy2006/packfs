// Package fuse provides a read-only FUSE filesystem for packfs datasets.
// It builds an in-memory index from the SQLite database mapping file paths
// to their physical byte ranges in shard files, then exposes them via FUSE.
package fuse

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"

	"github.com/kaichao/gopkg/errors"
)

// SegmentLoc describes where a file segment lives on disk.
type SegmentLoc struct {
	ShardPath string // absolute path to the shard file
	Offset    int64  // byte offset within the shard file
	Size      int64  // uncompressed data size
	Csize     int64  // compressed data size (0 = not segment-compressed)
	FileSize  int64  // total original file size
}

// Index maps virtual file paths to their physical segment locations.
// file_path (as stored in t_file, relative) → ordered list of SegmentLoc.
type Index struct {
	Files    map[string][]SegmentLoc
	Compress string // overall compress mode for all files ("", "segment:zstd", "segment:xz", "zstd", "xz")
}

// BuildIndex queries the database and builds an in-memory index for all files
// belonging to the given dataset. datasetRoot is the dataset's current_path
// (where shard files live on disk).
func BuildIndex(ctx context.Context, db *sql.DB, datasetID int, datasetRoot, compress string) (*Index, error) {
	query := `
		SELECT f.file_path, f.file_size,
		       COALESCE(s.file_path, '') AS shard_rel,
		       COALESCE(seg.offset, 0), COALESCE(seg.size, 0), COALESCE(seg.csize, 0)
		FROM t_file f
		JOIN t_segment seg ON seg.file = f.id
		JOIN t_shard s ON s.id = seg.shard
		WHERE f.dataset = ?
		ORDER BY f.file_path, seg.offset`

	rows, err := db.QueryContext(ctx, query, datasetID)
	if err != nil {
		return nil, errors.WrapE(err, "query file segments", "dataset_id", datasetID)
	}
	defer rows.Close()

	idx := &Index{
		Files:    make(map[string][]SegmentLoc),
		Compress: compress,
	}

	for rows.Next() {
		var fp string
		var fileSize int64
		var shardRel string
		var offset, size, csize int64
		if err := rows.Scan(&fp, &fileSize, &shardRel, &offset, &size, &csize); err != nil {
			return nil, errors.WrapE(err, "scan segment row")
		}

		shardAbs := filepath.Join(datasetRoot, shardRel)
		idx.Files[fp] = append(idx.Files[fp], SegmentLoc{
			ShardPath: shardAbs,
			Offset:    offset,
			Size:      size,
			Csize:     csize,
			FileSize:  fileSize,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errors.WrapE(err, "iterate segment rows")
	}

	return idx, nil
}

// DirTree builds a virtual directory tree from the file paths in the index.
// Returns a tree of path nodes where leaves hold segment locations.
func (idx *Index) DirTree() *DirNode {
	root := &DirNode{
		Name:     "/",
		Children: make(map[string]*DirNode),
	}

	for fp, segs := range idx.Files {
		parts := splitPath(fp)
		cur := root
		for i, part := range parts {
			if i == len(parts)-1 {
				// leaf: file node
				cur.Children[part] = &DirNode{
					Name:     part,
					Segments: segs,
				}
			} else {
				// intermediate directory
				if cur.Children[part] == nil {
					cur.Children[part] = &DirNode{
						Name:     part,
						Children: make(map[string]*DirNode),
					}
				}
				cur = cur.Children[part]
			}
		}
	}

	return root
}

// DirNode is a node in the virtual directory tree.
type DirNode struct {
	Name     string
	Children map[string]*DirNode // nil for files
	Segments []SegmentLoc        // nil for directories
}

// IsDir returns true if this node is a directory.
func (n *DirNode) IsDir() bool { return n.Segments == nil }

// FileSize returns the total uncompressed file size by summing all segment sizes.
func (n *DirNode) FileSize() int64 {
	var total int64
	for _, s := range n.Segments {
		total += s.Size
	}
	return total
}

func splitPath(p string) []string {
	p = filepath.Clean(p)
	// Split into components, handling leading/trailing slashes
	var parts []string
	for _, part := range strings.Split(p, string(filepath.Separator)) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}
