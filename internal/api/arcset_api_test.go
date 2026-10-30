package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ddy2006/packfs/internal/pgtest"
)

func TestArcsetEndpointsWithPostgres(t *testing.T) {
	pool := pgtest.Open(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dbDir := filepath.Join(tmp, "db")
	shardDir := filepath.Join(tmp, "shards")
	arcDir := filepath.Join(tmp, "arc")
	for _, dir := range []string{src, dbDir, shardDir, arcDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		if err := os.WriteFile(filepath.Join(src, "f"+string(rune('0'+i))+".dat"),
			bytes.Repeat([]byte("x"), 1024), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	NewServer(pool, dbDir).RegisterRoutes(mux)

	_, env := doJSON(t, mux, http.MethodPost, "/api/datasets", map[string]any{
		"root_dir":        src,
		"name":            "api-pg-ds",
		"target_root":     shardDir,
		"format":          "bin",
		"shard_max_bytes": 4096,
	})
	var ds struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(env.Data, &ds); err != nil || ds.UID == "" {
		t.Fatalf("create dataset: %+v err=%v", env, err)
	}

	_, env = doJSON(t, mux, http.MethodPost, "/api/arcsets", map[string]any{
		"name":               "api-pg-arc",
		"target_root":        arcDir,
		"ec":                 "4+2",
		"groups":             1,
		"layout":             "raid3",
		"partition_capacity": "1GiB",
	})
	var arc struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(env.Data, &arc); err != nil || arc.ID == 0 {
		t.Fatalf("create arcset: %+v err=%v", env, err)
	}

	cases := []struct {
		path string
		body any
		code int
	}{
		{"/api/arcsets/" + itoa(arc.ID) + "/append", map[string]any{"dataset_uid": ds.UID}, http.StatusOK},
		{"/api/arcsets/" + itoa(arc.ID) + "/build-data", map[string]any{}, http.StatusOK},
		{"/api/arcsets/" + itoa(arc.ID) + "/make-ec", map[string]any{"parallel": 2}, http.StatusOK},
		{"/api/arcsets/" + itoa(arc.ID) + "/validate", map[string]any{"stage": "all"}, http.StatusOK},
		{"/api/arcsets/" + itoa(arc.ID) + "/export", map[string]any{}, http.StatusOK},
	}
	for _, tc := range cases {
		code, env := doJSON(t, mux, http.MethodPost, tc.path, tc.body)
		if code != tc.code || !env.OK {
			t.Fatalf("%s: code=%d body=%+v", tc.path, code, env)
		}
	}

	code, env := doJSON(t, mux, http.MethodGet, "/api/arcsets/"+itoa(arc.ID)+"/plan", nil)
	if code != http.StatusOK || !env.OK {
		t.Fatalf("plan: code=%d body=%+v", code, env)
	}
	var plan struct {
		K       int `json:"k"`
		M       int `json:"m"`
		Stripes []struct {
			Data []any `json:"data"`
			EC   []any `json:"ec"`
		} `json:"stripes"`
	}
	if err := json.Unmarshal(env.Data, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.K != 4 || plan.M != 2 || len(plan.Stripes) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	if len(plan.Stripes[0].Data) != 2 || len(plan.Stripes[0].EC) != 4 {
		t.Fatalf("plan stripe: data=%d ec=%d", len(plan.Stripes[0].Data), len(plan.Stripes[0].EC))
	}

	code, env = doJSON(t, mux, http.MethodGet, "/api/arcsets/"+itoa(arc.ID), nil)
	if code != http.StatusOK || !env.OK {
		t.Fatalf("get arcset: code=%d body=%+v", code, env)
	}
	var detail struct {
		Status   string `json:"status"`
		Health   string `json:"health"`
		Datasets []any  `json:"datasets"`
	}
	if err := json.Unmarshal(env.Data, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Status != "completed" || detail.Health != "ok" || len(detail.Datasets) != 1 {
		t.Fatalf("detail=%+v", detail)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
