// Package builddata places sorted dataset DATA shards into arcset partitions.
package builddata

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options selects the arcset and optionally one dataset.
type Options struct {
	ArcsetID   int64
	DatasetUID string
}

// BuildData moves sorted DATA shards into the target arcset layout.
func BuildData(ctx context.Context, pool *pgxpool.Pool, opts Options) (int, error) {
	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, opts.ArcsetID)
	if err != nil {
		return 0, fmt.Errorf("find arcset: %w", err)
	}
	if a.Status != "building" || a.BuildPhase != "data" {
		return 0, fmt.Errorf("arcset must be building/data, got %s/%s", a.Status, a.BuildPhase)
	}
	if a.Health != "ok" {
		return 0, fmt.Errorf("arcset health is %s", a.Health)
	}

	state, err := newPlacementState(a)
	if err != nil {
		return 0, err
	}
	rows, err := store.ListShards(ctx, int64(a.ID))
	if err != nil {
		return 0, err
	}
	state.load(rows)

	relations, err := store.ListRelations(ctx, int64(a.ID))
	if err != nil {
		return 0, fmt.Errorf("list relations: %w", err)
	}

	built := 0
	for _, rel := range relations {
		if opts.DatasetUID != "" && rel.UID != opts.DatasetUID {
			continue
		}
		n, err := buildDataset(ctx, store, a, rel, state)
		if err != nil {
			return built, err
		}
		built += n
	}
	if opts.DatasetUID != "" && built == 0 {
		return 0, fmt.Errorf("dataset %s is not registered in arcset %d", opts.DatasetUID, a.ID)
	}
	if state.parityDegraded {
		if err := store.SetParityDegraded(ctx, int64(a.ID), true); err != nil {
			return built, err
		}
	}
	return built, nil
}

func buildDataset(ctx context.Context, store *arcset.PGStore, a *arcset.Arcset,
	rel arcset.RelationDatasetRow, state *placementState) (int, error) {

	dsDB, err := db.OpenDatasetSQLite(rel.SQLitePath, true)
	if err != nil {
		return 0, fmt.Errorf("open dataset %s: %w", rel.UID, err)
	}
	defer dsDB.Close()

	dsStore := dataset.NewDatasetStore(dsDB)
	ds, err := dsStore.FindByID(ctx, 1)
	if err != nil {
		// Dataset SQLite normally has id=1; fall back to the only row.
		limit := 1
		all, findErr := dsStore.Find(ctx, dataset.Filter{Limit: &limit})
		if findErr != nil || len(all) == 0 {
			return 0, fmt.Errorf("find dataset in %s: %w", rel.SQLitePath, err)
		}
		ds = all[0]
	}
	shardStore := shard.NewDatasetStore(dsDB)
	shards, err := shardStore.FindByDataset(ctx, ds.ID)
	if err != nil {
		return 0, fmt.Errorf("list dataset shards: %w", err)
	}
	sorted, err := sortShards(shards, ds)
	if err != nil {
		return 0, err
	}

	shardRoot, _ := ds.Metadata["shard_root"].(string)
	if shardRoot == "" {
		shardRoot = ds.CurrentPath
	}

	placed := 0
	for _, sh := range sorted {
		if state.hasData(rel.DatasetRef, sh.Seq) {
			continue
		}
		row, invalidatedEC, err := state.place(sh, rel, ds, shardRoot)
		if err != nil {
			return placed, err
		}
		if invalidatedEC {
			if err := store.DeleteECShards(ctx, int64(a.ID), *row.GroupNo, *row.StripeSeq); err != nil {
				return placed, err
			}
		}
		if row.ID == 0 {
			if err := store.InsertShard(ctx, row); err != nil {
				return placed, err
			}
		} else {
			if err := store.UpdateShardData(ctx, row); err != nil {
				return placed, err
			}
		}
		state.commit(row)
		placed++
	}
	return placed, nil
}

type posKey struct {
	group  int
	stripe int
	pos    int
}

