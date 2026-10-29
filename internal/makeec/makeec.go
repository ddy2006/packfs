// Package makeec builds EC and PAD shards for an arcset from PG placement.
package makeec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options controls make-ec.
type Options struct {
	ArcsetID  int64
	ChunkSize int
	Group     *int
	Stripe    *int
}

// Run builds all EC/PAD shards for an arcset.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	if opts.ChunkSize == 0 {
		opts.ChunkSize = ec.DefaultChunkSize
	}
	if err := ec.ValidateChunkSize(opts.ChunkSize); err != nil {
		return err
	}
	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, opts.ArcsetID)
	if err != nil {
		return fmt.Errorf("find arcset: %w", err)
	}
	if a.Status != "building" || (a.BuildPhase != "data" && a.BuildPhase != "ec") {
		return fmt.Errorf("arcset must be building/data or building/ec, got %s/%s",
			a.Status, a.BuildPhase)
	}
	if a.Health != "ok" {
		return fmt.Errorf("arcset health is %s", a.Health)
	}

	meta, err := parseMeta(a.Metadata)
	if err != nil {
		return err
	}
	if meta.m == 0 {
		if err := validateData(ctx, store, a); err != nil {
			return err
		}
		return store.CompleteBuild(ctx, opts.ArcsetID)
	}

	rows, err := store.ListShards(ctx, opts.ArcsetID)
	if err != nil {
		return err
	}
	stripes := groupStripes(rows, meta.k)
	if len(stripes) == 0 {
		return fmt.Errorf("arcset has no DATA shards")
	}
	if err := store.SetBuildPhase(ctx, opts.ArcsetID, "ec"); err != nil {
		return err
	}

	keys := make([]stripeKey, 0, len(stripes))
	for key := range stripes {
		if opts.Group != nil && key.group != *opts.Group {
			continue
		}
		if opts.Stripe != nil && key.stripe != *opts.Stripe {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].group != keys[j].group {
			return keys[i].group < keys[j].group
		}
		return keys[i].stripe < keys[j].stripe
	})
	if len(keys) == 0 {
		return fmt.Errorf("no matching stripes")
	}

	for _, key := range keys {
		if err := buildStripe(ctx, store, a, meta, key, stripes[key], opts.ChunkSize); err != nil {
			return fmt.Errorf("stripe group=%d seq=%d: %w", key.group, key.stripe, err)
		}
	}

	if opts.Group == nil && opts.Stripe == nil {
		return store.CompleteBuild(ctx, opts.ArcsetID)
	}
	return nil
}

type meta struct {
	k, m           int
	groupCount     int
	partitionWidth int
	seqWidth       int
	layout         string
	format         string
	compress       string
}

func parseMeta(m map[string]any) (meta, error) {
	ecStr := metaString(m, "ec")
	parts := strings.SplitN(ecStr, "+", 2)
	if len(parts) != 2 {
		return meta{}, fmt.Errorf("invalid ec metadata %q", ecStr)
	}
	k, err := strconv.Atoi(parts[0])
	if err != nil {
		return meta{}, fmt.Errorf("invalid ec metadata %q", ecStr)
	}
	mcount, err := strconv.Atoi(parts[1])
	if err != nil {
		return meta{}, fmt.Errorf("invalid ec metadata %q", ecStr)
	}
	out := meta{
		k:              k,
		m:              mcount,
		groupCount:     metaInt(m, "group_count"),
		partitionWidth: metaInt(m, "partition_width"),
		seqWidth:       metaInt(m, "seq_width"),
		layout:         metaString(m, "layout"),
		format:         metaString(m, "format"),
		compress:       metaString(m, "compress"),
	}
	if out.groupCount <= 0 {
		out.groupCount = 1
	}
	if out.partitionWidth == 0 {
		out.partitionWidth = 2
	}
	if out.seqWidth == 0 {
		out.seqWidth = 3
	}
	if out.layout == "" {
		out.layout = "raid3"
	}
	if out.format == "" {
		out.format = "bin"
	}
	if out.k <= 0 {
		return meta{}, fmt.Errorf("invalid k in ec metadata")
	}
	return out, nil
}

type stripeKey struct {
	group  int
	stripe int
}

func groupStripes(rows []*arcset.ShardRow, k int) map[stripeKey]map[int]*arcset.ShardRow {
	out := map[stripeKey]map[int]*arcset.ShardRow{}
	for _, row := range rows {
		if row.GroupNo == nil || row.StripeSeq == nil || row.Pos == nil {
			continue
		}
		if row.Type != "DATA" || *row.Pos >= k {
			continue
		}
		key := stripeKey{*row.GroupNo, *row.StripeSeq}
		if out[key] == nil {
			out[key] = map[int]*arcset.ShardRow{}
		}
		out[key][*row.Pos] = row
	}
	return out
}

