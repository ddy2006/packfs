package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/builddata"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/ec"
	exportpkg "github.com/ddy2006/packfs/internal/export"
	"github.com/ddy2006/packfs/internal/makeec"
	"github.com/ddy2006/packfs/internal/recoverec"
	"github.com/ddy2006/packfs/internal/validate"
)

type arcsetResponse struct {
	ID             int64          `json:"id"`
	Name           string         `json:"name"`
	Status         string         `json:"status"`
	BuildPhase     string         `json:"build_phase"`
	Health         string         `json:"health"`
	ParityDegraded bool           `json:"parity_degraded"`
	CurrentPath    string         `json:"current_path"`
	Metadata       map[string]any `json:"metadata"`
	DatasetCount   int            `json:"dataset_count"`
	ShardCount     int            `json:"shard_count"`
	DataShards     int            `json:"data_shards"`
	ECShards       int            `json:"ec_shards"`
	PADShards      int            `json:"pad_shards"`
	UsedBytes      int64          `json:"used_bytes"`
	DataCapacity   int64          `json:"data_capacity"`
	RawCapacity    int64          `json:"raw_capacity"`
	LastCheck      string         `json:"last_check,omitempty"`
}

type relationResponse struct {
	DatasetUID  string `json:"dataset_uid"`
	DatasetKey  string `json:"dataset_key"`
	Name        string `json:"name"`
	SQLitePath  string `json:"sqlite_path"`
	AppendOrder int    `json:"append_order"`
}

type arcsetDetailResponse struct {
	arcsetResponse
	Datasets []relationResponse `json:"datasets"`
}

func (s *Server) listArcsets(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	status := r.URL.Query().Get("status")
	store := arcset.NewPGStore(s.Pool)
	arcsets, err := store.List(r.Context(), status)
	if err != nil {
		writeOpError(w, err)
		return
	}
	out := make([]arcsetResponse, 0, len(arcsets))
	for _, a := range arcsets {
		resp, err := s.buildArcsetResponse(r, a)
		if err != nil {
			writeOpError(w, err)
			return
		}
		out = append(out, resp)
	}
	writeOK(w, out)
}

func (s *Server) buildArcsetResponse(r *http.Request, a *arcset.Arcset) (arcsetResponse, error) {
	store := arcset.NewPGStore(s.Pool)
	relations, err := store.ListRelations(r.Context(), int64(a.ID))
	if err != nil {
		return arcsetResponse{}, err
	}
	shards, err := store.ListShards(r.Context(), int64(a.ID))
	if err != nil {
		return arcsetResponse{}, err
	}
	resp := arcsetResponse{
		ID:             int64(a.ID),
		Name:           a.Name,
		Status:         a.Status,
		BuildPhase:     a.BuildPhase,
		Health:         a.Health,
		ParityDegraded: a.ParityDegraded,
		CurrentPath:    a.CurrentPath,
		Metadata:       a.Metadata,
		DatasetCount:   len(relations),
		ShardCount:     len(shards),
		DataCapacity:   metaInt64(a.Metadata, "data_capacity"),
		RawCapacity:    metaInt64(a.Metadata, "raw_capacity"),
	}
	if !a.LastCheck.IsZero() {
		resp.LastCheck = a.LastCheck.Format(time.RFC3339)
	}
	for _, sh := range shards {
		resp.UsedBytes += sh.FileSize
		switch sh.Type {
		case "DATA":
			resp.DataShards++
		case "EC":
			resp.ECShards++
		case "PAD":
			resp.PADShards++
		}
	}
	return resp, nil
}

type createArcsetRequest struct {
	Name              string          `json:"name"`
	TargetRoot        string          `json:"target_root"`
	EC                string          `json:"ec"`
	Groups            int             `json:"groups"`
	Layout            string          `json:"layout"`
	PartitionWidth    int             `json:"partition_width"`
	SeqWidth          int             `json:"seq_width"`
	PartitionCapacity json.RawMessage `json:"partition_capacity"`
	ShardMaxBytes     int64           `json:"shard_max_bytes"`
	Label             string          `json:"label"`
	Comment           string          `json:"comment"`
}

