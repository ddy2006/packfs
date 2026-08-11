// Package api provides HTTP REST handlers for the packfs WebUI.
// It wraps internal/dataset, internal/arcset, internal/shard, and internal/ec.
package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/ec"
	"github.com/ddy2006/packfs/internal/shard"
	"github.com/ddy2006/packfs/internal/simulate"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// Server holds the HTTP handler dependencies.
type Server struct {
	DB *sql.DB
}

// NewServer creates a new API server with the given database connection.
func NewServer(db *sql.DB) *Server {
	return &Server{DB: db}
}

// RegisterRoutes registers all API routes on the given ServeMux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Datasets
	mux.HandleFunc("GET /api/datasets", s.listDatasets)
	mux.HandleFunc("POST /api/datasets", s.createDataset)
	mux.HandleFunc("GET /api/datasets/{id}", s.getDataset)
	mux.HandleFunc("POST /api/datasets/{id}/finalize", s.finalizeDataset)
	mux.HandleFunc("GET /api/datasets/{id}/files", s.listDatasetFiles)
	mux.HandleFunc("DELETE /api/datasets/{id}", s.deleteDataset)

	// Arcsets
	mux.HandleFunc("GET /api/arcsets", s.listArcsets)
	mux.HandleFunc("POST /api/arcsets", s.createArcset)
	mux.HandleFunc("GET /api/arcsets/{id}", s.getArcset)
	mux.HandleFunc("POST /api/arcsets/{id}/append", s.appendDataset)

	// Shards
	mux.HandleFunc("GET /api/shards", s.listShards)
	mux.HandleFunc("POST /api/shards/validate/{id}", s.validateShard)

	// EC
	mux.HandleFunc("GET /api/ec/plan/{arcsetID}", s.planEC)
	mux.HandleFunc("POST /api/ec/encode/{arcsetID}", s.encodeEC)
	mux.HandleFunc("POST /api/ec/recover/{arcsetID}", s.recoverEC)

	// Simulation
	mux.HandleFunc("POST /api/simulate", s.simulateData)

	// Health
	mux.HandleFunc("GET /api/health", s.health)
}

// ====== Response helpers ======

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func writeError(w http.ResponseWriter, status int, msg string, details ...any) {
	body := map[string]any{"ok": false, "error": msg}
	if len(details) > 0 {
		body["details"] = details[0]
	}
	writeJSON(w, status, body)
}

func writeInternalError(w http.ResponseWriter, err error) {
	logrus.Errorf("api error: %+v", err)
	writeError(w, http.StatusInternalServerError, err.Error())
}

func pathID(r *http.Request, name string) (int, error) {
	s := r.PathValue(name)
	if s == "" {
		return 0, fmt.Errorf("missing path parameter: %s", name)
	}
	return strconv.Atoi(s)
}

// ====== Health ======

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]string{"status": "ok"})
}

// ====== Dataset handlers ======

type createDatasetRequest struct {
	RootDir       string `json:"root_dir"`
	Name          string `json:"name"`
	Format        string `json:"format"`
	Compress      string `json:"compress"`
	ShardMaxBytes int64  `json:"shard_max_bytes"`
	GenOnly       bool   `json:"gen_only"`
}

