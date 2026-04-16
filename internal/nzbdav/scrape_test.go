package nzbdav

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScrapeMessageIds(t *testing.T) {
	p := &Parser{}

	// Construct a blob similar to the one we saw
	// Header: 02 10 82 28 ...
	// Length: 27 00 00 00 (39)
	// MsgId: 750aadf553d4f97bad53285a233b53a@ngPost
	msgId := "750aadf553d4f97bad53285a233b53a@ngPost"
	data, _ := os.ReadFile("/tmp/walter_boys.raw")
	copy(data[0:4], []byte{0x02, 0x10, 0x82, 0x28})
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(msgId)))
	copy(data[8:], []byte(msgId))

	results := p.scrapeMessageIds(data)
	t.Logf("Found %d IDs", len(results))
	assert.Greater(t, len(results), 0, "Should have found some IDs")

}

func TestGetBlobPath(t *testing.T) {
	// Create a temp blobs directory
	tempDir, err := os.MkdirTemp("", "blobs")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	blobId := "4E4944AA-0AB7-4001-9BC0-AA44CE5B1D01"
	id := "4e4944aa-0ab7-4001-9bc0-aa44ce5b1d01"
	
	// Create the nested directory structure
	blobDir := filepath.Join(tempDir, id[0:2], id[2:4])
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a lowercase file on disk
	blobPath := filepath.Join(blobDir, id)
	if err := os.WriteFile(blobPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &Parser{
		blobsPath: tempDir,
	}

	// Should find it even with uppercase blobId
	foundPath := p.getBlobPath(blobId)
	assert.Equal(t, blobPath, foundPath)
}