func (s *Server) createArcset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	var req createArcsetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" || req.TargetRoot == "" || req.EC == "" {
		writeError(w, http.StatusBadRequest, "name, target_root and ec are required")
		return
	}
	capacity, err := parseCapacity(req.PartitionCapacity)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid partition_capacity: "+err.Error())
		return
	}
	if req.Groups == 0 {
		req.Groups = 1
	}
	result, err := arcsetops.Create(r.Context(), s.Pool, arcsetops.CreateOptions{
		Name:              req.Name,
		TargetRoot:        req.TargetRoot,
		EC:                req.EC,
		Groups:            req.Groups,
		Layout:            req.Layout,
		PartitionWidth:    req.PartitionWidth,
		SeqWidth:          req.SeqWidth,
		PartitionCapacity: capacity,
		ShardMaxBytes:     req.ShardMaxBytes,
		Label:             req.Label,
		Comment:           req.Comment,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{
		"id":              result.Arcset.ID,
		"name":            result.Arcset.Name,
		"status":          result.Arcset.Status,
		"partition_count": result.PartitionCount,
		"metadata":        result.Arcset.Metadata,
	})
}

func (s *Server) getArcset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	store := arcset.NewPGStore(s.Pool)
	a, err := store.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "arcset not found")
		return
	}
	resp, err := s.buildArcsetResponse(r, a)
	if err != nil {
		writeOpError(w, err)
		return
	}
	relations, err := store.ListRelations(r.Context(), id)
	if err != nil {
		writeOpError(w, err)
		return
	}
	detail := arcsetDetailResponse{arcsetResponse: resp}
	for _, rel := range relations {
		detail.Datasets = append(detail.Datasets, relationResponse{
			DatasetUID:  rel.UID,
			DatasetKey:  rel.DatasetKey,
			Name:        rel.Name,
			SQLitePath:  rel.SQLitePath,
			AppendOrder: rel.AppendOrder,
		})
	}
	if detail.Datasets == nil {
		detail.Datasets = []relationResponse{}
	}
	writeOK(w, detail)
}

type appendDatasetRequest struct {
	DatasetUID string `json:"dataset_uid"`
	DatasetDB  string `json:"dataset_db"`
	DatasetKey string `json:"dataset_key"`
}

func (s *Server) appendDataset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req appendDatasetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	dbPath := req.DatasetDB
	if req.DatasetUID != "" {
		if err := dataset.ValidateUID(req.DatasetUID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		dbPath = dataset.DBPath(s.DBDir, req.DatasetUID)
	}
	if dbPath == "" {
		writeError(w, http.StatusBadRequest, "dataset_uid is required")
		return
	}
	result, err := arcsetops.Append(r.Context(), s.Pool, arcsetops.AppendOptions{
		ArcsetID:   id,
		DatasetDB:  dbPath,
		DatasetKey: req.DatasetKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{
		"arcset_id":    id,
		"dataset_uid":  result.Dataset.UID,
		"dataset_key":  result.DatasetKey,
		"append_order": result.Relation.AppendOrder,
	})
}

type buildDataRequest struct {
	DatasetUID string `json:"dataset_uid"`
}

func (s *Server) buildData(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req buildDataRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	n, err := builddata.BuildData(r.Context(), s.Pool, builddata.Options{
		ArcsetID:   id,
		DatasetUID: req.DatasetUID,
	})
	if err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{"built": n})
}

type makeECRequest struct {
	ChunkSize int64 `json:"chunk_size"`
	Parallel  int   `json:"parallel"`
	Group     *int  `json:"group"`
	Seq       *int  `json:"seq"`
}

func (s *Server) makeEC(w http.ResponseWriter, r *http.Request) {
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.doMakeEC(w, r, id)
}

