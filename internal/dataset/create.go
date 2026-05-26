package dataset

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateFromDir scans a directory and creates a dataset with file records.
// dirPath: the local directory to scan.
// dsName: the name for the new dataset.
func CreateFromDir(ctx context.Context, store Store, dirPath, dsName string) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return errors.WrapE(err, "resolve absolute path", "dir", dirPath)
	}

	ds := &Dataset{
		Name:         dsName,
		RelativePath: absPath,
	}
	if err := store.Create(ctx, ds); err != nil {
		return err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return errors.WrapE(err, "read directory", "dir", absPath)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			logrus.Warnf("skip file %s: %v", entry.Name(), err)
			continue
		}

		checksum, err := fileChecksum(filepath.Join(absPath, entry.Name()))
		if err != nil {
			logrus.Warnf("skip file %s, checksum failed: %v", entry.Name(), err)
			continue
		}

		f := &File{
			FilePath: entry.Name(),
			FileSize: info.Size(),
			Ctime:    info.ModTime(), // SQLite schema has ctime, use ModTime as best available
			Mtime:    info.ModTime(),
			Checksum: checksum,
			Dataset:  getDatasetID(ds),
		}
		if err := store.AddFileRecord(ctx, f); err != nil {
			return errors.WrapE(err, "add file record", "file", entry.Name())
		}
	}

	logrus.Infof("created dataset %s with %d files from %s", dsName, len(entries), absPath)
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

func getDatasetID(ds *Dataset) int { return ds.ID }
