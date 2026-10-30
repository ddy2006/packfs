package api

import (
	"net/http"

	"github.com/ddy2006/packfs/internal/simulate"
)

func (s *Server) simulateData(w http.ResponseWriter, r *http.Request) {
	var cfg simulate.Config
	if err := decodeJSON(r, &cfg); err != nil {
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
		writeOpError(w, err)
		return
	}
	writeOK(w, stats)
}