func (s *Server) createDataset(w http.ResponseWriter, r *http.Request) {
	var req createDatasetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.RootDir == "" {
		writeError(w, http.StatusBadRequest, "root_dir is required")
		return
	}
	if req.Format == "" {
		req.Format = "bin"
	}

	ctx := context.Background()
	dsStore := dataset.NewSQLiteStore(s.DB)

	name := req.Name
	if name == "" {
		name = filepath.Base(req.RootDir)
	}

	ds, err := dataset.CreateFromDir(ctx, dsStore, req.RootDir, name)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	// Re-fetch to get metadata set by CreateFromDir
	ds, err = dsStore.FindByID(ctx, ds.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	meta := ds.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["format"] = req.Format
	meta["compress"] = req.Compress
	meta["shard_max_bytes"] = req.ShardMaxBytes
	if err := dsStore.UpdateMetadata(ctx, ds.ID, meta); err != nil {
		writeInternalError(w, err)
		return
	}

	result := map[string]any{
		"id":       ds.ID,
		"name":     ds.Name,
		"gen_only": req.GenOnly,
	}

	if req.GenOnly {
		writeOK(w, result)
		return
	}

	// Generate shards
	files, err := dsStore.ListFiles(ctx, ds.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	shardDefs := dataset.GenerateShardDefs(files, req.ShardMaxBytes, ds.ID)

	for _, sd := range shardDefs {
		outName := fmt.Sprintf("%04d.%s", sd.Seq, req.Format)
		outPath := filepath.Join(ds.CurrentPath, outName)
		cfg := shard.MakeConfig{
			Format:     req.Format,
			Compress:   req.Compress,
			SourceRoot: ds.CurrentPath,
			DatasetID:  ds.ID,
		}
		segs := convertSegmentsToDefs(sd.Segments)
		if _, err := shard.MakeShard(ctx, s.DB, cfg, segs, outPath); err != nil {
			writeInternalError(w, err)
			return
		}
	}

	ds.Metadata["shard_count"] = len(shardDefs)
	if err := dsStore.UpdateMetadata(ctx, ds.ID, ds.Metadata); err != nil {
		writeInternalError(w, err)
		return
	}

	result["shard_count"] = len(shardDefs)
	result["file_count"] = len(files)
	writeOK(w, result)
}

func (s *Server) listDatasets(w http.ResponseWriter, r *http.Request) {
	store := dataset.NewSQLiteStore(s.DB)
	datasets, err := store.Find(context.Background(), dataset.Filter{})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	type dsResponse struct {
		ID          int            `json:"id"`
		Name        string         `json:"name"`
		Status      string         `json:"status"`
		CurrentPath string         `json:"current_path"`
		Metadata    map[string]any `json:"metadata"`
		ShardCount  int            `json:"shard_count"`
	}

	shardStore := shard.NewSQLiteStore(s.DB)
	result := make([]dsResponse, 0, len(datasets))
	for _, ds := range datasets {
		shards, _ := shardStore.FindByDataset(context.Background(), ds.ID)
		// Only count DATA shards
		dataCount := 0
		for _, sh := range shards {
			if sh.Type == "DATA" {
				dataCount++
			}
		}
		result = append(result, dsResponse{
			ID:          ds.ID,
			Name:        ds.Name,
			Status:      ds.Status,
			CurrentPath: ds.CurrentPath,
			Metadata:    ds.Metadata,
			ShardCount:  dataCount,
		})
	}
	writeOK(w, result)
}

func (s *Server) getDataset(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	store := dataset.NewSQLiteStore(s.DB)
	ds, err := store.FindByID(context.Background(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}
	writeOK(w, ds)
}

func (s *Server) listDatasetFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	store := dataset.NewSQLiteStore(s.DB)
	files, err := store.ListFiles(context.Background(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if files == nil {
		files = []*dataset.File{}
	}
	writeOK(w, files)
}

func (s *Server) finalizeDataset(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx := context.Background()
	dsStore := dataset.NewSQLiteStore(s.DB)
	ds, err := dsStore.FindByID(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}

	// Validate all shards
	shardStore := shard.NewSQLiteStore(s.DB)
	shards, err := shardStore.FindByDataset(ctx, ds.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	for _, sh := range shards {
		absPath := filepath.Join(ds.CurrentPath, sh.FilePath)
		if err := validateShardChecksum(absPath, sh.Checksum); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("shard validation failed: %v", err))
			return
		}
	}

	// Copy DB to current_path
	dbPath := os.Getenv("SQLITE_DB")
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, "data", "packfs.db")
	}
	targetDB := filepath.Join(ds.CurrentPath, "packfs.db")
	if err := copyDBFile(dbPath, targetDB); err != nil {
		writeInternalError(w, err)
		return
	}

	// Normalize ID in target DB
	newDB, err := sql.Open("sqlite3", targetDB)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if _, err := newDB.ExecContext(ctx,
		`UPDATE t_dataset SET id = 1, current_path = '.' WHERE id = ?`, ds.ID); err != nil {
		newDB.Close()
		writeInternalError(w, err)
		return
	}
	newDB.Close()

	if err := dsStore.UpdateStatus(ctx, ds.ID, "archived"); err != nil {
		writeInternalError(w, err)
		return
	}

	writeOK(w, map[string]any{
		"id":          ds.ID,
		"status":      "archived",
		"shards":      len(shards),
		"db_copied_to": targetDB,
	})
}

func (s *Server) deleteDataset(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx := context.Background()
	dsStore := dataset.NewSQLiteStore(s.DB)

	// Verify dataset exists
	if _, err := dsStore.FindByID(ctx, id); err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}

	if err := dsStore.Delete(ctx, id); err != nil {
		writeInternalError(w, err)
		return
	}

	writeOK(w, map[string]any{"id": id, "deleted": true})
}

