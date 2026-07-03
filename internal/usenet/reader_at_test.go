package usenet

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sliceSegmentLoader serves n segments of segSize bytes each.
type sliceSegmentLoader struct {
	n       int
	segSize int64
}

func (l *sliceSegmentLoader) GetSegment(index int) (Segment, []string, bool) {
	if index < 0 || index >= l.n {
		return Segment{}, nil, false
	}
	return Segment{
		Id:    segments.MessageID(index),
		Start: 0,
		End:   l.segSize - 1,
		Size:  l.segSize,
	}, nil, true
}

func newSegmentsReaderAtFixture(t *testing.T, n int, segSize int64) (io.ReaderAt, *fakepool.Client, []byte) {
	t.Helper()
	fp := fakepool.New()
	file := segments.FileBytes(n, int(segSize))
	for i := range n {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Bytes: segments.Payload(i, int(segSize)),
		})
	}
	loader := &sliceSegmentLoader{n: n, segSize: segSize}
	getter := func() (pool.NntpClient, error) { return fp, nil }
	r := NewSegmentsReaderAt(context.Background(), loader, getter, noopMetrics{}, int64(n)*segSize)
	return r, fp, file
}

func TestSegmentsReaderAt_ReadsCorrectBytes(t *testing.T) {
	r, _, file := newSegmentsReaderAtFixture(t, 4, 1024)

	tests := []struct {
		name string
		off  int64
		len  int
	}{
		{name: "start of file", off: 0, len: 16},
		{name: "middle of segment", off: 500, len: 64},
		{name: "spans segment boundary", off: 1000, len: 100},
		{name: "end of file", off: 4096 - 32, len: 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.len)
			n, err := r.ReadAt(buf, tt.off)
			require.NoError(t, err)
			assert.Equal(t, tt.len, n)
			assert.True(t, bytes.Equal(buf, file[tt.off:tt.off+int64(tt.len)]),
				"bytes at offset %d differ", tt.off)
		})
	}

	t.Run("read past EOF", func(t *testing.T) {
		buf := make([]byte, 16)
		_, err := r.ReadAt(buf, 5000)
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("short read at EOF", func(t *testing.T) {
		buf := make([]byte, 64)
		n, err := r.ReadAt(buf, 4096-10)
		assert.ErrorIs(t, err, io.EOF)
		assert.Equal(t, 10, n)
		assert.True(t, bytes.Equal(buf[:10], file[4086:]))
	})
}

// TestSegmentsReaderAt_CacheDedup asserts that two header hops into the same
// segment trigger only one network fetch.
func TestSegmentsReaderAt_CacheDedup(t *testing.T) {
	r, fp, _ := newSegmentsReaderAtFixture(t, 2, 2048)

	buf := make([]byte, 16)
	_, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	_, err = r.ReadAt(buf, 100)
	require.NoError(t, err)
	_, err = r.ReadAt(buf, 1900)
	require.NoError(t, err)

	assert.Equal(t, int64(1), fp.PerMessageCalls(segments.MessageID(0)),
		"segment 0 should be fetched exactly once across reads")
	assert.Equal(t, int64(0), fp.PerMessageCalls(segments.MessageID(1)),
		"segment 1 should never be fetched")
}
