package importer

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/nzbfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testNzbContent = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"></nzb>`

func TestNzbCompressionMigration(t *testing.T) {
	dir := t.TempDir()
	nzbPath := filepath.Join(dir, "movie.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNzbContent), 0644))

	sentinelPath := filepath.Join(dir, migrationSentinelFile)

	err := migrateNzbsToGz(context.Background(), dir, sentinelPath, nil)
	require.NoError(t, err)

	// Original .nzb should be deleted
	_, err = os.Stat(nzbPath)
	assert.True(t, os.IsNotExist(err), "original .nzb should be deleted")

	// Compressed version should exist and be readable
	gzPath := filepath.Join(dir, "movie.nzb.gz")
	rc, err := nzbfile.Open(gzPath)
	require.NoError(t, err)
	data, err := io.ReadAll(rc)
	rc.Close()
	require.NoError(t, err)
	assert.Equal(t, testNzbContent, string(data))

	// Sentinel file should exist
	_, err = os.Stat(sentinelPath)
	require.NoError(t, err, "sentinel file should be written")
}

func TestNzbCompressionMigration_SkipsWhenSentinelExists(t *testing.T) {
	dir := t.TempDir()
	sentinelPath := filepath.Join(dir, migrationSentinelFile)
	require.NoError(t, os.WriteFile(sentinelPath, []byte("done"), 0644))

	nzbPath := filepath.Join(dir, "untouched.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNzbContent), 0644))

	err := migrateNzbsToGz(context.Background(), dir, sentinelPath, nil)
	require.NoError(t, err)

	_, err = os.Stat(nzbPath)
	assert.NoError(t, err, ".nzb should be untouched when sentinel exists")
}

func TestNzbCompressionMigration_AlreadyGzFiles(t *testing.T) {
	dir := t.TempDir()
	sentinelPath := filepath.Join(dir, migrationSentinelFile)

	// Place a .nzb.gz file — migration should not try to compress it again
	srcNzb := filepath.Join(dir, "src.nzb")
	gzPath := filepath.Join(dir, "existing.nzb.gz")
	require.NoError(t, os.WriteFile(srcNzb, []byte(testNzbContent), 0644))
	require.NoError(t, nzbfile.Compress(srcNzb, gzPath))
	require.NoError(t, os.Remove(srcNzb))

	err := migrateNzbsToGz(context.Background(), dir, sentinelPath, nil)
	require.NoError(t, err)

	// .nzb.gz should remain untouched (no double-compress)
	_, err = os.Stat(gzPath)
	assert.NoError(t, err, ".nzb.gz should remain after migration")

	// Sentinel should be written
	_, err = os.Stat(sentinelPath)
	require.NoError(t, err)
}

func TestNzbCompressionMigration_UpdaterCalled(t *testing.T) {
	dir := t.TempDir()
	nzbPath := filepath.Join(dir, "show.nzb")
	require.NoError(t, os.WriteFile(nzbPath, []byte(testNzbContent), 0644))

	sentinelPath := filepath.Join(dir, migrationSentinelFile)

	var capturedOld, capturedNew string
	updater := func(_ context.Context, oldPath, newPath string) {
		capturedOld = oldPath
		capturedNew = newPath
	}

	err := migrateNzbsToGz(context.Background(), dir, sentinelPath, updater)
	require.NoError(t, err)

	assert.Equal(t, nzbPath, capturedOld)
	assert.Equal(t, nzbPath+".gz", capturedNew)
}
