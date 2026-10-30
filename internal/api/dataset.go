package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/datasetops"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/ddy2006/packfs/internal/shard"
)

type datasetResponse struct {
	UID         string         `json:"uid"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	CurrentPath string         `json:"current_path"`
	SQLitePath  string         `json:"sqlite_path"`
	Metadata    map[string]any `json:"metadata"`
	ShardCount  int            `json:"shard_count"`
	FileCount   int64          `json:"file_count"`
	TotalBytes  int64          `json:"total_bytes"`
}

func newDatasetResponse(info datasetops.Info) datasetResponse {
	ds := info.Dataset
	return datasetResponse{
		UID:         ds.UID,
		Name:        ds.Name,
		Status:      ds.Status,
		CurrentPath: ds.CurrentPath,
		SQLitePath:  info.SQLitePath,
		Metadata:    ds.Metadata,
		ShardCount:  info.ShardCount,
		FileCount:   metaInt64(ds.Metadata, "num_files"),
		TotalBytes:  metaInt64(ds.Metadata, "total_bytes"),
	}
}

type createDatasetRequest struct {
	RootDir        string `json:"root_dir"`
	Name           string `json:"name"`
	TargetRoot     string `json:"target_root"`
	Format         string `json:"format"`
	Compress       string `json:"compress"`
	ShardMaxBytes  int64  `json:"shard_max_bytes"`
	ShardSortRegex string `json:"shard_sort_regex"`
	ShardSortIndex string `json:"shard_sort_index"`
	GenOnly        bool   `json:"gen_only"`
}

func (s *Server) listDatasets(w http.ResponseWriter, r *http.Request) {
	infos, err := datasetops.Scan(r.Context(), s.DBDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeOK(w, []datasetResponse{})
			return
		}
		writeOpError(w, err)
		return
	}
	out := make([]datasetResponse, len(infos))
	for i, info := range infos {
		out[i] = newDatasetResponse(info)
	}
	writeOK(w, out)
}

func (s *Server) createDataset(w http.ResponseWriter, r *http.Request) {
	var req createDatasetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.RootDir == "" {
		writeError(w, http.StatusBadRequest, "root_dir is required")
		return
	}
	result, err := datasetops.Create(r.Context(), datasetops.CreateOptions{
		RootDir:        req.RootDir,
		Name:           req.Name,
		DBDir:          s.DBDir,
		TargetRoot:     req.TargetRoot,
		Format:         req.Format,
		Compress:       req.Compress,
		ShardMaxBytes:  req.ShardMaxBytes,
		ShardSortRegex: req.ShardSortRegex,
		ShardSortIndex: req.ShardSortIndex,
		GenOnly:        req.GenOnly,
	})
	if err != nil {
		writeOpError(w, err)
		return
	}
	info := datasetops.Info{
		Dataset:    result.Dataset,
		SQLitePath: result.SQLitePath,
		ShardCount: result.ShardCount,
	}
	writeOK(w, newDatasetResponse(info))
}

func (s *Server) getDataset(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	info, err := datasetops.LoadInfoByUID(r.Context(), s.DBDir, uid)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "dataset not found")
			return
		}
		writeOpError(w, err)
		return
	}
	writeOK(w, newDatasetResponse(*info))
}

func (s *Server) listDatasetFiles(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	info, err := datasetops.LoadInfoByUID(r.Context(), s.DBDir, uid)
	if err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}
	dsDB, err := db.OpenDatasetSQLite(info.SQLitePath, true)
	if err != nil {
		writeOpError(w, err)
		return
	}
	defer dsDB.Close()

	files, err := dataset.NewDatasetStore(dsDB).ListFiles(r.Context(), info.Dataset.ID)
	if err != nil {
		writeOpError(w, err)
		return
	}
	if files == nil {
		files = []*dataset.File{}
	}
	writeOK(w, files)
}

type datasetShardSegment struct {
	FilePath string `json:"file_path"`
	Offset   int64  `json:"offset"`
	Size     int64  `json:"size"`
	Csize    int64  `json:"csize"`
}

type datasetShardResponse struct {
	ID           int                   `json:"id"`
	Seq          int                   `json:"seq"`
	FilePath     string                `json:"file_path"`
	FileSize     int64                 `json:"file_size"`
	Type         string                `json:"type"`
	Checksum     string                `json:"sha256"`
	Segments     []datasetShardSegment `json:"segments"`
	SegmentCount int                   `json:"segment_count"`
}

func (s *Server) listDatasetShards(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	info, err := datasetops.LoadInfoByUID(r.Context(), s.DBDir, uid)
	if err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}
	out, err := datasetShards(r, info)
	if err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, out)
}

func datasetShards(r *http.Request, info *datasetops.Info) ([]datasetShardResponse, error) {
	dsDB, err := db.OpenDatasetSQLite(info.SQLitePath, true)
	if err != nil {
		return nil, err
	}
	defer dsDB.Close()

	store := shard.NewDatasetStore(dsDB)
	shards, err := store.FindByDataset(r.Context(), info.Dataset.ID)
	if err != nil {
		return nil, err
	}
	out := make([]datasetShardResponse, 0, len(shards))
	for _, sh := range shards {
		infos, err := store.ListUnpackInfo(r.Context(), sh.ID)
		if err != nil {
			return nil, err
		}
		segs := make([]datasetShardSegment, len(infos))
		for i, seg := range infos {
			segs[i] = datasetShardSegment{
				FilePath: seg.FilePath,
				Offset:   seg.Offset,
				Size:     seg.Size,
				Csize:    seg.Csize,
			}
		}
		out = append(out, datasetShardResponse{
			ID:           sh.ID,
			Seq:          sh.Seq,
			FilePath:     sh.FilePath,
			FileSize:     sh.FileSize,
			Type:         sh.Type,
			Checksum:     sh.Checksum,
			Segments:     segs,
			SegmentCount: len(segs),
		})
	}
	return out, nil
}

func (s *Server) markDatasetBuilt(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if err := dataset.ValidateUID(uid); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ds, count, err := datasetops.MarkBuilt(r.Context(), dataset.DBPath(s.DBDir, uid))
	if err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{
		"uid":         ds.UID,
		"status":      "built",
		"shard_count": count,
	})
}

func (s *Server) makeDatasetShards(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if err := dataset.ValidateUID(uid); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ds, count, err := datasetops.BuildShards(r.Context(), dataset.DBPath(s.DBDir, uid))
	if err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{
		"uid":         ds.UID,
		"status":      ds.Status,
		"shard_count": count,
	})
}

type unpackDatasetRequest struct {
	SourceRoot string `json:"source_root"`
	TargetRoot string `json:"target_root"`
}

func (s *Server) unpackDataset(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	var req unpackDatasetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.TargetRoot == "" {
		writeError(w, http.StatusBadRequest, "target_root is required")
		return
	}
	info, err := datasetops.LoadInfoByUID(r.Context(), s.DBDir, uid)
	if err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}
	dsDB, err := db.OpenDatasetSQLite(info.SQLitePath, true)
	if err != nil {
		writeOpError(w, err)
		return
	}
	defer dsDB.Close()

	ds := info.Dataset
	format, _ := ds.Metadata["format"].(string)
	if format == "" {
		format = "bin"
	}
	compress, _ := ds.Metadata["compress"].(string)
	sourceRoot := req.SourceRoot
	if sourceRoot == "" {
		sourceRoot, _ = ds.Metadata["shard_root"].(string)
	}
	if sourceRoot == "" {
		sourceRoot = ds.CurrentPath
	}
	store := shard.NewDatasetStore(dsDB)
	shards, err := store.FindByDataset(r.Context(), ds.ID)
	if err != nil {
		writeOpError(w, err)
		return
	}
	total := 0
	for _, sh := range shards {
		n, err := shard.UnpackShardFile(r.Context(), store, sh.ID,
			filepath.Join(sourceRoot, sh.FilePath), req.TargetRoot, compress, format)
		if err != nil {
			writeOpError(w, err)
			return
		}
		total += n
	}
	writeOK(w, map[string]any{"uid": uid, "files": total, "shards": len(shards)})
}
