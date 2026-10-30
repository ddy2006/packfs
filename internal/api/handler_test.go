package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type apiEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path string, body any) (int, apiEnvelope) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var env apiEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, env
}

func TestDatasetEndpointsWithoutPostgres(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dbDir := filepath.Join(tmp, "db")
	shardDir := filepath.Join(tmp, "shards")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.dat", "b.dat"} {
		if err := os.WriteFile(filepath.Join(src, name), bytes.Repeat([]byte("x"), 1024), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	NewServer(nil, dbDir).RegisterRoutes(mux)

	code, env := doJSON(t, mux, http.MethodPost, "/api/datasets", map[string]any{
		"root_dir":        src,
		"name":            "test-ds",
		"target_root":     shardDir,
		"format":          "bin",
		"shard_max_bytes": 4096,
	})
	if code != http.StatusOK || !env.OK {
		t.Fatalf("create dataset: code=%d body=%+v", code, env)
	}
	var created struct {
		UID        string `json:"uid"`
		Status     string `json:"status"`
		ShardCount int    `json:"shard_count"`
		SQLitePath string `json:"sqlite_path"`
	}
	if err := json.Unmarshal(env.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.UID == "" || created.Status != "built" || created.ShardCount == 0 {
		t.Fatalf("unexpected dataset: %+v", created)
	}
	if _, err := os.Stat(created.SQLitePath); err != nil {
		t.Fatalf("dataset sqlite missing: %v", err)
	}

	code, env = doJSON(t, mux, http.MethodGet, "/api/datasets", nil)
	if code != http.StatusOK || !env.OK {
		t.Fatalf("list datasets: code=%d body=%+v", code, env)
	}
	var list []map[string]any
	if err := json.Unmarshal(env.Data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["uid"] != created.UID {
		t.Fatalf("unexpected dataset list: %+v", list)
	}

	code, env = doJSON(t, mux, http.MethodGet, "/api/datasets/"+created.UID+"/shards", nil)
	if code != http.StatusOK || !env.OK {
		t.Fatalf("list shards: code=%d body=%+v", code, env)
	}
	var shards []map[string]any
	if err := json.Unmarshal(env.Data, &shards); err != nil {
		t.Fatal(err)
	}
	if len(shards) != created.ShardCount {
		t.Fatalf("shards=%d, want %d", len(shards), created.ShardCount)
	}

	code, env = doJSON(t, mux, http.MethodGet, "/api/arcsets", nil)
	if code != http.StatusServiceUnavailable || env.OK {
		t.Fatalf("arcsets without PG: code=%d body=%+v", code, env)
	}
}