// ====== Arcset handlers ======

type createArcsetRequest struct {
	Name        string `json:"name"`
	TargetRoot  string `json:"target_root"`
	EC          string `json:"ec"`
	TapeMaxBytes int64 `json:"tape_max_bytes"`
	TapeCount   int    `json:"tape_count"`
}

func (s *Server) createArcset(w http.ResponseWriter, r *http.Request) {
	var req createArcsetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.TargetRoot == "" {
		writeError(w, http.StatusBadRequest, "target_root is required")
		return
	}

	metadata := map[string]any{}
	if req.EC != "" {
		ecCfg, err := ec.ParseConfig(req.EC)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid ec: "+err.Error())
			return
		}
		if err := ecCfg.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid ec: "+err.Error())
			return
		}
		metadata["ec"] = req.EC
	}
	if req.TapeMaxBytes > 0 {
		metadata["tape_max_bytes"] = req.TapeMaxBytes
	}
	if req.TapeCount > 0 {
		metadata["tape_count"] = req.TapeCount
	}

	store := arcset.NewSQLiteStore(s.DB)
	a, err := arcset.CreateArcset(context.Background(), store, arcset.CreateArcsetParams{
		Name:        req.Name,
		CurrentPath: req.TargetRoot,
		Metadata:    metadata,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeOK(w, map[string]any{
		"id":           a.ID,
		"name":         a.Name,
		"current_path": a.CurrentPath,
		"metadata":     a.Metadata,
		"status":       a.Status,
	})
}

func (s *Server) listArcsets(w http.ResponseWriter, r *http.Request) {
	store := arcset.NewSQLiteStore(s.DB)
	arcsets, err := store.Find(context.Background(), arcset.Filter{})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	type asResponse struct {
		ID          int            `json:"id"`
		Name        string         `json:"name"`
		Status      string         `json:"status"`
		CurrentPath string         `json:"current_path"`
		Metadata    map[string]any `json:"metadata"`
		DatasetIDs  []int          `json:"dataset_ids"`
		ShardCount  int            `json:"shard_count"`
	}

	shardStore := shard.NewSQLiteStore(s.DB)
	result := make([]asResponse, 0, len(arcsets))
	for _, a := range arcsets {
		refs, _ := store.ListDatasetRefs(context.Background(), a.ID)
		dsIDs := make([]int, len(refs))
		for i, ref := range refs {
			dsIDs[i] = ref.ID
		}
		shards, _ := shardStore.FindByArcset(context.Background(), a.ID)
		result = append(result, asResponse{
			ID:          a.ID,
			Name:        a.Name,
			Status:      a.Status,
			CurrentPath: a.CurrentPath,
			Metadata:    a.Metadata,
			DatasetIDs:  dsIDs,
			ShardCount:  len(shards),
		})
	}
	writeOK(w, result)
}

func (s *Server) getArcset(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	store := arcset.NewSQLiteStore(s.DB)
	a, err := store.FindByID(context.Background(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "arcset not found")
		return
	}
	writeOK(w, a)
}

type appendDatasetRequest struct {
	DatasetID int `json:"dataset_id"`
}

func (s *Server) appendDataset(w http.ResponseWriter, r *http.Request) {
	arcsetID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid arcset id")
		return
	}

	var req appendDatasetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.DatasetID <= 0 {
		writeError(w, http.StatusBadRequest, "dataset_id is required")
		return
	}

	ctx := context.Background()
	arcStore := arcset.NewSQLiteStore(s.DB)
	dsStore := dataset.NewSQLiteStore(s.DB)

	a, err := arcStore.FindByID(ctx, arcsetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "arcset not found")
		return
	}
	if a.Status != "building" {
		writeError(w, http.StatusBadRequest, "arcset is not in building state")
		return
	}

	ds, err := dsStore.FindByID(ctx, req.DatasetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "dataset not found")
		return
	}

	// Compatibility check
	if err := checkArcsetCompat(s.DB, a, ds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Inherit shard_max_bytes
	if err := inheritArcsetShardMax(arcStore, a, ds); err != nil {
		writeInternalError(w, err)
		return
	}

	if err := arcStore.AddDataset(ctx, a.ID, ds.ID); err != nil {
		writeInternalError(w, err)
		return
	}

	// Verify link was created
	refs, _ := arcStore.ListDatasetRefs(ctx, a.ID)
	logrus.Infof("append: arcset=%d dataset=%d, linked datasets after append: %d", a.ID, ds.ID, len(refs))

	writeOK(w, map[string]any{
		"arcset_id":  a.ID,
		"dataset_id": ds.ID,
	})
}

