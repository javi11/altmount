package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestSameResolvedSegments(t *testing.T) {
	base := []*metapb.SegmentData{
		{Id: "a@n", SegmentSize: 100, StartOffset: 0, EndOffset: 99},
		{Id: "b@n", SegmentSize: 100, StartOffset: 10, EndOffset: 89},
	}
	clone := func() []*metapb.SegmentData {
		out := make([]*metapb.SegmentData, len(base))
		for i, s := range base {
			out[i] = proto.Clone(s).(*metapb.SegmentData)
		}
		return out
	}

	tests := []struct {
		name    string
		mutate  func([]*metapb.SegmentData) []*metapb.SegmentData
		wantErr string
	}{
		{"identical", func(s []*metapb.SegmentData) []*metapb.SegmentData { return s }, ""},
		{"count differs", func(s []*metapb.SegmentData) []*metapb.SegmentData { return s[:1] }, "segment count"},
		{"id differs", func(s []*metapb.SegmentData) []*metapb.SegmentData {
			s[1].Id = "other@n"
			return s
		}, "segment 1"},
		{"size differs", func(s []*metapb.SegmentData) []*metapb.SegmentData {
			s[0].SegmentSize = 99
			return s
		}, "segment 0"},
		{"start offset differs", func(s []*metapb.SegmentData) []*metapb.SegmentData {
			s[1].StartOffset = 11
			return s
		}, "segment 1"},
		{"end offset differs", func(s []*metapb.SegmentData) []*metapb.SegmentData {
			s[1].EndOffset = 88
			return s
		}, "segment 1"},
		{"order differs", func(s []*metapb.SegmentData) []*metapb.SegmentData {
			s[0], s[1] = s[1], s[0]
			return s
		}, "segment 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sameResolvedSegments("main", base, tt.mutate(clone()))
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestMigrateGroup_RollsBackWhenVerificationFails proves the safety net: if the
// post-write check ever disagrees with the pre-migration segments, the file is
// restored byte-for-byte to its legacy form rather than left converted.
func TestMigrateGroup_RollsBackWhenVerificationFails(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	vpath := filepath.Join("movies", "A.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize: 200, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
	}))

	metaPath := filepath.Join(root, "movies", "A.mkv.meta")
	originalBytes, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.False(t, isV3Meta(originalBytes))

	// Force every verification to fail.
	restore := verifyMigratedFile
	verifyMigratedFile = func(_ *MetadataService, _ string, _ *metapb.FileMetadata) error {
		return assert.AnError
	}
	t.Cleanup(func() { verifyMigratedFile = restore })

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)

	assert.Equal(t, 0, res.FilesMigrated, "a file failing verification must not count as migrated")
	assert.Equal(t, 1, res.FilesFailed)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], "verification")

	// The .meta on disk is byte-identical to before the attempt.
	afterBytes, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	assert.Equal(t, originalBytes, afterBytes, "failed file must be restored byte-for-byte")

	// And it still reads as a working legacy file.
	got, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	require.Len(t, got.SegmentData, 2)
	assert.Equal(t, "s1@n", got.SegmentData[0].Id)
	assert.Empty(t, got.StoreRef)

	// No orphan store is left behind when nothing migrated.
	assert.Empty(t, res.StoreRef)
	entries, readErr := os.ReadDir(storeDir)
	if readErr == nil {
		assert.Empty(t, entries, "orphaned store must be removed")
	}
}

// TestMigrateGroup_VerificationRunsOnEveryFile guards against the check being
// silently skipped: every migrated file must have been verified.
func TestMigrateGroup_VerificationRunsOnEveryFile(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)

	for _, name := range []string{"A.mkv", "B.mkv", "C.mkv"} {
		require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", name), &metapb.FileMetadata{
			FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
			SegmentData: []*metapb.SegmentData{{Id: name + "@n", SegmentSize: 100, EndOffset: 99}},
		}))
	}

	var verified []string
	restore := verifyMigratedFile
	verifyMigratedFile = func(svc *MetadataService, virtualPath string, original *metapb.FileMetadata) error {
		verified = append(verified, virtualPath)
		return restore(svc, virtualPath, original)
	}
	t.Cleanup(func() { verifyMigratedFile = restore })

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)

	assert.Equal(t, 3, res.FilesMigrated)
	assert.Len(t, verified, 3, "every migrated file must be verified against its pre-migration segments")
}