func buildStripe(ctx context.Context, store *arcset.PGStore, a *arcset.Arcset,
	m meta, key stripeKey, data map[int]*arcset.ShardRow, chunkSize int) error {

	dataPaths := make([]string, m.k)
	dataSizes := make([]int64, m.k)
	padPaths := make([]string, m.k)
	for pos := 0; pos < m.k; pos++ {
		row := data[pos]
		if row != nil && row.FilePath != "" {
			dataPaths[pos] = filepath.Join(a.CurrentPath, row.FilePath)
			dataSizes[pos] = row.FileSize
			continue
		}
		part := partition(m.layout, key.group, pos, key.stripe, m.k, m.m)
		name := fmt.Sprintf("%0*dP%s", m.seqWidth, key.stripe, shardExt(m.format, m.compress))
		padPaths[pos] = filepath.Join(a.CurrentPath, fmt.Sprintf("%0*d", m.partitionWidth, part), name)
	}

	ecPaths := make([]string, m.m)
	partitions := make([]int, m.m)
	for i := 0; i < m.m; i++ {
		pos := m.k + i
		partitions[i] = partition(m.layout, key.group, pos, key.stripe, m.k, m.m)
		name := fmt.Sprintf("%0*dE%s", m.seqWidth, key.stripe, shardExt(m.format, m.compress))
		ecPaths[i] = filepath.Join(a.CurrentPath, fmt.Sprintf("%0*d", m.partitionWidth, partitions[i]), name)
	}

	padded, err := ec.EncodeStripeStreaming(dataPaths, dataSizes, ecPaths, m.k, m.m, chunkSize)
	if err != nil {
		return err
	}

	// Create zero-byte PAD files for missing data positions.
	for pos, p := range padPaths {
		if p == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(p, nil, 0644); err != nil {
			return fmt.Errorf("create PAD %s: %w", p, err)
		}
		_ = pos
	}

	// Verify the newly written stripe before changing PG rows.
	verifyFiles := make([]ec.StreamFile, m.k+m.m)
	for pos := 0; pos < m.k; pos++ {
		if dataPaths[pos] != "" {
			verifyFiles[pos] = ec.StreamFile{Path: dataPaths[pos], Size: dataSizes[pos], Type: 'D'}
		} else {
			verifyFiles[pos] = ec.StreamFile{Path: padPaths[pos], Size: 0, Type: 'P'}
		}
	}
	for i := 0; i < m.m; i++ {
		info, err := os.Stat(ecPaths[i])
		if err != nil {
			return err
		}
		verifyFiles[m.k+i] = ec.StreamFile{Path: ecPaths[i], Size: info.Size(), Type: 'E'}
	}
	ok, err := ec.VerifyStripeStreaming(verifyFiles, m.k, m.m, padded, chunkSize)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("stripe verification failed")
	}

	// Update DATA metadata and recreate PAD/EC rows.
	if err := store.DeleteECAndPADShards(ctx, int64(a.ID), key.group, key.stripe); err != nil {
		return err
	}
	for _, row := range data {
		md := row.Metadata
		if md == nil {
			md = map[string]any{}
		}
		md["padded_size"] = padded
		md["original_size"] = row.FileSize
		md["data_bytes"] = row.FileSize
		if err := store.UpdateShardMetadata(ctx, row.ID, md); err != nil {
			return err
		}
	}

	for pos, p := range padPaths {
		if p == "" {
			continue
		}
		part := partition(m.layout, key.group, pos, key.stripe, m.k, m.m)
		relPath, _ := filepath.Rel(a.CurrentPath, p)
		row := &arcset.ShardRow{
			ArcsetID:  int64(a.ID),
			StripeSeq: intPtr(key.stripe),
			GroupNo:   intPtr(key.group),
			Pos:       intPtr(pos),
			Partition: intPtr(part),
			FilePath:  relPath,
			FileSize:  0,
			Type:      "PAD",
			Metadata:  map[string]any{"padded_size": padded, "original_size": int64(0)},
		}
		if err := store.InsertShard(ctx, row); err != nil {
			return err
		}
	}

	for i, p := range ecPaths {
		sum, err := ec.SHA256File(p)
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(a.CurrentPath, p)
		row := &arcset.ShardRow{
			ArcsetID:  int64(a.ID),
			StripeSeq: intPtr(key.stripe),
			GroupNo:   intPtr(key.group),
			Pos:       intPtr(m.k + i),
			Partition: intPtr(partitions[i]),
			FilePath:  relPath,
			FileSize:  padded,
			Type:      "EC",
			Metadata:  map[string]any{"padded_size": padded, "original_size": int64(0)},
			SHA256:    &sum,
		}
		if err := store.InsertShard(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func validateData(ctx context.Context, store *arcset.PGStore, a *arcset.Arcset) error {
	rows, err := store.ListShards(ctx, int64(a.ID))
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Type != "DATA" {
			continue
		}
		path := filepath.Join(a.CurrentPath, row.FilePath)
		sum, err := ec.SHA256File(path)
		if err != nil {
			return fmt.Errorf("validate %s: %w", row.FilePath, err)
		}
		if row.SHA256 == nil || sum != *row.SHA256 {
			return fmt.Errorf("checksum mismatch for %s", row.FilePath)
		}
	}
	return nil
}

func partition(layout string, group, pos, stripe, k, m int) int {
	if layout == "raid5" {
		return group*(k+m) + ((pos + stripe) % (k + m))
	}
	return group*(k+m) + pos
}

func shardExt(format, compress string) string {
	if format == "" {
		format = "bin"
	}
	algo := ""
	switch compress {
	case "zstd", "zstd_seekable", "segment:zstd":
		algo = "zst"
	case "xz", "segment:xz":
		algo = "xz"
	}
	switch compress {
	case "zstd", "xz", "zstd_seekable":
		return "." + format + "." + algo
	case "segment:zstd", "segment:xz":
		return "." + algo + "." + format
	default:
		return "." + format
	}
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func metaInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func intPtr(v int) *int { return &v }
