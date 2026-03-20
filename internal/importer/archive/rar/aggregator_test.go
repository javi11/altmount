package rar

import (
	"context"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/require"
)

func TestValidateSegmentIntegrity(t *testing.T) {
	ctx := context.Background()

	t.Run("Healthy non-nested file", func(t *testing.T) {
		content := Content{
			Size:       1000,
			PackedSize: 800,
			Segments: []*metapb.SegmentData{
				{StartOffset: 0, EndOffset: 399},
				{StartOffset: 400, EndOffset: 799},
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.NoError(t, err)
	})

	t.Run("Corrupted non-nested file (missing segments)", func(t *testing.T) {
		content := Content{
			Size:       1000,
			PackedSize: 800,
			Segments: []*metapb.SegmentData{
				{StartOffset: 0, EndOffset: 399},
				// Missing the last 400 bytes
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.Error(t, err)
		require.Contains(t, err.Error(), "corrupted file: missing 400 bytes")
	})

	t.Run("Minor shortfall within 1% threshold", func(t *testing.T) {
		// 1000 bytes expected, 995 provided (0.5% shortfall)
		content := Content{
			Size:       1000,
			PackedSize: 1000,
			Segments: []*metapb.SegmentData{
				{StartOffset: 0, EndOffset: 994},
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.NoError(t, err)
	})

	t.Run("Shortfall exactly 1%", func(t *testing.T) {
		// 1000 bytes expected, 990 provided (1% shortfall)
		content := Content{
			Size:       1000,
			PackedSize: 1000,
			Segments: []*metapb.SegmentData{
				{StartOffset: 0, EndOffset: 989},
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.Error(t, err)
		require.Contains(t, err.Error(), "1% of total size")
	})

	t.Run("Healthy nested sources", func(t *testing.T) {
		content := Content{
			Size: 1000,
			NestedSources: []NestedSource{
				{
					InnerLength: 500,
					Segments: []*metapb.SegmentData{
						{StartOffset: 0, EndOffset: 499},
					},
				},
				{
					InnerLength: 500,
					Segments: []*metapb.SegmentData{
						{StartOffset: 0, EndOffset: 499},
					},
				},
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.NoError(t, err)
	})

	t.Run("Corrupted nested source", func(t *testing.T) {
		content := Content{
			Size: 1000,
			NestedSources: []NestedSource{
				{
					InnerLength: 500,
					Segments: []*metapb.SegmentData{
						{StartOffset: 0, EndOffset: 499},
					},
				},
				{
					InnerLength: 500,
					Segments: []*metapb.SegmentData{
						{StartOffset: 0, EndOffset: 100}, // Missing ~400 bytes
					},
				},
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.Error(t, err)
		require.Contains(t, err.Error(), "corrupted nested source: missing 399 bytes")
	})
}
