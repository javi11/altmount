package nzbfilesystem

import (
	"io"
	"sync"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v2"
)

// createTestVirtualFile creates a MetadataVirtualFile with default configuration for testing
func createTestVirtualFile(fileSize int64) *MetadataVirtualFile {
	return &MetadataVirtualFile{
		fileMeta: &metapb.FileMetadata{
			FileSize: fileSize,
		},
	}
}

func TestCreateTestVirtualFile(t *testing.T) {
	fileSize := int64(100 * 1024 * 1024) // 100MB
	mvf := createTestVirtualFile(fileSize)

	if mvf.fileMeta.FileSize != fileSize {
		t.Errorf("createTestVirtualFile() fileSize = %d, want %d", mvf.fileMeta.FileSize, fileSize)
	}

	if mvf.fileMeta == nil {
		t.Error("createTestVirtualFile() fileMeta should not be nil")
	}
}

func TestBasicRangeCalculation(t *testing.T) {
	fileSize := int64(100 * 1024 * 1024) // 100MB

	tests := []struct {
		name       string
		start      int64
		end        int64
		expectErr  bool
		shouldPass bool
	}{
		{
			name:       "valid range within file",
			start:      0,
			end:        1024,
			expectErr:  false,
			shouldPass: true,
		},
		{
			name:       "range at file end",
			start:      fileSize - 1024,
			end:        fileSize - 1,
			expectErr:  false,
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic range validation
			if tt.start < 0 || tt.end >= fileSize || tt.start > tt.end {
				if tt.shouldPass {
					t.Errorf("Test %s: invalid range [%d, %d] for file size %d", tt.name, tt.start, tt.end, fileSize)
				}
			} else {
				if !tt.shouldPass {
					t.Errorf("Test %s: expected invalid range but got valid [%d, %d]", tt.name, tt.start, tt.end)
				}
			}
		})
	}
}

// TestBuildSegmentIndex tests the segment offset index building
func TestBuildSegmentIndex(t *testing.T) {
	tests := []struct {
		name     string
		segments []*metapb.SegmentData
		wantNil  bool
	}{
		{
			name:     "nil segments",
			segments: nil,
			wantNil:  true,
		},
		{
			name:     "empty segments",
			segments: []*metapb.SegmentData{},
			wantNil:  true,
		},
		{
			name: "single segment",
			segments: []*metapb.SegmentData{
				{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
			},
			wantNil: false,
		},
		{
			name: "multiple segments",
			segments: []*metapb.SegmentData{
				{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
				{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
				{StartOffset: 0, EndOffset: 499, SegmentSize: 500},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := buildSegmentIndex(tt.segments)
			if tt.wantNil {
				if idx != nil {
					t.Errorf("buildSegmentIndex() = %v, want nil", idx)
				}
			} else {
				if idx == nil {
					t.Errorf("buildSegmentIndex() = nil, want non-nil")
				}
				if len(idx.offsets) != len(tt.segments) {
					t.Errorf("buildSegmentIndex() offsets len = %d, want %d", len(idx.offsets), len(tt.segments))
				}
			}
		})
	}
}

// TestSegmentOffsetIndexFindSegment tests O(1) segment lookup
func TestSegmentOffsetIndexFindSegment(t *testing.T) {
	// Create an index with 3 segments: [0-999], [1000-1999], [2000-2499]
	segments := []*metapb.SegmentData{
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},  // usable: 1000 bytes
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},  // usable: 1000 bytes
		{StartOffset: 0, EndOffset: 499, SegmentSize: 500},   // usable: 500 bytes
	}
	idx := buildSegmentIndex(segments)

	tests := []struct {
		name   string
		offset int64
		want   int
	}{
		{"start of first segment", 0, 0},
		{"middle of first segment", 500, 0},
		{"end of first segment", 999, 0},
		{"start of second segment", 1000, 1},
		{"middle of second segment", 1500, 1},
		{"end of second segment", 1999, 1},
		{"start of third segment", 2000, 2},
		{"end of third segment", 2499, 2},
		{"negative offset", -1, -1},
		{"beyond end", 2500, -1},
		{"way beyond end", 10000, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.findSegmentForOffset(tt.offset)
			if got != tt.want {
				t.Errorf("findSegmentForOffset(%d) = %d, want %d", tt.offset, got, tt.want)
			}
		})
	}
}

// TestSegmentOffsetIndexNil tests nil index safety
func TestSegmentOffsetIndexNil(t *testing.T) {
	var idx *segmentOffsetIndex

	got := idx.findSegmentForOffset(100)
	if got != -1 {
		t.Errorf("nil index findSegmentForOffset() = %d, want -1", got)
	}

	// Also test getOffsetForSegment on nil index
	offset := idx.getOffsetForSegment(0)
	if offset != 0 {
		t.Errorf("nil index getOffsetForSegment() = %d, want 0", offset)
	}
}

// TestGetOffsetForSegment tests the getOffsetForSegment method
func TestGetOffsetForSegment(t *testing.T) {
	// Create an index with 3 segments: [0-999], [1000-1999], [2000-2499]
	segments := []*metapb.SegmentData{
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},  // usable: 1000 bytes, offset: 0
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},  // usable: 1000 bytes, offset: 1000
		{StartOffset: 0, EndOffset: 499, SegmentSize: 500},   // usable: 500 bytes, offset: 2000
	}
	idx := buildSegmentIndex(segments)

	tests := []struct {
		name         string
		segmentIndex int
		wantOffset   int64
	}{
		{"first segment", 0, 0},
		{"second segment", 1, 1000},
		{"third segment", 2, 2000},
		{"negative index", -1, 0},
		{"out of bounds", 3, 0},
		{"way out of bounds", 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.getOffsetForSegment(tt.segmentIndex)
			if got != tt.wantOffset {
				t.Errorf("getOffsetForSegment(%d) = %d, want %d", tt.segmentIndex, got, tt.wantOffset)
			}
		})
	}
}