type placementState struct {
	a              *arcset.Arcset
	k, m           int
	groupCount     int
	partitionWidth int
	seqWidth       int
	capacity       int64
	layout         string
	used           map[int]int64
	rows           map[posKey]*arcset.ShardRow
	nextSeq        map[int]int
	dataRows       map[string]*arcset.ShardRow // datasetRef/seq
	padRows        []*arcset.ShardRow
	parityDegraded bool
}

func newPlacementState(a *arcset.Arcset) (*placementState, error) {
	k, m, err := parseEC(a.Metadata)
	if err != nil {
		return nil, err
	}
	s := &placementState{
		a:              a,
		k:              k,
		m:              m,
		groupCount:     metaInt(a.Metadata, "group_count"),
		partitionWidth: metaInt(a.Metadata, "partition_width"),
		seqWidth:       metaInt(a.Metadata, "seq_width"),
		capacity:       metaInt64(a.Metadata, "partition_capacity"),
		layout:         metaString(a.Metadata, "layout"),
		used:           map[int]int64{},
		rows:           map[posKey]*arcset.ShardRow{},
		nextSeq:        map[int]int{},
		dataRows:       map[string]*arcset.ShardRow{},
	}
	if s.groupCount <= 0 {
		s.groupCount = 1
	}
	if s.partitionWidth == 0 {
		s.partitionWidth = 2
	}
	if s.seqWidth == 0 {
		s.seqWidth = 3
	}
	if s.layout == "" {
		s.layout = "raid3"
	}
	if s.capacity <= 0 {
		return nil, fmt.Errorf("partition_capacity is not set")
	}
	return s, nil
}

func (s *placementState) load(rows []*arcset.ShardRow) {
	for _, row := range rows {
		if row.Partition != nil {
			s.used[*row.Partition] += row.FileSize
		}
		if row.GroupNo != nil && row.StripeSeq != nil && row.Pos != nil {
			key := posKey{*row.GroupNo, *row.StripeSeq, *row.Pos}
			s.rows[key] = row
			if *row.StripeSeq >= s.nextSeq[*row.GroupNo] {
				s.nextSeq[*row.GroupNo] = *row.StripeSeq + 1
			}
		}
		if row.Type == "PAD" && row.Pos != nil && *row.Pos < s.k {
			s.padRows = append(s.padRows, row)
		}
		if row.Type == "DATA" && row.DatasetRef != nil && row.Seq != nil {
			s.dataRows[dataKey(*row.DatasetRef, *row.Seq)] = row
		}
	}
	sort.Slice(s.padRows, func(i, j int) bool {
		if *s.padRows[i].GroupNo != *s.padRows[j].GroupNo {
			return *s.padRows[i].GroupNo < *s.padRows[j].GroupNo
		}
		if *s.padRows[i].StripeSeq != *s.padRows[j].StripeSeq {
			return *s.padRows[i].StripeSeq < *s.padRows[j].StripeSeq
		}
		return *s.padRows[i].Pos < *s.padRows[j].Pos
	})
}

func dataKey(datasetRef int64, seq int) string {
	return fmt.Sprintf("%d/%d", datasetRef, seq)
}

func (s *placementState) hasData(datasetRef int64, seq int) bool {
	_, ok := s.dataRows[dataKey(datasetRef, seq)]
	return ok
}

