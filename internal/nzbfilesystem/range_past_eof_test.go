package nzbfilesystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
	"github.com/kipsilabs/altmount/internal/testsupport/segments"
	"github.com/kipsilabs/altmount/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRangeTestMVF builds a plain file whose ctx carries the given HTTP Range
// header (as the WebDAV adapter does) and whose health persistence is real,
// so a corruption verdict would leave a visible record.
func newRangeTestMVF(t *testing.T, rangeHeader string, n, segSize int) (*MetadataVirtualFile, func() bool) {
	t.Helper()
	repo, _, ms := setupStreamHealthEnv(t)
	ctx := context.WithValue(context.Background(), utils.RangeKey, rangeHeader)
	fp := fakepool.New()
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{})
	mvf := newTestMVF(t, ctx, fp, n, segSize, 4)
	cfg := config.DefaultConfig()
	mvf.healthRepository = repo
	mvf.metadataService = ms
	mvf.configGetter = func() *config.Config { return cfg }

	recorded := func() bool {
		fh, err := repo.GetFileHealth(context.Background(), mvf.name)
		require.NoError(t, err)
		return fh != nil
	}
	return mvf, recorded
}

func TestRangeEndPastEOFIsClampedNotCorrupted(t *testing.T) {
	const n, segSize = 8, 64 << 10
	fileSize := int64(n * segSize)
	start := int64(6*segSize + 100)
	mvf, recorded := newRangeTestMVF(t, fmt.Sprintf("bytes=%d-%d", start, fileSize+1_000_000), n, segSize)

	_, err := mvf.Seek(start, io.SeekStart)
	require.NoError(t, err)

	got, err := io.ReadAll(mvf)
	var corrupted *CorruptedFileError
	require.False(t, errors.As(err, &corrupted), "range past EOF must not be a corruption verdict: %v", err)
	require.NoError(t, err)

	want := segments.FileBytes(n, segSize)[start:]
	assert.True(t, bytes.Equal(got, want), "got %d bytes, want %d", len(got), len(want))
	assert.False(t, recorded(), "no health record must be written for a legal range")
}

func TestRangeStartAtOrPastEOFIsEOFNotCorrupted(t *testing.T) {
	const n, segSize = 4, 64 << 10
	fileSize := int64(n * segSize)
	mvf, recorded := newRangeTestMVF(t, fmt.Sprintf("bytes=%d-%d", fileSize, fileSize+10), n, segSize)

	_, err := mvf.Seek(fileSize, io.SeekStart)
	require.NoError(t, err)

	buf := make([]byte, 16)
	n0, err := mvf.Read(buf)
	var corrupted *CorruptedFileError
	require.False(t, errors.As(err, &corrupted), "start at EOF must not be a corruption verdict: %v", err)
	assert.Equal(t, 0, n0)
	assert.ErrorIs(t, err, io.EOF)
	assert.False(t, recorded(), "no health record must be written for a read at EOF")
}
