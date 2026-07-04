package usenet

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
)

// NOTE: Tests for ValidateSegmentAvailability and ValidateSegmentAvailabilityDetailed
// were removed during v2→v4 migration because nntppool v4 uses a concrete *Client type
// (not an interface), making it impossible to mock directly. Integration tests with
// a real NNTP server should be used to test validation behavior.
//

func TestSelectSegmentsForValidation(t *testing.T) {
	// Use a deterministic RNG for predictability in middle segments.
	rng := rand.New(rand.NewSource(1))
	previousRandPerm := randPerm
	randPerm = rng.Perm
	t.Cleanup(func() {
		randPerm = previousRandPerm
	})

	// Create 100 dummy segments
	segments := make([]*metapb.SegmentData, 100)
	for i := range 100 {
		segments[i] = &metapb.SegmentData{Id: fmt.Sprintf("seg%d", i)}
	}

	t.Run("100 percent", func(t *testing.T) {
		selected := selectSegmentsForValidation(segments, 100)
		assert.Equal(t, 100, len(selected))
	})

	t.Run("10 percent", func(t *testing.T) {
		selected := selectSegmentsForValidation(segments, 10)
		// 10% of 100 = 10 segments
		assert.Equal(t, 10, len(selected))

		// Should include first 3
		assert.Equal(t, "seg0", selected[0].Id)
		assert.Equal(t, "seg1", selected[1].Id)
		assert.Equal(t, "seg2", selected[2].Id)

		// Should include last 2
		found98 := false
		found99 := false
		for _, s := range selected {
			if s.Id == "seg98" {
				found98 = true
			}
			if s.Id == "seg99" {
				found99 = true
			}
		}
		assert.True(t, found98, "Should include seg98")
		assert.True(t, found99, "Should include seg99")
	})

	t.Run("minimum 5", func(t *testing.T) {
		// 1% of 100 = 1 segment, but minimum is 5
		selected := selectSegmentsForValidation(segments, 1)
		assert.Equal(t, 5, len(selected))
	})

	t.Run("cap 55", func(t *testing.T) {
		// Create 20,000 segments (10% = 2000)
		largeSegments := make([]*metapb.SegmentData, 20000)
		for i := range 20000 {
			largeSegments[i] = &metapb.SegmentData{Id: fmt.Sprintf("seg%d", i)}
		}

		selected := selectSegmentsForValidation(largeSegments, 10)
		assert.Equal(t, 55, len(selected), "Should be capped at 55")
	})
}

// TestValidateSegmentAvailabilityDetailed_MissingSegmentEmitsDebugLog verifies
// the same for the non-fail-fast detailed path.
// NOT parallel: we replace the global slog default.
func TestValidateSegmentAvailabilityDetailed_MissingSegmentEmitsDebugLog(t *testing.T) {
	const segID = "missing-detailed@host"

	var mu sync.Mutex
	type logRecord struct{ msg, segID string }
	var captured []logRecord

	handler := &captureLogHandler{
		onHandle: func(r slog.Record) {
			var sid string
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "segment_id" {
					sid = a.Value.String()
				}
				return true
			})
			mu.Lock()
			captured = append(captured, logRecord{msg: r.Message, segID: sid})
			mu.Unlock()
		},
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	fp := fakepool.New()
	fp.SetBehavior(segID, fakepool.SegmentBehavior{
		Err: nntppool.ErrArticleNotFound,
	})

	mgr := &validationTestPoolManager{client: fp}
	segs := []*metapb.SegmentData{{Id: segID}}

	result, err := ValidateSegmentAvailabilityDetailed(context.Background(), segs, mgr, 1, 100, nil, 5*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, 1, result.MissingCount)

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, r := range captured {
		if r.msg == "missing segment" && r.segID == segID {
			found = true
		}
	}
	assert.True(t, found, "expected 'missing segment' debug log for segment_id=%q, got: %+v", segID, captured)
}

