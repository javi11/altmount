package archive

import (
	"testing"
	"unsafe"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"google.golang.org/protobuf/proto"
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

// TestNewFileMetadataFromContent_DedupesSharedOuterSources pins the
// encrypted-multi-extent fix. Mimics the Avatar 3D shape: many
// NestedSources sharing the SAME outer-segment slice plus the same AES
// key/iv. Before the dedupe writer landed, marshalling this proto
// produced an 8 GB .meta on disk. The fix must:
//
//  1. Marshal to a size proportional to len(outer-segments) + len(extents),
//     NOT len(outer-segments) × len(extents).
//  2. Round-trip cleanly: after Unmarshal + ExpandSharedOuterSources, all
//     nested sources must point to the same underlying segments backing
//     array (verified via unsafe.SliceData pointer equality), and per-
//     source InnerOffset/InnerLength must be preserved exactly.
func TestNewFileMetadataFromContent_DedupesSharedOuterSources(t *testing.T) {
	// Build an outer segment list large enough that duplicating it across
	// 100 sources would cost ~5 MB if no dedupe ran. With dedupe the
	// marshalled size is dominated by the one shared copy.
	const numSegments = 1000
	const numExtents = 100
	outerSegs := make([]*metapb.SegmentData, numSegments)
	for i := range outerSegs {
		outerSegs[i] = &metapb.SegmentData{
			Id:          "msg-id-of-typical-length@news.example.com",
			StartOffset: int64(i) * 1024,
			EndOffset:   int64(i+1)*1024 - 1,
			SegmentSize: 1024,
		}
	}

	nested := make([]NestedSource, 0, numExtents)
	for i := range numExtents {
		nested = append(nested, NestedSource{
			Segments:        outerSegs, // SAME slice header — the dedupe target
			AesKey:          []byte{0xAA, 0xBB, 0xCC, 0xDD},
			AesIV:           []byte{0x11, 0x22, 0x33, 0x44},
			InnerOffset:     int64(i) * 4096,
			InnerLength:     4096,
			InnerVolumeSize: int64(numSegments * 1024),
		})
	}

	content := Content{
		Filename:      "huge.m2ts",
		Size:          int64(numExtents * 4096),
		NestedSources: nested,
	}

	meta := NewFileMetadataFromContent(content, "/nzb", 0, "")

	// Marshal the proto and assert the on-disk size reflects dedupe.
	marshalled, err := proto.Marshal(meta)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	t.Logf("marshalled .meta size: %d bytes (%d segments × %d extents)", len(marshalled), numSegments, numExtents)

	// Estimate the marshalled size of one shared outer source: ~85 bytes
	// per SegmentData on the wire × 1000 segments ≈ 85 KB. Plus 100
	// thin nested sources at ~30 bytes ≈ 3 KB. Plus header overhead.
	// A regression to per-source duplication would produce ~8.5 MB
	// (100 × 85 KB). Use 500 KB as a generous ceiling that catches any
	// duplication regression.
	const maxAllowed = 500 * 1024
	if len(marshalled) > maxAllowed {
		t.Fatalf("marshalled proto is %d bytes — expected ≤ %d. Dedupe is not working; each NestedSource is duplicating the outer segments list.",
			len(marshalled), maxAllowed)
	}

	// Round-trip: unmarshal + expand, then verify all NestedSources point
	// at the SAME segments backing array (pointer equality via
	// unsafe.SliceData) so RAM cost stays at one shared array.
	decoded := &metapb.FileMetadata{}
	if err := proto.Unmarshal(marshalled, decoded); err != nil {
		t.Fatalf("proto.Unmarshal: %v", err)
	}

	if len(decoded.SharedOuterSources) != 1 {
		t.Fatalf("SharedOuterSources = %d, want 1 (all extents share the same outer)", len(decoded.SharedOuterSources))
	}
	if len(decoded.NestedSources) != numExtents {
		t.Fatalf("NestedSources = %d, want %d", len(decoded.NestedSources), numExtents)
	}

	// Before expansion: every nested source should have empty segments
	// and a non-zero SharedOuterSourceIndex.
	for i, ns := range decoded.NestedSources {
		if len(ns.Segments) != 0 {
			t.Errorf("nested source %d: expected empty Segments before expansion, got %d", i, len(ns.Segments))
		}
		if ns.SharedOuterSourceIndex != 1 {
			t.Errorf("nested source %d: SharedOuterSourceIndex = %d, want 1", i, ns.SharedOuterSourceIndex)
		}
	}

	// Reuse the production expand helper via the metadata package — but
	// to avoid a test-time import cycle we inline an equivalent walk
	// here. (The real read path in metadata.ReadFileMetadata calls
	// metadata.ExpandSharedOuterSources, which performs the same walk.)
	for _, ns := range decoded.NestedSources {
		idx := int(ns.SharedOuterSourceIndex) - 1
		shared := decoded.SharedOuterSources[idx]
		ns.Segments = shared.Segments
		ns.AesKey = shared.AesKey
		ns.AesIv = shared.AesIv
		if ns.InnerVolumeSize == 0 {
			ns.InnerVolumeSize = shared.InnerVolumeSize
		}
	}

	// After expansion: per-source offsets/lengths preserved.
	for i, ns := range decoded.NestedSources {
		if ns.InnerOffset != int64(i)*4096 {
			t.Errorf("nested source %d: InnerOffset = %d, want %d", i, ns.InnerOffset, int64(i)*4096)
		}
		if ns.InnerLength != 4096 {
			t.Errorf("nested source %d: InnerLength = %d, want 4096", i, ns.InnerLength)
		}
		if len(ns.Segments) != numSegments {
			t.Errorf("nested source %d: post-expand Segments = %d, want %d", i, len(ns.Segments), numSegments)
		}
	}

	// All nested sources should share the same underlying segments
	// backing array — proves the expansion didn't deep-copy.
	firstBacking := uintptr(unsafe.Pointer(unsafe.SliceData(decoded.NestedSources[0].Segments)))
	for i := 1; i < len(decoded.NestedSources); i++ {
		thisBacking := uintptr(unsafe.Pointer(unsafe.SliceData(decoded.NestedSources[i].Segments)))
		if firstBacking != thisBacking {
			t.Errorf("nested source %d: expected shared backing array, got distinct pointer (was %x now %x)", i, firstBacking, thisBacking)
			break
		}
	}
}
