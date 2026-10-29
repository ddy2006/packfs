package db

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed postgres_schema.sql
var postgresSchemaSQL string

// PostgresDSN returns PACKFS_PG_URL or an error when it is unset.
func PostgresDSN() (string, error) {
	dsn := os.Getenv("PACKFS_PG_URL")
	if dsn == "" {
		return "", fmt.Errorf("PACKFS_PG_URL is not set")
	}
	return dsn, nil
}

// OpenPostgres opens a pgx pool from PACKFS_PG_URL.
func OpenPostgres(ctx context.Context) (*pgxpool.Pool, error) {
	dsn, err := PostgresDSN()
	if err != nil {
		return nil, err
	}
	return OpenPostgresDSN(ctx, dsn)
}

// OpenPostgresDSN opens a pgx pool from an explicit DSN.
func OpenPostgresDSN(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// PostgresSchemaSQL returns the embedded global schema.
func PostgresSchemaSQL() string { return postgresSchemaSQL }

// InitPostgresSchema applies the global schema. It is intended for tests and
// development databases; production deployments use build/postgres/arcset_schema.sql.
func InitPostgresSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, postgresSchemaSQL); err != nil {
		return fmt.Errorf("init postgres schema: %w", err)
	}
	return nil
}
