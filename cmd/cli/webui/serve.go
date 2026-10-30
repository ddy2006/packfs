// Package webui provides the "packfs serve" command for the WebUI.
package webui

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/ddy2006/packfs"
	"github.com/ddy2006/packfs/internal/api"
	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// Command returns the "serve" cobra command.
func Command() *cobra.Command {
	var addr, dbDir string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the WebUI management server",
		Long: `Start an HTTP server with the packfs management WebUI and REST API.

The WebUI is embedded in the binary. Dataset endpoints read per-dataset
SQLite files under --db-dir; arcset endpoints use PACKFS_PG_URL.

Examples:
  packfs serve                          # default :8080
  packfs serve --addr=:9090             # custom port
  packfs serve --addr=127.0.0.1:8080    # localhost only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dbDir == "" {
				dbDir = dataset.DefaultDBDir()
			}
			ctx := context.Background()

			pool, pgErr := db.OpenPostgres(ctx)
			if pgErr != nil {
				logrus.Warnf("PostgreSQL unavailable, arcset API disabled: %v", pgErr)
			} else {
				defer pool.Close()
			}

			mux := http.NewServeMux()
			apiServer := api.NewServer(pool, dbDir)
			apiServer.RegisterRoutes(mux)

			staticFS, err := fs.Sub(packfs.WebUI, "webui")
			if err != nil {
				return errors.WrapE(err, "create static file system")
			}
			fileServer := http.FileServer(http.FS(staticFS))
			mux.Handle("GET /", fileServer)

			fmt.Fprintf(os.Stderr, "packfs WebUI starting on %s\n", addr)
			fmt.Fprintf(os.Stderr, "  → WebUI:   http://%s\n", addr)
			fmt.Fprintf(os.Stderr, "  → API:     http://%s/api/health\n", addr)
			fmt.Fprintf(os.Stderr, "  → dataset: %s\n", dbDir)
			if pool != nil {
				fmt.Fprintf(os.Stderr, "  → postgres: connected\n")
			} else {
				fmt.Fprintf(os.Stderr, "  → postgres: unavailable\n")
			}

			logrus.Infof("packfs serve listening on %s", addr)
			if err := http.ListenAndServe(addr, mux); err != nil {
				return errors.WrapE(err, "listen and serve")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	cmd.Flags().StringVar(&dbDir, "db-dir", "", "directory containing dataset SQLite files")
	return cmd
}
