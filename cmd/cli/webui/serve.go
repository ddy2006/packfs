// Package webui provides the "packfs serve" command for the WebUI.
package webui

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/ddy2006/packfs"
	"github.com/ddy2006/packfs/internal/api"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// Command returns the "serve" cobra command.
func Command() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the WebUI management server",
		Long: `Start an HTTP server with the packfs management WebUI and REST API.

The WebUI is embedded in the binary. All /api/* routes call the packfs
internal packages directly against the SQLite database.

Examples:
  packfs serve                          # default :8080
  packfs serve --addr=:9090             # custom port
  packfs serve --addr=127.0.0.1:8080   # localhost only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			mux := http.NewServeMux()

			// API routes
			apiServer := api.NewServer(sqlDB)
			apiServer.RegisterRoutes(mux)

			// Static files (embedded webui, served from "webui/" subdirectory)
			staticFS, err := fs.Sub(packfs.WebUI, "webui")
			if err != nil {
				return errors.WrapE(err, "create static file system")
			}
			fileServer := http.FileServer(http.FS(staticFS))
			mux.Handle("GET /", fileServer)

			fmt.Fprintf(os.Stderr, "packfs WebUI starting on %s\n", addr)
			fmt.Fprintf(os.Stderr, "  → WebUI:  http://%s\n", addr)
			fmt.Fprintf(os.Stderr, "  → API:    http://%s/api/health\n", addr)

			logrus.Infof("packfs serve listening on %s", addr)
			if err := http.ListenAndServe(addr, mux); err != nil {
				return errors.WrapE(err, "listen and serve")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	return cmd
}
