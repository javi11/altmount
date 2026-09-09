package nzbfilesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
	"github.com/kipsilabs/altmount/internal/testsupport/segments"
	"github.com/kipsilabs/altmount/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTruncatedTailPool serves n segments where the final article carries
// `short` bytes fewer than its SegmentSize claims — a truncated last part.
// The metadata still advertises the full n*segSize, so the last `short`
// bytes of the file can never be produced.
func newTruncatedTailPool(n, segSize, short int) *fakepool.Client {
	fp := fakepool.New()
	all := segments.FileBytes(n, segSize)
	for i := range n {
		b := all[i*segSize : (i+1)*segSize]
		if i == n-1 {
			b = b[:segSize-short]
		}
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: b})
	}
	return fp
}

func rangeCtx(start, end int64) context.Context {
	return context.WithValue(context.Background(), utils.RangeKey, fmt.Sprintf("bytes=%d-%d", start, end))
}

// A client asking for the unreachable tail (the retry loop seen in the field:
// the same 4760-byte range every 60 ms) must get a definitive corruption
// error, not a bare unexpected EOF that reads as a transient failure.
func TestTruncatedFinalArticleReportsCorruption(t *testing.T) {
	const n, segSize, short = 8, 64 << 10, 4760
	fileSize := int64(n * segSize)
	realEnd := fileSize - short

	for _, tc := range []struct {
		name  string
		start int64
	}{
		{"range starts where the data ends", realEnd},
		{"range starts before the data ends", realEnd - 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fp := newTruncatedTailPool(n, segSize, short)
			mvf := newTestMVF(t, rangeCtx(tc.start, fileSize-1), fp, n, segSize, 4)
			mvf.originalRangeEnd = 0 // parse the Range header on first read

			_, err := mvf.Seek(tc.start, io.SeekStart)
			require.NoError(t, err)

			got, err := readAllOrHang(t, mvf)
			require.Error(t, err)
			var corrupted *CorruptedFileError
			assert.True(t, errors.As(err, &corrupted), "want *CorruptedFileError, got %T: %v", err, err)
			assert.True(t, errors.Is(err, io.ErrUnexpectedEOF), "must still unwrap to io.ErrUnexpectedEOF: %v", err)
			assert.Equal(t, int(realEnd-tc.start), len(got), "everything before the truncation point is delivered")
		})
	}
}

// ReadAt takes the shared-cursor path; it must reach the same verdict.
func TestTruncatedFinalArticleReportsCorruptionOnReadAt(t *testing.T) {
	const n, segSize, short = 8, 64 << 10, 4760
	fileSize := int64(n * segSize)
	realEnd := fileSize - short

	fp := newTruncatedTailPool(n, segSize, short)
	mvf := newTestMVF(t, context.Background(), fp, n, segSize, 4)

	buf := make([]byte, short)
	_, err := mvf.ReadAtContext(context.Background(), buf, realEnd)
	require.Error(t, err)
	var corrupted *CorruptedFileError
	assert.True(t, errors.As(err, &corrupted), "want *CorruptedFileError, got %T: %v", err, err)
	assert.True(t, errors.Is(err, io.ErrUnexpectedEOF), "must still unwrap to io.ErrUnexpectedEOF: %v", err)
}

// With the health system wired, the truncation is recorded and the file
// handed to repair like any other corruption, so the metadata is moved and
// the next client request stops at a 404 instead of re-fetching the article.
func TestTruncatedFinalArticleTriggersRepair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}
	const n, segSize, short = 4, 64 << 10, 4760
	fileSize := int64(n * segSize)
	realEnd := fileSize - short

	repo, db, ms := setupStreamHealthEnv(t)
	ctx := context.Background()
	filePath := "series/truncated.s01e01.mkv"

	fp := newTruncatedTailPool(n, segSize, short)
	mvf := newTestMVF(t, rangeCtx(realEnd, fileSize-1), fp, n, segSize, 4)
	mvf.originalRangeEnd = 0
	mvf.name = filePath

	meta := ms.CreateFileMetadata(fileSize, "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		mvf.meta.SegmentData, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
	require.NoError(t, ms.WriteFileMetadata(filePath, meta))
	_, err := db.Exec(
		`INSERT INTO file_health (file_path, library_path, status, scheduled_check_at) VALUES (?, ?, 'healthy', datetime('now'))`,
		filePath, "/media/library/truncated.s01e01.mkv",
	)
	require.NoError(t, err)

	enabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &enabled
	cfg.MountPath = ""
	mvf.metadataService = ms
	mvf.healthRepository = repo
	mvf.configGetter = func() *config.Config { return cfg }

	_, err = mvf.Seek(realEnd, io.SeekStart)
	require.NoError(t, err)
	_, err = readAllOrHang(t, mvf)
	require.Error(t, err)

	fh, err := repo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusRepairTriggered, fh.Status)

	moved, err := ms.ReadFileMetadata(filePath)
	require.NoError(t, err)
	assert.Nil(t, moved, "metadata must be moved to the corrupted folder")
}
