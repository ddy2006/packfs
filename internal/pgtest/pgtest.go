// Package pgtest creates an isolated PostgreSQL schema for integration tests.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ddy2006/packfs/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open returns a pool whose search_path points at a temporary schema. It skips
// the test when no PostgreSQL URL is configured. The schema is dropped on cleanup.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PACKFS_TEST_PG_URL")
	if dsn == "" {
		dsn = os.Getenv("PACKFS_PG_URL")
	}
	if dsn == "" {
		t.Skip("PACKFS_TEST_PG_URL / PACKFS_PG_URL not set")
	}

	ctx := context.Background()
	adminCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("ping postgres: %v", err)
	}

	schema := fmt.Sprintf("packfs_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		admin.Close()
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
		t.Fatalf("open test pool: %v", err)
	}
	if err := db.InitPostgresSchema(ctx, pool); err != nil {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
		t.Fatalf("init schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
		admin.Close()
	})
	return pool
}