// ====== Shard handlers ======

func (s *Server) listShards(w http.ResponseWriter, r *http.Request) {
	store := shard.NewSQLiteStore(s.DB)

	var result []*shard.Shard
	var err error

	dsIDStr := r.URL.Query().Get("dataset_id")
	asIDStr := r.URL.Query().Get("arcset_id")

	if dsID, _ := strconv.Atoi(dsIDStr); dsID > 0 {
		result, err = store.FindByDataset(context.Background(), dsID)
	} else if asID, _ := strconv.Atoi(asIDStr); asID > 0 {
		result, err = store.FindByArcset(context.Background(), asID)
	} else {
		writeError(w, http.StatusBadRequest, "dataset_id or arcset_id query parameter required")
		return
	}

	if err != nil {
		writeInternalError(w, err)
		return
	}
	if result == nil {
		result = []*shard.Shard{}
	}

	// Build output path map for display
	arcStore := arcset.NewSQLiteStore(s.DB)
	dsStore := dataset.NewSQLiteStore(s.DB)

	type shardWithPath struct {
		*shard.Shard
		OutputDir string `json:"output_dir"`
	}

	out := make([]shardWithPath, len(result))
	for i, sh := range result {
		out[i] = shardWithPath{Shard: sh}
		if sh.Arcset.Valid {
			if a, err := arcStore.FindByID(context.Background(), int(sh.Arcset.Int64)); err == nil {
				out[i].OutputDir = a.CurrentPath
			}
		}
		if out[i].OutputDir == "" && sh.Dataset.Valid {
			if ds, err := dsStore.FindByID(context.Background(), int(sh.Dataset.Int64)); err == nil {
				out[i].OutputDir = ds.CurrentPath
			}
		}
	}

	writeOK(w, out)
}

func (s *Server) validateShard(w http.ResponseWriter, r *http.Request) {
	_, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	store := shard.NewSQLiteStore(s.DB)
	// Shard ID lookup is not directly available; we need to iterate or add a FindByID
	// For now, find all shards and filter by ID
	allShards, err := store.FindByDataset(context.Background(), 0)
	if err != nil {
		// Try arcset lookup
		allShards, _ = store.FindByArcset(context.Background(), 0)
	}
	_ = allShards

	writeError(w, http.StatusNotImplemented, "shard validate by ID: use CLI for now")
}

// ====== EC handlers ======

type planECResponse struct {
	K       int            `json:"k"`
	M       int            `json:"m"`
	Stripes []planStripe   `json:"stripes"`
}

type planStripe struct {
	Index int           `json:"index"`
	Data  []planShard   `json:"data"`
	EC    []planECBlock `json:"ec"`
}

type planShard struct {
	ID       int    `json:"id"`
	FilePath string `json:"file_path"`
	Position int    `json:"position"`
}

type planECBlock struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Position int    `json:"position"`
}

