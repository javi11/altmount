package archive

import (
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

func TestNewFileMetadataFromContent_PreservesNestedSources(t *testing.T) {
	c := Content{
		Filename: "main_feature.m2ts",
		Size:     100,
		Segments: []*metapb.SegmentData{{Id: "outer@"}},
		NestedSources: []NestedSource{
			{InnerOffset: 0, InnerLength: 40, Segments: []*metapb.SegmentData{{Id: "a@"}}},
			{InnerOffset: 0, InnerLength: 60, Segments: []*metapb.SegmentData{{Id: "b@"}}},
		},
	}

	got := NewFileMetadataFromContent(c, "/path/to.nzb", 1234567890, "nzbdav-id-1")

	if got.FileSize != 100 {
		t.Errorf("FileSize = %d, want 100", got.FileSize)
	}
	if got.SourceNzbPath != "/path/to.nzb" {
		t.Errorf("SourceNzbPath = %q, want %q", got.SourceNzbPath, "/path/to.nzb")
	}
	if got.ReleaseDate != 1234567890 {
		t.Errorf("ReleaseDate = %d, want 1234567890", got.ReleaseDate)
	}
	if got.NzbdavId != "nzbdav-id-1" {
		t.Errorf("NzbdavId = %q, want %q", got.NzbdavId, "nzbdav-id-1")
	}
	if got.Status != metapb.FileStatus_FILE_STATUS_HEALTHY {
		t.Errorf("Status = %v, want FILE_STATUS_HEALTHY", got.Status)
	}
	if len(got.SegmentData) != 1 || got.SegmentData[0].Id != "outer@" {
		t.Errorf("SegmentData not preserved: %+v", got.SegmentData)
	}
	if len(got.NestedSources) != 2 {
		t.Fatalf("NestedSources = %d, want 2", len(got.NestedSources))
	}
	if got.NestedSources[0].InnerLength != 40 || got.NestedSources[1].InnerLength != 60 {
		t.Errorf("NestedSources lengths wrong: %+v", got.NestedSources)
	}
	if got.NestedSources[0].Segments[0].Id != "a@" || got.NestedSources[1].Segments[0].Id != "b@" {
		t.Errorf("NestedSources segment ids wrong: %+v", got.NestedSources)
	}
	// No AES key on Content → no encryption on metadata
	if got.Encryption != metapb.Encryption_NONE {
		t.Errorf("Encryption = %v, want NONE (no AES key on content)", got.Encryption)
	}
}

func TestNewFileMetadataFromContent_SetsAESWhenKeyPresent(t *testing.T) {
	c := Content{
		Filename: "encrypted.bin",
		Size:     50,
		AesKey:   []byte{0x01, 0x02, 0x03},
		AesIV:    []byte{0x10, 0x20, 0x30},
	}

	got := NewFileMetadataFromContent(c, "", 0, "")

	if got.Encryption != metapb.Encryption_AES {
		t.Errorf("Encryption = %v, want AES", got.Encryption)
	}
	if string(got.AesKey) != string(c.AesKey) {
		t.Errorf("AesKey not propagated")
	}
	if string(got.AesIv) != string(c.AesIV) {
		t.Errorf("AesIv not propagated")
	}
}
