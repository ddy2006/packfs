package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "embed"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// OpenSQLite 打开或创建 SQLite 数据库。
// 数据库路径由环境变量 SQLITE_DB 指定，缺省为 data/packfs.db。
// 若数据库文件不存在，则用嵌入的 schema 自动建表。
func OpenSQLite() (*sql.DB, error) {
	dbPath := os.Getenv("SQLITE_DB")
	if dbPath == "" {
		dbPath = filepath.Join("data", "packfs.db")
	}

	needInit := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		needInit = true
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if needInit {
		if _, err := db.Exec(schemaSQL); err != nil {
			db.Close()
			return nil, err
		}
	}

	return db, nil
}
