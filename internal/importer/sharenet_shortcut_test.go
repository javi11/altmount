package importer

import (
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

func TestIsSafeVirtualPath(t *testing.T) {
	tests := []struct {
		name string
		vp   string
		safe bool
	}{
		{"normal nested", "Movies/Title/Title.mkv", true},
		{"single segment", "file.mkv", true},
		{"empty", "", false},
		{"absolute", "/etc/passwd", false},
		{"parent escape", "../../etc/cron.d/evil", false},
		{"bare dotdot", "..", false},
		{"dotdot prefix", "../sibling", false},
		{"interior dotdot collapses safely", "a/b/../c", true}, // cleans to "a/c"
		{"interior dotdot escapes", "a/../../b", false},        // cleans to "../b"
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafeVirtualPath(tt.vp); got != tt.safe {
				t.Fatalf("isSafeVirtualPath(%q) = %v; want %v", tt.vp, got, tt.safe)
			}
		})
	}
}

func TestMetaWithinStore(t *testing.T) {
	const size = 10

	tests := []struct {
		name string
		fm   *metapb.FileMetadata
		ok   bool
	}{
		{
			name: "run within bounds",
			fm:   &metapb.FileMetadata{SegmentRuns: []*metapb.SegmentRun{{BaseStoreIndex: 0, Count: 10}}},
			ok:   true,
		},
		{
			name: "run exceeds end",
			fm:   &metapb.FileMetadata{SegmentRuns: []*metapb.SegmentRun{{BaseStoreIndex: 5, Count: 6}}},
			ok:   false,
		},
		{
			name: "run negative base",
			fm:   &metapb.FileMetadata{SegmentRuns: []*metapb.SegmentRun{{BaseStoreIndex: -1, Count: 2}}},
			ok:   false,
		},
		{
			name: "run zero count rejected",
			fm:   &metapb.FileMetadata{SegmentRuns: []*metapb.SegmentRun{{BaseStoreIndex: 0, Count: 0}}},
			ok:   false,
		},
		{
			name: "ref within bounds",
			fm:   &metapb.FileMetadata{SegmentRefs: []*metapb.SegmentRef{{StoreIndex: 9}}},
			ok:   true,
		},
		{
			name: "ref out of range",
			fm:   &metapb.FileMetadata{SegmentRefs: []*metapb.SegmentRef{{StoreIndex: 10}}},
			ok:   false,
		},
		{
			name: "par2 ref out of range",
			fm: &metapb.FileMetadata{
				Par2Files: []*metapb.Par2FileReference{{SegmentRefs: []*metapb.SegmentRef{{StoreIndex: 99}}}},
			},
			ok: false,
		},
		{
			name: "nested source ref out of range",
			fm: &metapb.FileMetadata{
				NestedSources: []*metapb.NestedSegmentSource{{SegmentRefs: []*metapb.SegmentRef{{StoreIndex: 50}}}},
			},
			ok: false,
		},
		{
			name: "empty meta is vacuously within store",
			fm:   &metapb.FileMetadata{},
			ok:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := metaWithinStore(tt.fm, size); got != tt.ok {
				t.Fatalf("metaWithinStore() = %v; want %v", got, tt.ok)
			}
		})
	}
}
