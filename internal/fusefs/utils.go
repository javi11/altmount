//go:build fuse

package fusefs

import (
	"os"

	"github.com/hanwen/go-fuse/v2/fuse"
)

// ToFuseMode converts Go's os.FileMode to FUSE compatible mode bits
func ToFuseMode(m os.FileMode) uint32 {
	// Extract standard permission bits (0777)
	mode := uint32(m & os.ModePerm)

	// Add file type bits
	if m.IsDir() {
		mode |= fuse.S_IFDIR
	} else {
		mode |= fuse.S_IFREG
	}

	return mode
}
