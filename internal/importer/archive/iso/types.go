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

// ISOFileContent represents one file found inside the ISO. The file's
// data may be split across multiple on-disc extents (Blu-ray main-feature
// M2TS files routinely use hundreds), so Sources is a slice of inner
// sources in disc order. Concatenating their byte ranges yields the
// complete file content.
type ISOFileContent struct {
	InternalPath string // e.g. "BDMV/STREAM/00001.m2ts"
	Filename     string // Base filename
	Size         int64  // Total file size in bytes (sum of Sources.InnerLength)
	NzbdavID     string // Carried from parent archive Content
	Sources      []ISONestedSource
}

// ISONestedSource is one extent of an inner file. For unencrypted ISOs,
// Segments is pre-sliced to cover exactly this extent and AesKey is nil
// (InnerOffset is 0, InnerLength equals the extent length). For encrypted
// ISOs, AesKey/AesIV are populated, Segments cover the full outer ISO,
// InnerOffset is the byte offset of this extent within the decrypted
// ISO, and InnerVolumeSize is the full decrypted ISO size — the cipher
// chain needs to start at byte 0 so multi-extent encrypted reads use
// the same outer-ISO data with different inner offsets.
type ISONestedSource struct {
	Segments        []*metapb.SegmentData
	AesKey          []byte
	AesIV           []byte
	InnerOffset     int64
	InnerLength     int64
	InnerVolumeSize int64
}

// AnalyzedISO is the full result of inspecting one ISO image. Files mirrors
// what AnalyzeISOContent has always returned (all media files with extension
// filtering applied). MainFeature, when non-nil, is the ordered M2TS list
// that forms the Blu-ray main feature according to BDMV/PLAYLIST/*.mpls —
// this is the slice callers should concatenate to produce a single playable
// virtual file.
type AnalyzedISO struct {
	VolumeLabel   string
	Files         []ISOFileContent
	MainFeature   []ISOFileContent // nil for non-BDMV / unparseable playlists
	DurationTicks int64            // sum of (OUT-IN) of MainFeature at 45 kHz
}
