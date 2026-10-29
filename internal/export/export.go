// Package export builds arcset.json from PostgreSQL placement.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options controls export.
type Options struct {
	ArcsetID   int64
	TargetRoot string
}

// Run writes arcset.json and returns its path.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (string, error) {
	store := arcset.NewPGStore(pool)
	a, err := store.FindByID(ctx, opts.ArcsetID)
	if err != nil {
		return "", fmt.Errorf("find arcset: %w", err)
	}
	if a.Status != "completed" {
		return "", fmt.Errorf("arcset must be completed, got %s", a.Status)
	}
	relations, err := store.ListRelations(ctx, int64(a.ID))
	if err != nil {
		return "", err
	}
	shards, err := store.ListShards(ctx, int64(a.ID))
	if err != nil {
		return "", err
	}
	meta, err := parseMeta(a.Metadata)
	if err != nil {
		return "", err
	}

	refByID := map[int64]arcset.RelationDatasetRow{}
	for _, rel := range relations {
		refByID[rel.DatasetRef] = rel
	}
	used := map[int]int64{}
	for _, sh := range shards {
		if sh.Partition != nil {
			used[*sh.Partition] += sh.FileSize
		}
	}

	partitions := make([]partitionManifest, 0, meta.groups*(meta.k+meta.m))
	for group := 0; group < meta.groups; group++ {
		for pos := 0; pos < meta.k+meta.m; pos++ {
			part := group*(meta.k+meta.m) + pos
			partitions = append(partitions, partitionManifest{
				Partition:         part,
				GroupNo:           group,
				Pos:               pos,
				PartitionCapacity: meta.capacity,
				UsedCapacity:      used[part],
				AvailableCapacity: max64(0, meta.capacity-used[part]),
			})
		}
	}

	datasets := make([]datasetManifest, 0, len(relations))
	for _, rel := range relations {
		datasets = append(datasets, datasetManifest{
			UID:            rel.UID,
			Key:            rel.DatasetKey,
			Name:           rel.Name,
			SQLitePath:     rel.SQLitePath,
			ShardCount:     metaInt64(rel.Metadata, "shard_count"),
			ShardSortRegex: metaString(rel.Metadata, "shard_sort_regex"),
			ShardSortIndex: metaString(rel.Metadata, "shard_sort_index"),
		})
	}

	shardItems := make([]shardManifest, 0, len(shards))
	for _, sh := range shards {
		item := shardManifest{
			Name:         sh.FilePath,
			OriginalName: derefString(sh.OriginalName),
			DatasetKey:   derefString(sh.DatasetKey),
			Type:         sh.Type,
			Partition:    derefInt(sh.Partition),
			GroupNo:      derefInt(sh.GroupNo),
			Pos:          derefInt(sh.Pos),
			Seq:          derefInt(sh.StripeSeq),
			Size:         sh.FileSize,
			SHA256:       derefString(sh.SHA256),
			PaddedSize:   metaInt64(sh.Metadata, "padded_size"),
			OriginalSize: metaInt64(sh.Metadata, "original_size"),
		}
		if sh.DatasetRef != nil {
			if rel, ok := refByID[*sh.DatasetRef]; ok {
				item.DatasetUID = rel.UID
			}
		}
		shardItems = append(shardItems, item)
	}

	manifest := manifest{
		ManifestVersion: 1,
		Arcset: arcsetManifest{
			Name:           a.Name,
			EC:             ecManifest{K: meta.k, M: meta.m},
			Layout:         meta.layout,
			PartitionWidth: meta.partitionWidth,
			SeqWidth:       meta.seqWidth,
			GroupCount:     meta.groups,
			PartitionCap:   meta.capacity,
			DataCapacity:   int64(meta.k) * int64(meta.groups) * meta.capacity,
			RawCapacity:    int64(meta.k+meta.m) * int64(meta.groups) * meta.capacity,
			ShardCount:     len(shards),
			Status:         a.Status,
			Health:         a.Health,
			ParityDegraded: a.ParityDegraded,
		},
		Partitions: partitions,
		Datasets:   datasets,
		Shards:     shardItems,
	}

	root := opts.TargetRoot
	if root == "" {
		root = a.CurrentPath
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(root, "arcset.json")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

type arcsetMeta struct {
	k, m           int
	groups         int
	partitionWidth int
	seqWidth       int
	layout         string
	capacity       int64
}

func parseMeta(m map[string]any) (arcsetMeta, error) {
	ecStr := metaString(m, "ec")
	var k, mm int
	if _, err := fmt.Sscanf(ecStr, "%d+%d", &k, &mm); err != nil {
		return arcsetMeta{}, fmt.Errorf("invalid ec metadata %q", ecStr)
	}
	out := arcsetMeta{
		k:              k,
		m:              mm,
		groups:         metaInt(m, "group_count"),
		partitionWidth: metaInt(m, "partition_width"),
		seqWidth:       metaInt(m, "seq_width"),
		layout:         metaString(m, "layout"),
		capacity:       metaInt64(m, "partition_capacity"),
	}
	if out.groups <= 0 {
		out.groups = 1
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
	return out, nil
}

type manifest struct {
	ManifestVersion int                 `json:"manifest_version"`
	Arcset          arcsetManifest      `json:"arcset"`
	Partitions      []partitionManifest `json:"partitions"`
	Datasets        []datasetManifest   `json:"datasets"`
	Shards          []shardManifest     `json:"shards"`
}

type arcsetManifest struct {
	Name           string     `json:"name"`
	EC             ecManifest `json:"ec"`
	Layout         string     `json:"layout"`
	PartitionWidth int        `json:"partition_width"`
	SeqWidth       int        `json:"seq_width"`
	GroupCount     int        `json:"group_count"`
	PartitionCap   int64      `json:"partition_capacity"`
	DataCapacity   int64      `json:"data_capacity"`
	RawCapacity    int64      `json:"raw_capacity"`
	ShardCount     int        `json:"shard_count"`
	Status         string     `json:"status"`
	Health         string     `json:"health"`
	ParityDegraded bool       `json:"parity_degraded"`
}

type ecManifest struct {
	K int `json:"k"`
	M int `json:"m"`
}

type partitionManifest struct {
	Partition         int   `json:"partition"`
	GroupNo           int   `json:"group_no"`
	Pos               int   `json:"pos"`
	PartitionCapacity int64 `json:"partition_capacity"`
	UsedCapacity      int64 `json:"used_capacity"`
	AvailableCapacity int64 `json:"available_capacity"`
}

type datasetManifest struct {
	UID            string `json:"uid"`
	Key            string `json:"key"`
	Name           string `json:"name"`
	SQLitePath     string `json:"sqlite_path"`
	ShardCount     int64  `json:"shard_count"`
	ShardSortRegex string `json:"shard_sort_regex,omitempty"`
	ShardSortIndex string `json:"shard_sort_index,omitempty"`
}

type shardManifest struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name,omitempty"`
	DatasetUID   string `json:"dataset_uid,omitempty"`
	DatasetKey   string `json:"dataset_key,omitempty"`
	Type         string `json:"type"`
	Partition    int    `json:"partition"`
	GroupNo      int    `json:"group_no"`
	Pos          int    `json:"pos"`
	Seq          int    `json:"seq"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256,omitempty"`
	PaddedSize   int64  `json:"padded_size,omitempty"`
	OriginalSize int64  `json:"original_size,omitempty"`
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
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

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
