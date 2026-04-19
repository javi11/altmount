package importer

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testNzbContent = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"></nzb>`

func TestOpenNzbFile_PlainNzb(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.nzb")
	require.NoError(t, os.WriteFile(path, []byte(testNzbContent), 0644))

	rc, err := openNzbFile(path)
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, testNzbContent, string(data))
}

func TestOpenNzbFile_GzippedNzb(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.nzb.gz")

	f, err := os.Create(path)
	require.NoError(t, err)
	gw := gzip.NewWriter(f)
	_, err = gw.Write([]byte(testNzbContent))
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	require.NoError(t, f.Close())

	rc, err := openNzbFile(path)
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, testNzbContent, string(data))
}

func TestOpenNzbFile_NotFound(t *testing.T) {
	_, err := openNzbFile("/nonexistent/path/test.nzb")
	assert.Error(t, err)
}

func TestCompressNzbToGz(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.nzb")
	require.NoError(t, os.WriteFile(srcPath, []byte(testNzbContent), 0644))

	dstPath := filepath.Join(dir, "dest.nzb.gz")
	require.NoError(t, compressNzbToGz(srcPath, dstPath))

	rc, err := openNzbFile(dstPath)
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, testNzbContent, string(data))

	dstInfo, err := os.Stat(dstPath)
	require.NoError(t, err)
	assert.NotZero(t, dstInfo.Size())
}
