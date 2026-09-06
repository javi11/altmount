package health

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agedInfo restates a real Lstat result with a different mtime. Backdating a
// symlink on disk needs lutimes, which the standard library does not expose
// (os.Chtimes follows the link), and the guard only ever reads Mode and
// ModTime.
type agedInfo struct {
	fs.FileInfo
	mtime time.Time
}

func (a agedInfo) ModTime() time.Time { return a.mtime }

// Orphan cleanup protects symlinks that AltMount imported, so an ARR still
// mid-import does not lose the file underneath it. That protection used to rest
// solely on an import_history row, but deleting a queue item now drops its
// history copy (issue #586) — so a user tidying the completed queue could strip
// the guard off a symlink whose import was still in flight. The symlink's own
// age is an anchor no database cleanup can take away.
func TestSymlinkWithinImportGuard(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	link := filepath.Join(dir, "movie.mkv")
	require.NoError(t, os.Symlink(filepath.Join(dir, "target.mkv"), link))
	linkInfo, err := os.Lstat(link)
	require.NoError(t, err)

	plainFile := filepath.Join(dir, "plain.mkv")
	require.NoError(t, os.WriteFile(plainFile, []byte("x"), 0o644))
	fileInfo, err := os.Lstat(plainFile)
	require.NoError(t, err)

	assert.True(t, symlinkWithinImportGuard(linkInfo, now),
		"a symlink created moments ago may still have an ARR import in flight")

	stale := agedInfo{FileInfo: linkInfo, mtime: now.Add(-recentSymlinkImportGuard - time.Hour)}
	assert.False(t, symlinkWithinImportGuard(stale, now),
		"a symlink older than the guard window is a normal cleanup candidate")

	// Right on the boundary the link is already outside the window.
	edge := agedInfo{FileInfo: linkInfo, mtime: now.Add(-recentSymlinkImportGuard)}
	assert.False(t, symlinkWithinImportGuard(edge, now))

	// A future mtime (clock skew, a restored backup) must not read as ancient.
	skewed := agedInfo{FileInfo: linkInfo, mtime: now.Add(2 * time.Hour)}
	assert.True(t, symlinkWithinImportGuard(skewed, now),
		"clock skew must fail safe towards keeping the file")

	assert.False(t, symlinkWithinImportGuard(fileInfo, now),
		"the guard covers symlinks only; regular files have their own safety checks")
	assert.False(t, symlinkWithinImportGuard(nil, now))
}
