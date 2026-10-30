package api

import (
	"net/http"
	"strconv"

	"github.com/ddy2006/packfs/internal/datasetops"
)

func (s *Server) listShards(w http.ResponseWriter, r *http.Request) {
	if uid := r.URL.Query().Get("dataset_uid"); uid != "" {
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
		return
	}
	if idStr := r.URL.Query().Get("arcset_id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid arcset_id")
			return
		}
		if !s.requirePG(w) {
			return
		}
		out, err := s.arcsetShardRows(r, id)
		if err != nil {
			writeOpError(w, err)
			return
		}
		writeOK(w, out)
		return
	}
	writeError(w, http.StatusBadRequest, "dataset_uid or arcset_id query parameter required")
}
