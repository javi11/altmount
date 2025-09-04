//go:build cli

package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var EmbeddedFS embed.FS

// GetBuildFS returns the embedded build filesystem for CLI builds
func GetBuildFS() (fs.FS, error) {
	return fs.Sub(EmbeddedFS, "dist")
}