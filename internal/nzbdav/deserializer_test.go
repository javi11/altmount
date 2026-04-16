package nzbdav

import (
	"os"
	"path/filepath"
	"testing"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
)

func TestBlobDecompression(t *testing.T) {
	blobPath := filepath.Join("testdata", "test_blob.zst")
	data, err := os.ReadFile(blobPath)
	assert.NoError(t, err)

	zr, err := zstd.NewReader(nil)
	assert.NoError(t, err)
	decompressed, err := zr.DecodeAll(data, nil)
	assert.NoError(t, err)

	t.Logf("Decompressed size: %d", len(decompressed))
	// Log the data in a safe way
	if len(decompressed) >= 200 {
		t.Logf("First 200 bytes (hex): %x", decompressed[:200])
	} else {
		t.Logf("Decompressed data (hex): %x", decompressed)
	}
}
