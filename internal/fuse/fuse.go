package fuse

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"syscall"

	"github.com/ddy2006/packfs/internal/shard"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// Mount mounts a read-only FUSE filesystem for the given dataset at mountPoint.
func Mount(db *sql.DB, datasetID int, datasetRoot, compress, mountPoint string) (*fuse.Server, error) {
	ctx := context.Background()
	idx, err := BuildIndex(ctx, db, datasetID, datasetRoot, compress)
	if err != nil {
		return nil, err
	}
	if len(idx.Files) == 0 {
		return nil, errors.E("no files found for dataset", "dataset_id", datasetID)
	}

	tree := idx.DirTree()

	root := &rootNode{idx: idx, tree: tree}

	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			Debug:      false,
			AllowOther: false,
			Name:       "packfs",
			FsName:     "packfs",
		},
	}

	server, err := fs.Mount(mountPoint, root, opts)
	if err != nil {
		return nil, errors.WrapE(err, "mount FUSE", "mount_point", mountPoint)
	}

	logrus.Infof("FUSE mounted at %s (%d files)", mountPoint, len(idx.Files))
	return server, nil
}

// ====== rootNode ======

type rootNode struct {
	fs.Inode
	idx  *Index
	tree *DirNode // stored for OnAdd
}

var _ fs.InodeEmbedder = (*rootNode)(nil)
var _ fs.NodeGetattrer = (*rootNode)(nil)
var _ fs.NodeOnAdder = (*rootNode)(nil)

func (n *rootNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0555
	out.Nlink = 2
	return 0
}

func (n *rootNode) OnAdd(ctx context.Context) {
	n.buildTree(ctx, n.tree)
}

func (n *rootNode) buildTree(ctx context.Context, tree *DirNode) {
	for name, child := range tree.Children {
		if child.IsDir() {
			dn := &dirNode{idx: n.idx, tree: child}
			n.AddChild(name, n.NewPersistentInode(ctx, dn, fs.StableAttr{Mode: syscall.S_IFDIR | 0555}), true)
		} else {
			fn := &fileNode{idx: n.idx, segments: child.Segments}
			n.AddChild(name, n.NewPersistentInode(ctx, fn, fs.StableAttr{Mode: syscall.S_IFREG | 0444}), false)
		}
	}
}

func (n *rootNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return n.readDir(), 0
}

func (n *rootNode) readDir() fs.DirStream {
	entries := n.Children()
	result := make([]fuse.DirEntry, 0, len(entries))
	for name, child := range entries {
		result = append(result, fuse.DirEntry{
			Name: name,
			Mode: child.Mode(),
			Ino:  child.StableAttr().Ino,
		})
	}
	return fs.NewListDirStream(result)
}

// ====== dirNode ======

type dirNode struct {
	fs.Inode
	idx  *Index
	tree *DirNode // stored for OnAdd
}

var _ fs.InodeEmbedder = (*dirNode)(nil)
var _ fs.NodeGetattrer = (*dirNode)(nil)
var _ fs.NodeOnAdder = (*dirNode)(nil)

func (n *dirNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0555
	out.Nlink = 2
	return 0
}

func (n *dirNode) OnAdd(ctx context.Context) {
	for name, child := range n.tree.Children {
		if child.IsDir() {
			dn := &dirNode{idx: n.idx, tree: child}
			n.AddChild(name, n.NewPersistentInode(ctx, dn, fs.StableAttr{Mode: syscall.S_IFDIR | 0555}), true)
		} else {
			fn := &fileNode{idx: n.idx, segments: child.Segments}
			n.AddChild(name, n.NewPersistentInode(ctx, fn, fs.StableAttr{Mode: syscall.S_IFREG | 0444}), false)
		}
	}
}

func (n *dirNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return n.readDir(), 0
}

func (n *dirNode) readDir() fs.DirStream {
	entries := n.Children()
	result := make([]fuse.DirEntry, 0, len(entries))
	for name, child := range entries {
		result = append(result, fuse.DirEntry{
			Name: name,
			Mode: child.Mode(),
			Ino:  child.StableAttr().Ino,
		})
	}
	return fs.NewListDirStream(result)
}

// ====== fileNode ======

type fileNode struct {
	fs.Inode
	idx      *Index
	segments []SegmentLoc
}

var _ fs.InodeEmbedder = (*fileNode)(nil)
var _ fs.NodeOpener = (*fileNode)(nil)
var _ fs.NodeGetattrer = (*fileNode)(nil)

func (n *fileNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | 0444
	out.Size = uint64(n.fileSize())
	out.Nlink = 1
	return 0
}

func (n *fileNode) fileSize() int64 {
	var total int64
	for _, s := range n.segments {
		total += s.Size
	}
	return total
}

func (n *fileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return &fileHandle{
		idx:      n.idx,
		segments: n.segments,
	}, 0, 0
}

// ====== fileHandle ======

type fileHandle struct {
	idx      *Index
	segments []SegmentLoc
	mu       sync.Mutex
}

var _ fs.FileReader = (*fileHandle)(nil)

func (fh *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	if off < 0 {
		off = 0
	}

	var readBuf []byte
	var skipped int64

	for _, seg := range fh.segments {
		segEnd := skipped + seg.Size
		if off >= segEnd {
			skipped = segEnd
			continue
		}

		readStart := off - skipped
		if readStart < 0 {
			readStart = 0
		}
		readLen := int64(len(dest)) - int64(len(readBuf))
		if readStart+readLen > seg.Size {
			readLen = seg.Size - readStart
		}

		data, err := readSegment(seg, readStart, readLen, fh.idx.Compress)
		if err != nil {
			logrus.Errorf("read segment from %s: %v", seg.ShardPath, err)
			return nil, syscall.EIO
		}

		readBuf = append(readBuf, data...)
		if int64(len(readBuf)) >= int64(len(dest)) {
			break
		}
		skipped = segEnd
	}

	if len(readBuf) == 0 {
		return nil, 0
	}
	return fuse.ReadResultData(readBuf), 0
}

// readSegment reads a portion of a segment from its shard file.
func readSegment(seg SegmentLoc, start, length int64, compress string) ([]byte, error) {
	f, err := os.Open(seg.ShardPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch {
	case compress == "segment:zstd" || compress == "segment:xz":
		csize := seg.Csize
		if csize <= 0 {
			csize = seg.Size
		}
		compressed := make([]byte, csize)
		if _, err := f.ReadAt(compressed, seg.Offset); err != nil {
			return nil, err
		}
		isXZ := compress == "segment:xz"
		decompressed, err := shard.DecompressAll(compressed, isXZ)
		if err != nil {
			return nil, err
		}
		if start+length > int64(len(decompressed)) {
			length = int64(len(decompressed)) - start
		}
		return decompressed[start : start+length], nil

	case compress == "zstd" || compress == "xz":
		shardData, err := os.ReadFile(seg.ShardPath)
		if err != nil {
			return nil, err
		}
		isXZ := compress == "xz"
		decompressed, err := shard.DecompressAll(shardData, isXZ)
		if err != nil {
			return nil, err
		}
		absStart := seg.Offset + start
		if absStart+length > int64(len(decompressed)) {
			length = int64(len(decompressed)) - absStart
		}
		return decompressed[absStart : absStart+length], nil

	default:
		buf := make([]byte, length)
		n, err := f.ReadAt(buf, seg.Offset+start)
		if err != nil && n == 0 {
			return nil, err
		}
		return buf[:n], nil
	}
}