func (s *placementState) place(sh *shard.Shard, rel arcset.RelationDatasetRow,
	ds *dataset.Dataset, shardRoot string) (*arcset.ShardRow, bool, error) {

	size := sh.FileSize
	if size == 0 {
		if info, err := os.Stat(filepath.Join(shardRoot, sh.FilePath)); err == nil {
			size = info.Size()
		}
	}

	var row *arcset.ShardRow
	var group, stripe, pos, partition int
	var usePad bool
	var invalidatedEC bool

	if s.m > 0 {
		for i, pad := range s.padRows {
			if *pad.Pos >= s.k {
				continue
			}
			p := s.partition(*pad.GroupNo, *pad.Pos, *pad.StripeSeq)
			if s.capacity-s.used[p] < size {
				continue
			}
			row = pad
			group, stripe, pos, partition = *pad.GroupNo, *pad.StripeSeq, *pad.Pos, p
			s.padRows = append(s.padRows[:i], s.padRows[i+1:]...)
			usePad = true
			break
		}
	}
	if row == nil {
		var ok bool
		group, stripe, pos, partition, ok = s.findNewSlot(size)
		if !ok {
			return nil, false, fmt.Errorf("no capacity for shard %s (size=%d) in arcset %s",
				sh.FilePath, size, s.a.Name)
		}
		row = &arcset.ShardRow{ArcsetID: int64(s.a.ID)}
	}

	// Filling a PAD or an empty position in an existing stripe invalidates
	// that stripe's EC shards; make-ec must recompute them.
	if usePad || stripe < s.nextSeq[group] {
		for key, old := range s.rows {
			if old.Type != "EC" || key.group != group || key.stripe != stripe {
				continue
			}
			if old.Partition != nil {
				s.used[*old.Partition] -= old.FileSize
			}
			delete(s.rows, key)
			invalidatedEC = true
		}
		if invalidatedEC {
			s.parityDegraded = true
		}
	}

	origName := filepath.Base(sh.FilePath)
	fileName := fmt.Sprintf("%0*dD-%s-%s", s.seqWidth, stripe, rel.DatasetKey, origName)
	partitionDir := fmt.Sprintf("%0*d", s.partitionWidth, partition)
	relPath := filepath.Join(partitionDir, fileName)
	targetPath := filepath.Join(s.a.CurrentPath, relPath)
	srcPath := filepath.Join(shardRoot, sh.FilePath)
	if err := moveFile(srcPath, targetPath, sh.Checksum); err != nil {
		return nil, false, fmt.Errorf("move shard %s: %w", sh.FilePath, err)
	}

	row.DatasetRef = int64Ptr(rel.DatasetRef)
	row.DatasetKey = stringPtr(rel.DatasetKey)
	row.Seq = intPtr(sh.Seq)
	row.StripeSeq = intPtr(stripe)
	row.GroupNo = intPtr(group)
	row.Pos = intPtr(pos)
	row.Partition = intPtr(partition)
	row.FilePath = relPath
	row.OriginalName = stringPtr(origName)
	row.FileSize = size
	row.Type = "DATA"
	row.SHA256 = stringPtr(sh.Checksum)
	row.Metadata = map[string]any{"data_bytes": sh.FileSize}
	return row, invalidatedEC, nil
}

func (s *placementState) findNewSlot(size int64) (group, stripe, pos, partition int, ok bool) {
	// Fill an existing incomplete stripe first, for both m=0 and m>0.
	for g := 0; g < s.groupCount; g++ {
		for seq := 0; seq < s.nextSeq[g]; seq++ {
			for p := 0; p < s.k; p++ {
				if _, exists := s.rows[posKey{g, seq, p}]; exists {
					continue
				}
				part := s.partition(g, p, seq)
				if s.capacity-s.used[part] >= size {
					return g, seq, p, part, true
				}
			}
		}
	}
	for g := 0; g < s.groupCount; g++ {
		seq := s.nextSeq[g]
		for p := 0; p < s.k; p++ {
			part := s.partition(g, p, seq)
			if s.capacity-s.used[part] < size {
				// Try the next group rather than a partial stripe.
				break
			}
			if p == s.k-1 {
				return g, seq, 0, s.partition(g, 0, seq), true
			}
		}
	}
	return 0, 0, 0, 0, false
}

func (s *placementState) partition(group, pos, stripe int) int {
	if s.layout == "raid5" {
		return group*(s.k+s.m) + ((pos + stripe) % (s.k + s.m))
	}
	return group*(s.k+s.m) + pos
}

