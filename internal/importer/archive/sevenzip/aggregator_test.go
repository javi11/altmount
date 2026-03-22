package sevenzip

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
			},
		}
		err := validateSegmentIntegrity(ctx, content)
		require.Error(t, err)
		require.Contains(t, err.Error(), "corrupted file: missing 400 bytes")
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
}