func (s *Server) doMakeEC(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requirePG(w) {
		return
	}
	var req makeECRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ChunkSize > int64(^uint(0)>>1) {
		writeError(w, http.StatusBadRequest, "chunk_size is too large")
		return
	}
	if req.Parallel <= 0 {
		req.Parallel = 1
	}
	if err := makeec.Run(r.Context(), s.Pool, makeec.Options{
		ArcsetID:  id,
		ChunkSize: int(req.ChunkSize),
		Parallel:  req.Parallel,
		Group:     req.Group,
		Stripe:    req.Seq,
	}); err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{"status": "completed"})
}

type validateRequest struct {
	Stage string `json:"stage"`
}

func (s *Server) validateArcset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req validateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Stage == "" {
		req.Stage = "all"
	}
	if err := validate.Run(r.Context(), s.Pool, validate.Options{ArcsetID: id, Stage: req.Stage}); err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{"status": "ok", "stage": req.Stage})
}

type expandRequest struct {
	AddGroups int `json:"add_groups"`
}

func (s *Server) expandArcset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req expandRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	result, err := arcsetops.Expand(r.Context(), s.Pool, id, req.AddGroups)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{
		"group_count":     result.GroupCount,
		"partition_count": result.PartitionCount,
		"data_capacity":   result.DataCapacity,
		"raw_capacity":    result.RawCapacity,
	})
}

type recoverRequest struct {
	ShardFile string `json:"shard_file"`
	Target    string `json:"target"`
	ChunkSize int64  `json:"chunk_size"`
}

func (s *Server) recoverEC(w http.ResponseWriter, r *http.Request) {
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.doRecoverEC(w, r, id)
}

func (s *Server) doRecoverEC(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requirePG(w) {
		return
	}
	var req recoverRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ShardFile == "" {
		writeError(w, http.StatusBadRequest, "shard_file is required")
		return
	}
	if req.Target == "" {
		req.Target = "all"
	}
	if req.ChunkSize > int64(^uint(0)>>1) {
		writeError(w, http.StatusBadRequest, "chunk_size is too large")
		return
	}
	if err := recoverec.Run(r.Context(), s.Pool, recoverec.Options{
		ArcsetID:  id,
		ShardFile: req.ShardFile,
		Target:    req.Target,
		ChunkSize: int(req.ChunkSize),
	}); err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{"status": "recovered", "shard_file": req.ShardFile})
}

type exportRequest struct {
	TargetRoot string `json:"target_root"`
}

func (s *Server) exportArcset(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req exportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	path, err := exportpkg.Run(r.Context(), s.Pool, exportpkg.Options{
		ArcsetID:   id,
		TargetRoot: req.TargetRoot,
	})
	if err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{"manifest": path})
}

type arcsetShardResponse struct {
	ID           int64          `json:"id"`
	DatasetRef   *int64         `json:"dataset_ref"`
	DatasetKey   *string        `json:"dataset_key"`
	Seq          *int           `json:"seq"`
	GroupNo      *int           `json:"group_no"`
	StripeSeq    *int           `json:"stripe_seq"`
	Pos          *int           `json:"pos"`
	Partition    *int           `json:"partition"`
	FilePath     string         `json:"file_path"`
	OriginalName *string        `json:"original_name"`
	FileSize     int64          `json:"file_size"`
	Type         string         `json:"type"`
	Metadata     map[string]any `json:"metadata"`
	SHA256       *string        `json:"sha256"`
	LastCheck    *time.Time     `json:"last_check"`
}

func (s *Server) listArcsetShards(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.arcsetShardRows(r, id)
	if err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, out)
}

func (s *Server) arcsetShardRows(r *http.Request, id int64) ([]arcsetShardResponse, error) {
	rows, err := arcset.NewPGStore(s.Pool).ListShards(r.Context(), id)
	if err != nil {
		return nil, err
	}
	out := make([]arcsetShardResponse, len(rows))
	for i, sh := range rows {
		out[i] = arcsetShardResponse{
			ID: sh.ID, DatasetRef: sh.DatasetRef, DatasetKey: sh.DatasetKey,
			Seq: sh.Seq, GroupNo: sh.GroupNo, StripeSeq: sh.StripeSeq, Pos: sh.Pos,
			Partition: sh.Partition, FilePath: sh.FilePath, OriginalName: sh.OriginalName,
			FileSize: sh.FileSize, Type: sh.Type, Metadata: sh.Metadata,
			SHA256: sh.SHA256, LastCheck: sh.LastCheck,
		}
	}
	return out, nil
}

