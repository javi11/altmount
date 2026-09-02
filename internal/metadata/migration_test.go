package metadata

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfigGetter(t *testing.T) config.ConfigGetter {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "altmount.db")
	cfg := &config.Config{}
	cfg.Database.Path = dbPath
	cfg.Metadata.Migration.DefaultGroup = "alt.binaries.misc"
	return func() *config.Config { return cfg }
}

func seedLegacyRelease(t *testing.T, ms *MetadataService) {
	t.Helper()
	require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", "A.mkv"), &metapb.FileMetadata{
		FileSize: 200, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{
			{Id: "s1@n", SegmentSize: 100, EndOffset: 99},
			{Id: "s2@n", SegmentSize: 100, EndOffset: 99},
		},
	}))
	require.NoError(t, ms.WriteFileMetadata(filepath.Join("movies", "B.mkv"), &metapb.FileMetadata{
		FileSize: 100, SourceNzbPath: "/nzbs/rel.nzb",
		SegmentData: []*metapb.SegmentData{{Id: "s3@n", SegmentSize: 100, EndOffset: 99}},
	}))
}

func TestMigrationWorker_DryRunLeavesLibraryUntouched(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	seedLegacyRelease(t, ms)

	w := NewMigrationWorker(ms, testConfigGetter(t))
	res, err := w.DryRun(context.Background())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.True(t, res.DryRun)
	assert.Equal(t, 1, res.Groups)
	assert.Equal(t, 2, res.FilesMigrated)
	assert.Equal(t, 0, res.FilesFailed)
	assert.Positive(t, res.BytesBefore)
	assert.Positive(t, res.BytesAfter)

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Files, 2)

	status := w.GetStatus()
	assert.False(t, status.IsRunning)
	require.NotNil(t, status.LastDryRun)
	assert.Nil(t, status.LastResult, "a dry run is not a migration")
}

func TestMigrationWorker_StartMigratesEverything(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	seedLegacyRelease(t, ms)

	w := NewMigrationWorker(ms, testConfigGetter(t))
	require.NoError(t, w.Start(context.Background()))

	require.Eventually(t, func() bool {
		return !w.GetStatus().IsRunning && w.GetStatus().LastResult != nil
	}, 10*time.Second, 20*time.Millisecond)

	res := w.GetStatus().LastResult
	require.NotNil(t, res)
	assert.False(t, res.DryRun)
	assert.Equal(t, 2, res.FilesMigrated)
	assert.Equal(t, 0, res.FilesFailed)
	assert.Equal(t, 1, res.SynthesizedGroups)
	assert.Equal(t, 0, res.FaithfulGroups)

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	assert.Empty(t, groups, "nothing legacy is left")

	got, err := ms.ReadFileMetadata(filepath.Join("movies", "A.mkv"))
	require.NoError(t, err)
	require.Len(t, got.SegmentData, 2)
	assert.Equal(t, "s1@n", got.SegmentData[0].Id)
}

func TestMigrationWorker_RejectsConcurrentRuns(t *testing.T) {
	root := t.TempDir()
	ms := NewMetadataService(root)
	w := NewMigrationWorker(ms, testConfigGetter(t))

	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	err := w.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	_, dryErr := w.DryRun(context.Background())
	require.Error(t, dryErr)
}
