package nzbdav

import (
	"os"
	"testing"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
)

func TestBlobDecompression(t *testing.T) {
	blobPath := "/media/docker/data/nzbdav/blobs/4e/49/4e4944aa-0ab7-4001-9bc0-aa44ce5b1d01"
	data, err := os.ReadFile(blobPath)
	assert.NoError(t, err)

	zr, err := zstd.NewReader(nil)
	assert.NoError(t, err)
	decompressed, err := zr.DecodeAll(data, nil)
	assert.NoError(t, err)

	t.Logf("Decompressed size: %d", len(decompressed))
	// Look for typical structures, e.g., strings or message ID patterns
	t.Logf("First 200 bytes (hex): %x", decompressed[:200])
}
