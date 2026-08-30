package usenet

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
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

	t.Run("large files honor the configured percentage", func(t *testing.T) {
		// Regression for #812: a hard 55-sample cap made every sub-100%
		// setting sample the same handful of segments on large releases.
		largeSegments := make([]*metapb.SegmentData, 20000)
		for i := range 20000 {
			largeSegments[i] = &metapb.SegmentData{Id: fmt.Sprintf("seg%d", i)}
		}

		assert.Equal(t, 2000, len(selectSegmentsForValidation(largeSegments, 10)))
		assert.Equal(t, 10000, len(selectSegmentsForValidation(largeSegments, 50)))
		// Higher percentages must sample strictly more than lower ones.
		assert.Greater(t,
			len(selectSegmentsForValidation(largeSegments, 25)),
			len(selectSegmentsForValidation(largeSegments, 10)),
		)
	})
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
func (m *validationTestPoolManager) SetAdmissionCap(_ int) {}
func (m *validationTestPoolManager) AcquireImportConnection(_ context.Context) (func(), error) {
	return func() {}, nil
}
func (m *validationTestPoolManager) SetImportConnCapacity(_ int)                 {}
func (m *validationTestPoolManager) ImportConnCapacity() int                     { return 0 }
func (m *validationTestPoolManager) SetStreamSource(_ pool.StreamActivitySource) {}
func (m *validationTestPoolManager) NotifyStreamChange()                         {}
