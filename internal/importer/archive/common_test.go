package archive

import (
	"context"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// seg builds a SegmentData covering [0, size-1].
func seg(id string, size int64) *metapb.SegmentData {
	return &metapb.SegmentData{Id: id, StartOffset: 0, EndOffset: size - 1, SegmentSize: size}
}

// TestValidateSegmentIntegrityUsesPackedSize documents that the shared validator
// compares segment coverage against PackedSize, not the (possibly larger) unpacked
// Size. This is required for COMPRESSED archives (e.g. 7z): segments carry the packed
// stream, so a compressed file legitimately has coverage == PackedSize < Size. The
// stricter "coverage must equal declared Size" rule applies only to stored RAR sets
// and lives in the rar package, where compressed files are rejected upstream.
func TestValidateSegmentIntegrityUsesPackedSize(t *testing.T) {
	tests := []struct {
		name    string
		content Content
		wantErr bool
	}{
		{
			name: "compressed file: coverage matches PackedSize despite larger Size",
			content: Content{
				Size:       1000, // unpacked (decompressed) size
				PackedSize: 800,  // compressed stream size
				Segments:   []*metapb.SegmentData{seg("a@x", 800)},
			},
			wantErr: false,
		},
		{
			name: "packed stream under-covered fails",
			content: Content{
				Size:       1000,
				PackedSize: 800,
				Segments:   []*metapb.SegmentData{seg("a@x", 400)}, // 50% of packed missing
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSegmentIntegrity(context.Background(), tt.content)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}
