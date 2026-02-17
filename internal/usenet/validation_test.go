package usenet

import (
	"fmt"
	"math/rand"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
)

// NOTE: Tests for ValidateSegmentAvailability and ValidateSegmentAvailabilityDetailed
// were removed during v2→v4 migration because nntppool v4 uses a concrete *Client type
// (not an interface), making it impossible to mock directly. Integration tests with
// a real NNTP server should be used to test validation behavior.

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