type planData struct {
	ID       int64  `json:"id"`
	FilePath string `json:"file_path"`
	Position int    `json:"position"`
	FileSize int64  `json:"file_size"`
}

type planBlock struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Position int    `json:"position"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

type planStripe struct {
	Index int         `json:"index"`
	Group int         `json:"group"`
	Seq   int         `json:"seq"`
	Data  []planData  `json:"data"`
	EC    []planBlock `json:"ec"`
}

type arcsetPlanResponse struct {
	K       int          `json:"k"`
	M       int          `json:"m"`
	Stripes []planStripe `json:"stripes"`
}

func (s *Server) arcsetPlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.doArcsetPlan(w, r, id)
}

func (s *Server) doArcsetPlan(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requirePG(w) {
		return
	}
	store := arcset.NewPGStore(s.Pool)
	a, err := store.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "arcset not found")
		return
	}
	ecCfg, err := ec.ParseConfig(metaString(a.Metadata, "ec"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid EC config: "+err.Error())
		return
	}
	rows, err := store.ListShards(r.Context(), id)
	if err != nil {
		writeOpError(w, err)
		return
	}
	type stripeKey struct{ group, seq int }
	grouped := map[stripeKey][]*arcset.ShardRow{}
	for _, sh := range rows {
		if sh.GroupNo == nil || sh.StripeSeq == nil || sh.Pos == nil {
			continue
		}
		key := stripeKey{*sh.GroupNo, *sh.StripeSeq}
		grouped[key] = append(grouped[key], sh)
	}
	keys := make([]stripeKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].group != keys[j].group {
			return keys[i].group < keys[j].group
		}
		return keys[i].seq < keys[j].seq
	})
	resp := arcsetPlanResponse{K: ecCfg.K, M: ecCfg.M, Stripes: []planStripe{}}
	for idx, key := range keys {
		sort.Slice(grouped[key], func(i, j int) bool { return *grouped[key][i].Pos < *grouped[key][j].Pos })
		ps := planStripe{Index: idx, Group: key.group, Seq: key.seq, Data: []planData{}, EC: []planBlock{}}
		for _, sh := range grouped[key] {
			pos := *sh.Pos
			if pos < ecCfg.K && sh.Type == "DATA" {
				ps.Data = append(ps.Data, planData{
					ID: sh.ID, FilePath: sh.FilePath, Position: pos, FileSize: sh.FileSize,
				})
			} else {
				ps.EC = append(ps.EC, planBlock{
					Name: filepath.Base(sh.FilePath), Type: sh.Type, Position: pos,
					FilePath: sh.FilePath, FileSize: sh.FileSize,
				})
			}
		}
		resp.Stripes = append(resp.Stripes, ps)
	}
	writeOK(w, resp)
}

func (s *Server) ecPlanAlias(w http.ResponseWriter, r *http.Request) {
	id, err := pathID64(r, "arcsetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.doArcsetPlan(w, r, id)
}

func (s *Server) ecEncodeAlias(w http.ResponseWriter, r *http.Request) {
	id, err := pathID64(r, "arcsetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.doMakeEC(w, r, id)
}

func (s *Server) ecRecoverAlias(w http.ResponseWriter, r *http.Request) {
	id, err := pathID64(r, "arcsetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.doRecoverEC(w, r, id)
}

func parseCapacity(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("value is required")
	}
	var s string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
		return arcsetops.ParseBytes(s)
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("value must be positive")
		}
		return n, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if f <= 0 {
			return 0, fmt.Errorf("value must be positive")
		}
		return int64(f), nil
	}
	return 0, fmt.Errorf("invalid number")
}
