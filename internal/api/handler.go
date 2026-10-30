// Package api provides HTTP REST handlers for the packfs WebUI.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// Server holds the HTTP handler dependencies.
type Server struct {
	Pool  *pgxpool.Pool
	DBDir string
}

// NewServer creates an API server. Pool may be nil when PostgreSQL is not
// configured; dataset endpoints still work.
func NewServer(pool *pgxpool.Pool, dbDir string) *Server {
	if dbDir == "" {
		dbDir = dataset.DefaultDBDir()
	}
	return &Server{Pool: pool, DBDir: dbDir}
}

// RegisterRoutes registers all API routes on the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Health
	mux.HandleFunc("GET /api/health", s.health)

	// Datasets
	mux.HandleFunc("GET /api/datasets", s.listDatasets)
	mux.HandleFunc("POST /api/datasets", s.createDataset)
	mux.HandleFunc("GET /api/datasets/{uid}", s.getDataset)
	mux.HandleFunc("GET /api/datasets/{uid}/files", s.listDatasetFiles)
	mux.HandleFunc("GET /api/datasets/{uid}/shards", s.listDatasetShards)
	mux.HandleFunc("POST /api/datasets/{uid}/mark-built", s.markDatasetBuilt)
	mux.HandleFunc("POST /api/datasets/{uid}/make-shards", s.makeDatasetShards)
	mux.HandleFunc("POST /api/datasets/{uid}/unpack", s.unpackDataset)

	// Shards
	mux.HandleFunc("GET /api/shards", s.listShards)

	// Arcsets
	mux.HandleFunc("GET /api/arcsets", s.listArcsets)
	mux.HandleFunc("POST /api/arcsets", s.createArcset)
	mux.HandleFunc("GET /api/arcsets/{id}", s.getArcset)
	mux.HandleFunc("POST /api/arcsets/{id}/append", s.appendDataset)
	mux.HandleFunc("POST /api/arcsets/{id}/build-data", s.buildData)
	mux.HandleFunc("POST /api/arcsets/{id}/make-ec", s.makeEC)
	mux.HandleFunc("POST /api/arcsets/{id}/validate", s.validateArcset)
	mux.HandleFunc("POST /api/arcsets/{id}/expand", s.expandArcset)
	mux.HandleFunc("POST /api/arcsets/{id}/recover", s.recoverEC)
	mux.HandleFunc("POST /api/arcsets/{id}/export", s.exportArcset)
	mux.HandleFunc("GET /api/arcsets/{id}/shards", s.listArcsetShards)
	mux.HandleFunc("GET /api/arcsets/{id}/plan", s.arcsetPlan)

	// WebUI compatibility aliases for EC operations.
	mux.HandleFunc("GET /api/ec/plan/{arcsetID}", s.ecPlanAlias)
	mux.HandleFunc("POST /api/ec/encode/{arcsetID}", s.ecEncodeAlias)
	mux.HandleFunc("POST /api/ec/recover/{arcsetID}", s.ecRecoverAlias)

	// FUSE mount records
	mux.HandleFunc("GET /api/fs/mounts", s.listMounts)
	mux.HandleFunc("DELETE /api/fs/mounts", s.deleteMount)

	// Simulation
	mux.HandleFunc("POST /api/simulate", s.simulateData)
}

// ====== Helpers ======

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func writeOpError(w http.ResponseWriter, err error) {
	logrus.Errorf("api error: %+v", err)
	writeError(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) requirePG(w http.ResponseWriter) bool {
	if s.Pool == nil {
		writeError(w, http.StatusServiceUnavailable,
			"PostgreSQL is not configured; set PACKFS_PG_URL")
		return false
	}
	return true
}

func pathID64(r *http.Request, name string) (int64, error) {
	s := r.PathValue(name)
	if s == "" {
		return 0, fmt.Errorf("missing path parameter: %s", name)
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return id, nil
}

func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	return nil
}

// ====== Health ======

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	pgOK := false
	if s.Pool != nil {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		pgOK = s.Pool.Ping(ctx) == nil
	}
	writeOK(w, map[string]any{
		"status":         "ok",
		"postgres":       pgOK,
		"dataset_db_dir": s.DBDir,
	})
}