func (s *Server) planEC(w http.ResponseWriter, r *http.Request) {
	arcsetID, err := pathID(r, "arcsetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid arcsetID")
		return
	}

	ctx := context.Background()
	arcStore := arcset.NewSQLiteStore(s.DB)
	dsStore := dataset.NewSQLiteStore(s.DB)
	shardStore := shard.NewSQLiteStore(s.DB)

	a, err := arcStore.FindByID(ctx, arcsetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "arcset not found")
		return
	}

	ecStr, ok := a.Metadata["ec"].(string)
	if !ok || ecStr == "" {
		writeError(w, http.StatusBadRequest, "arcset has no EC config")
		return
	}
	ecCfg, err := ec.ParseConfig(ecStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid EC config: "+err.Error())
		return
	}

	// Collect data shards
	refs, err := arcStore.ListDatasetRefs(ctx, a.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	type dataShardInfo struct {
		ID       int
		FilePath string
	}
	var dataShards []dataShardInfo
	for _, ref := range refs {
		ds, err := dsStore.FindByID(ctx, ref.ID)
		if err != nil {
			continue
		}
		shards, err := shardStore.FindByDataset(ctx, ds.ID)
		if err != nil {
			continue
		}
		for _, sh := range shards {
			if sh.Type != "DATA" {
				continue
			}
			dataShards = append(dataShards, dataShardInfo{
				ID:       sh.ID,
				FilePath: sh.FilePath,
			})
		}
	}

	k := ecCfg.K
	m := ecCfg.M

	var stripes []planStripe
	for i := 0; i < len(dataShards); i += k {
		end := i + k
		if end > len(dataShards) {
			end = len(dataShards)
		}
		chunk := dataShards[i:end]
		si := len(stripes)

		var ps planStripe
		ps.Index = si
		for j, ds := range chunk {
			ps.Data = append(ps.Data, planShard{
				ID:       ds.ID,
				FilePath: ds.FilePath,
				Position: j,
			})
		}
		// EC blocks
		for j := 0; j < m; j++ {
			ps.EC = append(ps.EC, planECBlock{
				Name:     fmt.Sprintf("E%d-%d.ec", si, k+j),
				Type:     "EC",
				Position: k + j,
			})
		}
		// PAD if needed
		padCount := k - len(chunk)
		for j := 0; j < padCount; j++ {
			ps.EC = append(ps.EC, planECBlock{
				Name:     fmt.Sprintf("PAD-%d-%d.pad", si, len(chunk)+j),
				Type:     "PAD",
				Position: len(chunk) + j,
			})
		}
		stripes = append(stripes, ps)
	}

	writeOK(w, planECResponse{K: k, M: m, Stripes: stripes})
}

func (s *Server) encodeEC(w http.ResponseWriter, r *http.Request) {
	arcsetID, err := pathID(r, "arcsetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid arcsetID")
		return
	}

	// Verify arcset has linked datasets before attempting EC
	arcStore := arcset.NewSQLiteStore(s.DB)
	refs, _ := arcStore.ListDatasetRefs(context.Background(), arcsetID)
	logrus.Infof("encodeEC: arcset=%d, linked datasets=%d", arcsetID, len(refs))
	if len(refs) == 0 {
		writeError(w, http.StatusBadRequest, "arcset 尚未关联任何 Dataset，请先在 Arcset 页签执行 Append")
		return
	}

	if err := shard.MakeECShard(context.Background(), s.DB, arcsetID); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, map[string]string{"status": "encoded"})
}

type recoverRequest struct {
	ShardFile string `json:"shard_file"`
}

func (s *Server) recoverEC(w http.ResponseWriter, r *http.Request) {
	arcsetID, err := pathID(r, "arcsetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid arcsetID")
		return
	}

	var req recoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ShardFile == "" {
		writeError(w, http.StatusBadRequest, "shard_file is required")
		return
	}

	if err := recoverShard(s.DB, arcsetID, req.ShardFile); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, map[string]string{
		"status":     "recovered",
		"shard_file": req.ShardFile,
	})
}

// ====== Simulation handler ======

func (s *Server) simulateData(w http.ResponseWriter, r *http.Request) {
	var cfg simulate.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if cfg.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if cfg.OutputRoot == "" {
		cfg.OutputRoot = "./data/dat"
	}
	if cfg.FileBytes <= 0 {
		cfg.FileBytes = 1024
	}

	stats, err := simulate.Generate(cfg)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeOK(w, stats)
}

// ====== Helpers ======

func convertSegmentsToDefs(descs []dataset.SegmentDesc) []shard.SegmentDef {
	defs := make([]shard.SegmentDef, len(descs))
	for i, d := range descs {
		defs[i] = shard.SegmentDef{
			Path:   d.FilePath,
			Offset: d.FileOffset,
			Size:   d.SegmentSize,
			FileID: d.FileID,
		}
	}
	return defs
}

func validateShardChecksum(absPath, expected string) error {
	f, err := os.Open(absPath)
	if err != nil {
		return errors.WrapE(err, "open shard", "path", absPath)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.WrapE(err, "read shard", "path", absPath)
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected[:16], actual[:16])
	}
	return nil
}

func copyDBFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Sync()
}

