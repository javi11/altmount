package iso

import metapb "github.com/javi11/altmount/internal/metadata/proto"

// ISOSource describes an ISO file's location within a RAR or 7zip archive.
type ISOSource struct {
	Filename string
	Segments []*metapb.SegmentData // Usenet segments covering the ISO bytes
	AesKey   []byte                // Nil if unencrypted
	AesIV    []byte
	Size     int64 // Decrypted ISO size
}

// ISOFileContent represents one file found inside the ISO.
type ISOFileContent struct {
	InternalPath string // e.g. "BDMV/STREAM/00001.m2ts"
	Filename     string // Base filename
	Size         int64  // File size in bytes
	NzbdavID     string // Carried from parent archive Content
	// Unencrypted case: Segments sliced to cover exactly this file
	Segments []*metapb.SegmentData
	// Encrypted case: nil Segments + populated NestedSource
	NestedSource *ISONestedSource
}

// ISONestedSource holds everything needed to decrypt and seek into the ISO
// for a single inner file.
type ISONestedSource struct {
	Segments        []*metapb.SegmentData
	AesKey          []byte
	AesIV           []byte
	InnerOffset     int64 // lba * 2048
	InnerLength     int64 // file size
	InnerVolumeSize int64 // ISO total decrypted size
}
