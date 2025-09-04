//go:build !cli

package frontend

import (
	"errors"
	"io/fs"
)

// GetBuildFS returns an error for non-CLI builds since we use static files
func GetBuildFS() (fs.FS, error) {
	return nil, errors.New("embedded filesystem not available in non-CLI builds, use static files")
}