// TestSegmentIndexIntegration tests that findSegmentForOffset and getOffsetForSegment work together
func TestSegmentIndexIntegration(t *testing.T) {
	// Create a realistic segment index with varying segment sizes
	segments := []*metapb.SegmentData{
		{StartOffset: 0, EndOffset: 749999, SegmentSize: 750000},  // 750KB
		{StartOffset: 0, EndOffset: 749999, SegmentSize: 750000},  // 750KB
		{StartOffset: 0, EndOffset: 749999, SegmentSize: 750000},  // 750KB
		{StartOffset: 0, EndOffset: 749999, SegmentSize: 750000},  // 750KB
		{StartOffset: 0, EndOffset: 249999, SegmentSize: 250000},  // 250KB (final partial)
	}
	idx := buildSegmentIndex(segments)

	// Test that findSegmentForOffset and getOffsetForSegment are consistent
	testOffsets := []int64{0, 375000, 750000, 1500000, 2250000, 2750000, 3000000}

	for _, offset := range testOffsets {
		segIdx := idx.findSegmentForOffset(offset)
		if segIdx < 0 {
			continue // Skip offsets beyond the file
		}

		segOffset := idx.getOffsetForSegment(segIdx)

		// The segment's start offset should be <= the query offset
		if segOffset > offset {
			t.Errorf("offset %d: segment %d starts at %d which is after the query offset",
				offset, segIdx, segOffset)
		}

		// The segment should contain this offset (check upper bound)
		if segIdx < len(segments) {
			usableLen := segments[segIdx].EndOffset - segments[segIdx].StartOffset + 1
			segEnd := segOffset + usableLen - 1
			if offset > segEnd {
				t.Errorf("offset %d: segment %d ends at %d which is before the query offset",
					offset, segIdx, segEnd)
			}
		}
	}
}

// TestReadAtBoundsValidation tests ReadAt boundary validation
func TestReadAtBoundsValidation(t *testing.T) {
	mvf := &MetadataVirtualFile{
		fileMeta: &metapb.FileMetadata{
			FileSize: 1000,
		},
	}

	tests := []struct {
		name    string
		offset  int64
		bufSize int
		wantErr error
	}{
		{"negative offset", -1, 100, ErrNegativeOffset},
		{"at file size", 1000, 100, io.EOF},
		{"beyond file size", 1500, 100, io.EOF},
		{"empty buffer", 0, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.bufSize)
			_, err := mvf.ReadAt(buf, tt.offset)

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ReadAt() error = %v, want nil", err)
				}
			} else {
				if err != tt.wantErr {
					t.Errorf("ReadAt() error = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

// TestReadAtNoPoolManager tests ReadAt when pool manager is nil
func TestReadAtNoPoolManager(t *testing.T) {
	segments := []*metapb.SegmentData{
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
	}

	mvf := &MetadataVirtualFile{
		fileMeta: &metapb.FileMetadata{
			FileSize:    1000,
			SegmentData: segments,
		},
		segmentIndex: buildSegmentIndex(segments),
		poolManager:  nil, // No pool manager
	}

	buf := make([]byte, 100)
	_, err := mvf.ReadAt(buf, 0)

	if err != ErrNoUsenetPool {
		t.Errorf("ReadAt() error = %v, want ErrNoUsenetPool", err)
	}
}

// TestReadAtNoSegments tests ReadAt when there are no segments
func TestReadAtNoSegments(t *testing.T) {
	mvf := &MetadataVirtualFile{
		fileMeta: &metapb.FileMetadata{
			FileSize:    1000,
			SegmentData: nil, // No segments
		},
		poolManager: &mockPoolManager{},
	}

	buf := make([]byte, 100)
	_, err := mvf.ReadAt(buf, 0)

	if err != ErrNoNzbData {
		t.Errorf("ReadAt() error = %v, want ErrNoNzbData", err)
	}
}

// Compile-time check that mockPoolManager implements pool.Manager
var _ pool.Manager = (*mockPoolManager)(nil)

// mockPoolManager implements pool.Manager for testing
type mockPoolManager struct{}

func (m *mockPoolManager) GetPool() (nntppool.UsenetConnectionPool, error) {
	return nil, nil
}

func (m *mockPoolManager) SetProviders(_ []nntppool.UsenetProviderConfig) error {
	return nil
}

func (m *mockPoolManager) ClearPool() error {
	return nil
}

func (m *mockPoolManager) HasPool() bool {
	return true
}

func (m *mockPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}

// TestConcurrentSegmentIndexAccess tests thread safety of segment index
func TestConcurrentSegmentIndexAccess(t *testing.T) {
	segments := []*metapb.SegmentData{
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
		{StartOffset: 0, EndOffset: 999, SegmentSize: 1000},
	}
	idx := buildSegmentIndex(segments)

	// Run concurrent lookups
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(offset int64) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = idx.findSegmentForOffset(offset % 3000)
			}
		}(int64(i * 30))
	}
	wg.Wait()
}
