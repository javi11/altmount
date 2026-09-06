//go:build linux

package hanwen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStableInode_RootPaths(t *testing.T) {
	assert.Equal(t, uint64(1), stableInode(""))
	assert.Equal(t, uint64(1), stableInode("."))
	assert.Equal(t, uint64(1), stableInode("/"))
}

func TestStableInode_Deterministic(t *testing.T) {
	path := "movies/Fight Club (1999)/Fight.Club.1999.mkv"
	ino1 := stableInode(path)
	ino2 := stableInode(path)
	ino3 := stableInode(path)

	assert.Equal(t, ino1, ino2)
	assert.Equal(t, ino2, ino3)
	assert.Greater(t, ino1, uint64(1), "inode must not be 0 or 1 for regular file")
}

func TestStableInode_DistinctForDifferentPaths(t *testing.T) {
	path1 := "movies/Movie A (2020)/movie.mkv"
	path2 := "movies/Movie B (2020)/movie.mkv"
	path3 := "tv/Show A/season 01/episode 01.mkv"

	ino1 := stableInode(path1)
	ino2 := stableInode(path2)
	ino3 := stableInode(path3)

	assert.NotEqual(t, ino1, ino2)
	assert.NotEqual(t, ino1, ino3)
	assert.NotEqual(t, ino2, ino3)
}

func TestStableInode_ReaddirAndLookupConsistency(t *testing.T) {
	dirPath := "movies/Inception (2010)"
	fileName := "Inception.2010.mkv"
	fullPath := filepath.Join(dirPath, fileName)

	// Inode computed during Readdir:
	readdirIno := stableInode(filepath.Join(dirPath, fileName))

	// Inode computed during Lookup:
	lookupIno := stableInode(fullPath)

	assert.Equal(t, readdirIno, lookupIno, "Readdir and Lookup must produce the exact same inode for identical paths")
	assert.Greater(t, lookupIno, uint64(1))
}