// TestValidateSegmentAvailabilityDetailed_MissingSegmentByteRanges verifies
// that misses are reported with their original index and file-coordinate byte
// range (prefix sum of usable segment lengths), for both full and sampled runs.
func TestValidateSegmentAvailabilityDetailed_MissingSegmentByteRanges(t *testing.T) {
	fp := fakepool.New()
	mgr := &validationTestPoolManager{client: fp}

	// 10 segments of 1000 usable bytes each (with a non-zero archive slice
	// offset on segment 3 to prove usable length, not segment size, drives
	// the prefix sum).
	segs := make([]*metapb.SegmentData, 10)
	for i := range segs {
		segs[i] = &metapb.SegmentData{
			Id:          fmt.Sprintf("seg%d@host", i),
			SegmentSize: 1200,
			StartOffset: 100,
			EndOffset:   1099, // usable = 1000
		}
	}

	fp.SetBehavior("seg0@host", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	fp.SetBehavior("seg7@host", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	result, err := ValidateSegmentAvailabilityDetailed(context.Background(), segs, mgr, 2, 100, nil, 5*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, 2, result.MissingCount)
	assert.Len(t, result.MissingSegments, 2)

	assert.Equal(t, 0, result.MissingSegments[0].Index)
	assert.Equal(t, "seg0@host", result.MissingSegments[0].ID)
	assert.Equal(t, int64(0), result.MissingSegments[0].Start)
	assert.Equal(t, int64(999), result.MissingSegments[0].End)

	assert.Equal(t, 7, result.MissingSegments[1].Index)
	assert.Equal(t, int64(7000), result.MissingSegments[1].Start)
	assert.Equal(t, int64(7999), result.MissingSegments[1].End)

	// MissingIDs stays populated for compatibility.
	assert.ElementsMatch(t, []string{"seg0@host", "seg7@host"}, result.MissingIDs)
}

// TestValidateSegmentAvailabilityDetailed_MissingSegmentsCap verifies the
// 50-entry cap holds while MissingCount reports the true total.
func TestValidateSegmentAvailabilityDetailed_MissingSegmentsCap(t *testing.T) {
	fp := fakepool.New()
	mgr := &validationTestPoolManager{client: fp}

	segs := make([]*metapb.SegmentData, 60)
	for i := range segs {
		id := fmt.Sprintf("seg%d@host", i)
		segs[i] = &metapb.SegmentData{Id: id, StartOffset: 0, EndOffset: 999}
		fp.SetBehavior(id, fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	}

	result, err := ValidateSegmentAvailabilityDetailed(context.Background(), segs, mgr, 4, 100, nil, 5*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, 60, result.MissingCount)
	assert.Len(t, result.MissingIDs, 50)
	assert.Len(t, result.MissingSegments, 50)
}

// validationTestPoolManager is a minimal pool.Manager for validation tests.
// It wraps a fakepool.Client and no-ops everything else.
type validationTestPoolManager struct {
	client pool.NntpClient
}

var _ pool.Manager = (*validationTestPoolManager)(nil)

func (m *validationTestPoolManager) GetPool() (pool.NntpClient, error)        { return m.client, nil }
func (m *validationTestPoolManager) SetProviders(_ []nntppool.Provider) error { return nil }
func (m *validationTestPoolManager) ClearPool() error                         { return nil }
func (m *validationTestPoolManager) HasPool() bool                            { return true }
func (m *validationTestPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m *validationTestPoolManager) ResetMetrics(_ context.Context, _, _ bool) error { return nil }
func (m *validationTestPoolManager) ResetProviderErrors(_ context.Context) error     { return nil }
func (m *validationTestPoolManager) IncArticlesDownloaded()                          {}
func (m *validationTestPoolManager) UpdateDownloadProgress(_ string, _ int64)        {}
func (m *validationTestPoolManager) IncArticlesPosted()                              {}
func (m *validationTestPoolManager) AddProvider(_ nntppool.Provider) error           { return nil }
func (m *validationTestPoolManager) RemoveProvider(_ string) error                   { return nil }
func (m *validationTestPoolManager) ResetProviderQuota(_ context.Context, _ string) error {
	return nil
}
func (m *validationTestPoolManager) SetProviderIDs(_ map[string]string) {}
func (m *validationTestPoolManager) AcquireImportSlot(_ context.Context) (func(), error) {
	return func() {}, nil
}
func (m *validationTestPoolManager) SetAdmissionCaps(_ int, _ int)               {}
func (m *validationTestPoolManager) SetStreamSource(_ pool.StreamActivitySource) {}
func (m *validationTestPoolManager) NotifyStreamChange()                         {}
