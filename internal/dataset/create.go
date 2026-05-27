package dataset

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateFromDir recursively scans a directory and creates a dataset with file records.
func CreateFromDir(ctx context.Context, store Store, dirPath, dsName string) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return errors.WrapE(err, "resolve absolute path", "dir", dirPath)
	}

	ds := &Dataset{
		Name:        dsName,
		CurrentPath: absPath,
	}
	if err := store.Create(ctx, ds); err != nil {
		return err
	}

	var fileCount int
	err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			logrus.Warnf("skip %s: %v", path, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			logrus.Warnf("skip %s: %v", path, err)
			return nil
		}

		relPath, err := filepath.Rel(absPath, path)
		if err != nil {
			logrus.Warnf("skip %s: %v", path, err)
			return nil
		}

		checksum, err := fileChecksum(path)
		if err != nil {
			logrus.Warnf("skip %s: %v", path, err)
			return nil
		}

		f := &File{
			FilePath: relPath,
			FileSize: info.Size(),
			Metadata: map[string]any{
				"ctime": info.ModTime().UTC().Format(time.RFC3339),
				"mtime": info.ModTime().UTC().Format(time.RFC3339),
			},
			Checksum: checksum,
			Dataset:  ds.ID,
		}
		if err := store.AddFileRecord(ctx, f); err != nil {
			return errors.WrapE(err, "add file record", "file", relPath)
		}
		fileCount++
		return nil
	})
	if err != nil {
		return err
	}

	logrus.Infof("created dataset %s with %d files from %s", dsName, fileCount, absPath)
	return nil
}

func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