func checkArcsetCompat(db *sql.DB, a *arcset.Arcset, ds *dataset.Dataset) error {
	arcStore := arcset.NewSQLiteStore(db)
	dsStore := dataset.NewSQLiteStore(db)

	refs, err := arcStore.ListDatasetRefs(context.Background(), a.ID)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}

	ref, err := dsStore.FindByID(context.Background(), refs[0].ID)
	if err != nil {
		return err
	}
	if refFmt, _ := ref.Metadata["format"].(string); refFmt != "" {
		if dsFmt, _ := ds.Metadata["format"].(string); dsFmt != "" && dsFmt != refFmt {
			return fmt.Errorf("format mismatch: arcset has %s, dataset has %s", refFmt, dsFmt)
		}
	}
	if refComp, _ := ref.Metadata["compress"].(string); refComp != "" {
		if dsComp, _ := ds.Metadata["compress"].(string); dsComp != "" && dsComp != refComp {
			return fmt.Errorf("compress mismatch: arcset has %s, dataset has %s", refComp, dsComp)
		}
	}
	return nil
}

func inheritArcsetShardMax(arcStore arcset.Store, a *arcset.Arcset, ds *dataset.Dataset) error {
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}
	if _, ok := a.Metadata["shard_max_bytes"]; !ok {
		if smb, ok := ds.Metadata["shard_max_bytes"]; ok {
			a.Metadata["shard_max_bytes"] = smb
			return arcStore.Update(context.Background(), a.Name, arcset.Update{Metadata: a.Metadata})
		}
	}
	return nil
}

// recoverShard mirrors the CLI recover logic (cmd/cli/shard/recover.go:doRecoverShard).
func recoverShard(db *sql.DB, arcsetID int, lostPath string) error {
	ctx := context.Background()
	arcStore := arcset.NewSQLiteStore(db)
	shardStore := shard.NewSQLiteStore(db)

	a, err := arcStore.FindByID(ctx, arcsetID)
	if err != nil {
		return errors.WrapE(err, "find arcset")
	}
	ecStr, ok := a.Metadata["ec"].(string)
	if !ok || ecStr == "" {
		return fmt.Errorf("arcset has no EC config")
	}
	ecCfg, err := ec.ParseConfig(ecStr)
	if err != nil {
		return errors.WrapE(err, "parse EC config")
	}

	lostShard, err := shardStore.FindByArcsetAndFilePath(ctx, arcsetID, lostPath)
	if err != nil {
		return errors.WrapE(err, "find lost shard")
	}
	stripe := getMetaInt(lostShard.Metadata, "stripe")
	position := getMetaInt(lostShard.Metadata, "position")
	if stripe <= 0 || position <= 0 {
		return fmt.Errorf("lost shard has no stripe/position metadata: %s", lostPath)
	}
	paddedSize := getMetaInt64(lostShard.Metadata, "padded_size")

	allShards, err := shardStore.FindByArcset(ctx, arcsetID)
	if err != nil {
		return errors.WrapE(err, "find arcset shards")
	}

	stripeFiles := make([]ec.StripeFile, ecCfg.Total())
	var originalSizes []int64
	for _, sh := range allShards {
		s := getMetaInt(sh.Metadata, "stripe")
		p := getMetaInt(sh.Metadata, "position")
		if s != stripe || p <= 0 || p > ecCfg.Total() {
			continue
		}
		idx := p - 1
		stripeFiles[idx] = ec.StripeFile{
			Type:     string(sh.Type[0]),
			NewPath:  filepath.Join(a.CurrentPath, sh.FilePath),
			Stripe:   stripe,
			Position: p,
		}
		if sh.Type == "DATA" {
			if originalSizes == nil {
				originalSizes = make([]int64, ecCfg.K)
			}
			os := getMetaInt64(sh.Metadata, "original_size")
			if idx < ecCfg.K {
				originalSizes[idx] = os
			}
		}
	}

	lostIdx := position - 1
	stripeFiles[lostIdx].Type = string(lostShard.Type[0])

	logrus.Infof("recovering stripe %d position %d (file: %s)", stripe, position, lostPath)

	if err := ec.ReconstructStripe(stripeFiles, ecCfg, originalSizes, paddedSize); err != nil {
		return errors.WrapE(err, "reconstruct stripe")
	}

	// Verify
	ok2, err := ec.VerifyStripe(stripeFiles, ecCfg)
	if err != nil {
		return errors.WrapE(err, "verify stripe after recovery")
	}
	if !ok2 {
		return fmt.Errorf("stripe verification failed after recovery")
	}

	logrus.Infof("recovered %s successfully", lostPath)
	return nil
}

func getMetaInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func getMetaInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