func (s *placementState) commit(row *arcset.ShardRow) {
	if row.GroupNo != nil && row.StripeSeq != nil && row.Pos != nil {
		key := posKey{*row.GroupNo, *row.StripeSeq, *row.Pos}
		s.rows[key] = row
		if *row.StripeSeq >= s.nextSeq[*row.GroupNo] {
			s.nextSeq[*row.GroupNo] = *row.StripeSeq + 1
		}
	}
	if row.Partition != nil {
		s.used[*row.Partition] += row.FileSize
	}
	if row.DatasetRef != nil && row.Seq != nil {
		s.dataRows[dataKey(*row.DatasetRef, *row.Seq)] = row
	}
}

func sortShards(in []*shard.Shard, ds *dataset.Dataset) ([]*shard.Shard, error) {
	out := append([]*shard.Shard(nil), in...)
	regexStr := metaString(ds.Metadata, "shard_sort_regex")
	if regexStr == "" {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Seq != out[j].Seq {
				return out[i].Seq < out[j].Seq
			}
			return out[i].FilePath < out[j].FilePath
		})
		return out, nil
	}
	re, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, fmt.Errorf("invalid shard_sort_regex: %w", err)
	}
	indexes, err := parseSortIndexes(metaString(ds.Metadata, "shard_sort_index"))
	if err != nil {
		return nil, err
	}

	type keyed struct {
		sh   *shard.Shard
		keys []string
	}
	items := make([]keyed, 0, len(out))
	for _, sh := range out {
		m := re.FindStringSubmatch(filepath.Base(sh.FilePath))
		if m == nil {
			return nil, fmt.Errorf("shard %s does not match shard_sort_regex", sh.FilePath)
		}
		keys := make([]string, len(indexes))
		for i, idx := range indexes {
			if idx < 0 || idx >= len(m) {
				return nil, fmt.Errorf("shard_sort_index %d out of range for %s", idx, sh.FilePath)
			}
			keys[i] = m[idx]
		}
		items = append(items, keyed{sh: sh, keys: keys})
	}
	sort.SliceStable(items, func(i, j int) bool {
		for k := range items[i].keys {
			c := compareKeys(items[i].keys[k], items[j].keys[k])
			if c != 0 {
				return c < 0
			}
		}
		if items[i].sh.Seq != items[j].sh.Seq {
			return items[i].sh.Seq < items[j].sh.Seq
		}
		return items[i].sh.FilePath < items[j].sh.FilePath
	})
	for i := range items {
		out[i] = items[i].sh
	}
	return out, nil
}

func compareKeys(a, b string) int {
	ai, aerr := strconv.ParseInt(a, 10, 64)
	bi, berr := strconv.ParseInt(b, 10, 64)
	if aerr == nil && berr == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

func parseSortIndexes(s string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("shard_sort_index is required with shard_sort_regex")
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("invalid shard_sort_index %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

func moveFile(src, dst, checksum string) error {
	if _, err := os.Stat(dst); err == nil {
		ok, err := fileMatches(dst, checksum)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("target exists with different checksum: %s", dst)
		}
		if _, err := os.Stat(src); err == nil {
			if err := os.Remove(src); err != nil {
				return fmt.Errorf("remove source after verified target: %w", err)
			}
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Cross-device fallback: copy + verify + rename + remove source.
	tmp := dst + ".tmp"
	if err := copyFile(src, tmp); err != nil {
		return err
	}
	ok, err := fileMatches(tmp, checksum)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if !ok {
		os.Remove(tmp)
		return fmt.Errorf("copied shard checksum mismatch: %s", src)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileMatches(path, checksum string) (bool, error) {
	if checksum == "" {
		return true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)) == checksum, nil
}

func parseEC(metadata map[string]any) (int, int, error) {
	ecStr := metaString(metadata, "ec")
	parts := strings.SplitN(ecStr, "+", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid arcset ec metadata %q", ecStr)
	}
	k, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid arcset ec metadata %q", ecStr)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid arcset ec metadata %q", ecStr)
	}
	return k, m, nil
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func metaInt(m map[string]any, key string) int {
	return int(metaInt64(m, key))
}

func metaInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
func stringPtr(v string) *string {
	return &v
}
