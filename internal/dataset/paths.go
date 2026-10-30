package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDBDir returns the default directory for per-dataset SQLite files.
func DefaultDBDir() string {
	if dir := os.Getenv("PACKFS_DATASET_DB_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "data", "packfs", "datasets")
}

// DBPath returns the SQLite path for a dataset uid.
func DBPath(dbDir, uid string) string {
	return filepath.Join(dbDir, uid+".db")
}

// ValidateUID rejects path separators and traversal in a dataset uid.
func ValidateUID(uid string) error {
	if uid == "" {
		return fmt.Errorf("dataset uid is empty")
	}
	if uid != filepath.Base(uid) || strings.ContainsAny(uid, `/\`) || uid == "." || uid == ".." {
		return fmt.Errorf("invalid dataset uid %q", uid)
	}
	return nil
}
