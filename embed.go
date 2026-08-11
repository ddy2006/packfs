// Package packfs is the root package for the packfs module.
// It embeds the web UI static files for use by the serve command.
package packfs

import "embed"

// WebUI holds all web UI assets (HTML, CSS, JS, astro data) embedded at compile time.
//
//go:embed all:webui
var WebUI embed.FS
