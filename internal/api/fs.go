package api

import (
	"net/http"
	"time"

	"github.com/ddy2006/packfs/internal/arcset"
)

type mountResponse struct {
	ID         int64     `json:"id"`
	MountPoint string    `json:"mount_point"`
	Type       string    `json:"type"`
	DatasetUID *string   `json:"dataset_uid"`
	ArcsetID   *int64    `json:"arcset_id"`
	CreateTime time.Time `json:"create_time"`
}

func (s *Server) listMounts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	rows, err := arcset.NewPGStore(s.Pool).ListMounts(r.Context())
	if err != nil {
		writeOpError(w, err)
		return
	}
	out := make([]mountResponse, len(rows))
	for i, row := range rows {
		out[i] = mountResponse{
			ID: row.ID, MountPoint: row.MountPoint, Type: row.Type,
			DatasetUID: row.DatasetUID, ArcsetID: row.ArcsetID, CreateTime: row.CreateTime,
		}
	}
	writeOK(w, out)
}

func (s *Server) deleteMount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePG(w) {
		return
	}
	mountPoint := r.URL.Query().Get("mount_point")
	if mountPoint == "" {
		writeError(w, http.StatusBadRequest, "mount_point is required")
		return
	}
	if err := arcset.NewPGStore(s.Pool).DeleteMount(r.Context(), mountPoint); err != nil {
		writeOpError(w, err)
		return
	}
	writeOK(w, map[string]any{"mount_point": mountPoint, "deleted": true})
}
