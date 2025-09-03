package nzbfilesystem

import (
	"fmt"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/utils"
)

// Default constants for testing - these match the values in config.go
const (
	DefaultMaxRangeSize       = 33554432 // 32MB - Maximum range size for a single request
	DefaultStreamingChunkSize = 8388608  // 8MB - Chunk size for streaming when end=-1
)

// createTestVirtualFile creates a MetadataVirtualFile with default configuration for testing
func createTestVirtualFile(fileSize int64) *MetadataVirtualFile {
	return &MetadataVirtualFile{
		fileMeta: &metapb.FileMetadata{
			FileSize: fileSize,
		},
		maxRangeSize:       DefaultMaxRangeSize,
		streamingChunkSize: DefaultStreamingChunkSize,
	}
}

func TestCalculateIntelligentRange(t *testing.T) {
	// Create a mock file with 100MB size
	fileSize := int64(100 * 1024 * 1024) // 100MB
	mvf := createTestVirtualFile(fileSize)

	tests := []struct {
		name          string
		inputStart    int64
		inputEnd      int64
		expectedStart int64
		expectedEnd   int64
		description   string
	}{
		{
			name:          "end=-1 with small remaining",
			inputStart:    fileSize - 4*1024*1024, // 4MB from end
			inputEnd:      -1,
			expectedStart: fileSize - 4*1024*1024,
			expectedEnd:   fileSize - 1, // Should use all remaining
			description:   "When remaining size is smaller than chunk size, use all remaining",
		},
		{
			name:          "end=-1 with large remaining",
			inputStart:    0,
			inputEnd:      -1,
			expectedStart: 0,
			expectedEnd:   DefaultStreamingChunkSize - 1, // Should limit to chunk size
			description:   "When remaining size is larger than chunk size, limit to chunk size",
		},
		{
			name:          "range larger than max",
			inputStart:    0,
			inputEnd:      DefaultMaxRangeSize + 1024*1024, // 1MB over max
			expectedStart: 0,
			expectedEnd:   DefaultMaxRangeSize - 1, // Should limit to max range
			description:   "Range larger than max should be limited",
		},
		{
			name:          "normal range within limits",
			inputStart:    1024 * 1024,     // 1MB
			inputEnd:      2*1024*1024 - 1, // 2MB - 1
			expectedStart: 1024 * 1024,
			expectedEnd:   2*1024*1024 - 1,
			description:   "Normal range within limits should pass through unchanged",
		},
		{
			name:          "small range should be preserved exactly",
			inputStart:    1024, // 1KB
			inputEnd:      2048, // 2KB
			expectedStart: 1024,
			expectedEnd:   2048,
			description:   "Small ranges should be preserved exactly as requested",
		},
		{
			name:          "medium range under 32MB should be preserved",
			inputStart:    0,
			inputEnd:      16*1024*1024 - 1, // 16MB - 1
			expectedStart: 0,
			expectedEnd:   16*1024*1024 - 1,
			description:   "Medium ranges under 32MB should be preserved exactly",
		},
		{
			name:          "start beyond file size",
			inputStart:    fileSize + 1000,
			inputEnd:      fileSize + 2000,
			expectedStart: fileSize - 1, // Should be corrected to last byte
			expectedEnd:   fileSize - 1,
			description:   "Start beyond file size should be corrected",
		},
		{
			name:          "end beyond file size",
			inputStart:    fileSize - 1024*1024, // 1MB from end
			inputEnd:      fileSize + 1000,
			expectedStart: fileSize - 1024*1024,
			expectedEnd:   fileSize - 1, // Should be limited to file end
			description:   "End beyond file size should be limited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := mvf.calculateIntelligentRange(tt.inputStart, tt.inputEnd)

			if start != tt.expectedStart {
				t.Errorf("calculateIntelligentRange() start = %d, want %d", start, tt.expectedStart)
			}
			if end != tt.expectedEnd {
				t.Errorf("calculateIntelligentRange() end = %d, want %d", end, tt.expectedEnd)
			}

			// Verify range is valid
			if start > end {
				t.Errorf("calculateIntelligentRange() invalid range: start %d > end %d", start, end)
			}

			// Verify range doesn't exceed file size
			if end >= fileSize {
				t.Errorf("calculateIntelligentRange() end %d >= file size %d", end, fileSize)
			}

			// Verify range doesn't exceed max range size (except for very small files)
			rangeSize := end - start + 1
			if rangeSize > DefaultMaxRangeSize && fileSize > DefaultMaxRangeSize {
				t.Errorf("calculateIntelligentRange() range size %d > max %d", rangeSize, DefaultMaxRangeSize)
			}

			t.Logf("%s: start=%d, end=%d, size=%d", tt.description, start, end, rangeSize)
		})
	}
}

func TestGetRequestRangeWithoutHeader(t *testing.T) {
	fileSize := int64(50 * 1024 * 1024) // 50MB
	mvf := createTestVirtualFile(fileSize)
	mvf.args = utils.PathWithArgs{} // Empty args, no range header

	start, end := mvf.getRequestRange()

	// Should start from 0 and be limited by streaming chunk size
	expectedStart := int64(0)
	expectedEnd := int64(DefaultStreamingChunkSize - 1)

	if start != expectedStart {
		t.Errorf("getRequestRange() start = %d, want %d", start, expectedStart)
	}
	if end != expectedEnd {
		t.Errorf("getRequestRange() end = %d, want %d", end, expectedEnd)
	}

	rangeSize := end - start + 1
	if rangeSize != int64(DefaultStreamingChunkSize) {
		t.Errorf("getRequestRange() range size = %d, want %d", rangeSize, int64(DefaultStreamingChunkSize))
	}
}

func TestIntelligentRangePreservesOriginalRequest(t *testing.T) {
	fileSize := int64(100 * 1024 * 1024) // 100MB

	tests := []struct {
		name        string
		rangeStart  int64
		rangeEnd    int64
		description string
	}{
		{
			name:        "small specific range should be preserved",
			rangeStart:  1024,       // 1KB
			rangeEnd:    1024 * 100, // 100KB
			description: "Small specific ranges should pass through unchanged",
		},
		{
			name:        "medium range under limit should be preserved",
			rangeStart:  0,
			rangeEnd:    10*1024*1024 - 1, // 10MB - 1
			description: "Medium ranges under 32MB should be preserved exactly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create PathWithArgs with range header set
			args := utils.NewPathWithArgs("/test/file")
			rangeHeader := fmt.Sprintf("bytes=%d-%d", tt.rangeStart, tt.rangeEnd)
			args.SetRange(rangeHeader)

			mvf := createTestVirtualFile(fileSize)
			mvf.args = args

			start, end := mvf.getRequestRange()

			if start != tt.rangeStart {
				t.Errorf("getRequestRange() start = %d, want %d", start, tt.rangeStart)
			}
			if end != tt.rangeEnd {
				t.Errorf("getRequestRange() end = %d, want %d", end, tt.rangeEnd)
			}

			t.Logf("%s: preserved range [%d, %d] size=%d", tt.description, start, end, end-start+1)
		})
	}
}

func TestProgressiveRangeReading(t *testing.T) {
	// Test that when a smart range is exhausted, a new reader is created for remaining data
	fileSize := int64(50 * 1024 * 1024) // 50MB file

	// Create PathWithArgs with unbounded range (end=-1)
	args := utils.NewPathWithArgs("/test/file")
	rangeHeader := "bytes=0-" // Unbounded range
	args.SetRange(rangeHeader)

	mvf := createTestVirtualFile(fileSize)
	mvf.args = args

	// Test initial range calculation
	start, end := mvf.getRequestRange()

	// Should start at 0 and be limited to streaming chunk size
	expectedStart := int64(0)
	expectedEnd := int64(DefaultStreamingChunkSize - 1)

	if start != expectedStart {
		t.Errorf("Initial getRequestRange() start = %d, want %d", start, expectedStart)
	}
	if end != expectedEnd {
		t.Errorf("Initial getRequestRange() end = %d, want %d", end, expectedEnd)
	}

	// Verify original range was saved correctly
	if mvf.originalRangeEnd != -1 {
		t.Errorf("originalRangeEnd = %d, want -1 (unbounded)", mvf.originalRangeEnd)
	}

	// Simulate advancing position to end of first range
	mvf.position = DefaultStreamingChunkSize
	mvf.readerInitialized = true
	mvf.currentRangeStart = 0
	mvf.currentRangeEnd = DefaultStreamingChunkSize - 1

	// Test hasMoreDataToRead
	if !mvf.hasMoreDataToRead() {
		t.Error("hasMoreDataToRead() should return true when position < fileSize and original range was unbounded")
	}

	// Close current reader (simulates EOF)
	mvf.closeCurrentReader()

	// Get next range
	nextStart, nextEnd := mvf.getRequestRange()

	expectedNextStart := int64(DefaultStreamingChunkSize)
	expectedNextEnd := int64(2*DefaultStreamingChunkSize - 1)

	if nextStart != expectedNextStart {
		t.Errorf("Next getRequestRange() start = %d, want %d", nextStart, expectedNextStart)
	}
	if nextEnd != expectedNextEnd {
		t.Errorf("Next getRequestRange() end = %d, want %d", nextEnd, expectedNextEnd)
	}

	t.Logf("Progressive reading: first range [%d, %d], next range [%d, %d]",
		start, end, nextStart, nextEnd)
}

func TestProgressiveRangeWithBoundedOriginal(t *testing.T) {
	// Test progressive reading with a bounded original range
	fileSize := int64(100 * 1024 * 1024)   // 100MB file
	originalEnd := int64(50*1024*1024 - 1) // Original request was for 50MB

	// Create PathWithArgs with bounded range
	args := utils.NewPathWithArgs("/test/file")
	rangeHeader := fmt.Sprintf("bytes=0-%d", originalEnd)
	args.SetRange(rangeHeader)

	mvf := createTestVirtualFile(fileSize)
	mvf.args = args

	// Get initial range
	_, _ = mvf.getRequestRange()

	// Should respect the bounded original range
	if mvf.originalRangeEnd != originalEnd {
		t.Errorf("originalRangeEnd = %d, want %d", mvf.originalRangeEnd, originalEnd)
	}

	// Simulate advancing to near end of original range
	mvf.position = originalEnd - 1000 // 1KB before end
	mvf.readerInitialized = true
	mvf.currentRangeStart = 0
	mvf.currentRangeEnd = DefaultStreamingChunkSize - 1

	// Should still have more data to read within original range
	if !mvf.hasMoreDataToRead() {
		t.Error("hasMoreDataToRead() should return true when position <= originalRangeEnd")
	}

	// Simulate reaching exactly the original end
	mvf.position = originalEnd

	// Should have no more data to read (position == originalRangeEnd)
	if mvf.hasMoreDataToRead() {
		t.Error("hasMoreDataToRead() should return false when position >= originalRangeEnd")
	}

	// Move beyond the original end
	mvf.position = originalEnd + 1

	// Should definitely have no more data to read
	if mvf.hasMoreDataToRead() {
		t.Error("hasMoreDataToRead() should return false when position > originalRangeEnd")
	}

	t.Logf("Bounded progressive reading: original end %d, final position %d", originalEnd, mvf.position)
}

func TestConfigurableRangeSizes(t *testing.T) {
	// Test that custom range sizes are properly used
	fileSize := int64(100 * 1024 * 1024) // 100MB file

	tests := []struct {
		name                 string
		customMaxRangeSize   int64
		customStreamingChunk int64
		inputStart           int64
		inputEnd             int64
		expectedEnd          int64
		description          string
	}{
		{
			name:                 "custom small max range size",
			customMaxRangeSize:   2 * 1024 * 1024, // 2MB max
			customStreamingChunk: DefaultStreamingChunkSize,
			inputStart:           0,
			inputEnd:             10*1024*1024 - 1, // 10MB request
			expectedEnd:          2*1024*1024 - 1,  // Limited to 2MB
			description:          "Large range should be limited to custom max size",
		},
		{
			name:                 "custom small streaming chunk",
			customMaxRangeSize:   DefaultMaxRangeSize,
			customStreamingChunk: 1 * 1024 * 1024, // 1MB chunks
			inputStart:           0,
			inputEnd:             -1,              // Unbounded
			expectedEnd:          1*1024*1024 - 1, // Limited to 1MB chunk
			description:          "Unbounded range should use custom streaming chunk size",
		},
		{
			name:                 "both custom values small",
			customMaxRangeSize:   4 * 1024 * 1024, // 4MB max
			customStreamingChunk: 1 * 1024 * 1024, // 1MB chunks
			inputStart:           0,
			inputEnd:             -1,              // Unbounded
			expectedEnd:          1*1024*1024 - 1, // Uses streaming chunk (smaller)
			description:          "Unbounded range uses streaming chunk even when max is larger",
		},
		{
			name:                 "large custom values",
			customMaxRangeSize:   64 * 1024 * 1024, // 64MB max
			customStreamingChunk: 16 * 1024 * 1024, // 16MB chunks
			inputStart:           0,
			inputEnd:             -1,               // Unbounded
			expectedEnd:          16*1024*1024 - 1, // Uses larger streaming chunk
			description:          "Can configure larger values than defaults",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mvf := &MetadataVirtualFile{
				fileMeta: &metapb.FileMetadata{
					FileSize: fileSize,
				},
				maxRangeSize:       tt.customMaxRangeSize,
				streamingChunkSize: tt.customStreamingChunk,
			}

			start, end := mvf.calculateIntelligentRange(tt.inputStart, tt.inputEnd)

			if start != tt.inputStart {
				t.Errorf("calculateIntelligentRange() start = %d, want %d", start, tt.inputStart)
			}
			if end != tt.expectedEnd {
				t.Errorf("calculateIntelligentRange() end = %d, want %d", end, tt.expectedEnd)
			}

			rangeSize := end - start + 1
			t.Logf("%s: custom_max=%dMB, custom_chunk=%dMB, result=[%d,%d], size=%dMB",
				tt.description, tt.customMaxRangeSize/(1024*1024), tt.customStreamingChunk/(1024*1024),
				start, end, rangeSize/(1024*1024))
		})
	}
}

func TestSmartRangeCalculation(t *testing.T) {
	// Test comprehensive smart range calculation scenarios
	fileSize := int64(200 * 1024 * 1024) // 200MB file

	tests := []struct {
		name          string
		inputStart    int64
		inputEnd      int64
		expectedStart int64
		expectedEnd   int64
		description   string
		shouldLimit   bool // Whether smart limiting should be applied
	}{
		{
			name:          "small range preserved exactly",
			inputStart:    1000,
			inputEnd:      5000,
			expectedStart: 1000,
			expectedEnd:   5000,
			description:   "Small ranges should pass through unchanged",
			shouldLimit:   false,
		},
		{
			name:          "medium range under 32MB preserved",
			inputStart:    0,
			inputEnd:      20*1024*1024 - 1, // 20MB
			expectedStart: 0,
			expectedEnd:   20*1024*1024 - 1,
			description:   "Medium ranges under max limit preserved",
			shouldLimit:   false,
		},
		{
			name:          "large range gets limited to 32MB",
			inputStart:    0,
			inputEnd:      50*1024*1024 - 1, // 50MB
			expectedStart: 0,
			expectedEnd:   DefaultMaxRangeSize - 1, // Limited to 32MB
			description:   "Ranges larger than 32MB should be limited",
			shouldLimit:   true,
		},
		{
			name:          "unbounded range from start gets chunked",
			inputStart:    0,
			inputEnd:      -1,
			expectedStart: 0,
			expectedEnd:   DefaultStreamingChunkSize - 1, // 8MB chunk
			description:   "Unbounded ranges should be chunked to streaming size",
			shouldLimit:   true,
		},
		{
			name:          "unbounded from middle gets chunked",
			inputStart:    50 * 1024 * 1024, // Start at 50MB
			inputEnd:      -1,
			expectedStart: 50 * 1024 * 1024,
			expectedEnd:   50*1024*1024 + DefaultStreamingChunkSize - 1,
			description:   "Unbounded ranges from middle should chunk from position",
			shouldLimit:   true,
		},
		{
			name:          "small remaining with unbounded uses all",
			inputStart:    fileSize - 2*1024*1024, // 2MB from end
			inputEnd:      -1,
			expectedStart: fileSize - 2*1024*1024,
			expectedEnd:   fileSize - 1, // Use all remaining
			description:   "Small remaining data with unbounded should use all",
			shouldLimit:   false, // Uses all remaining, not limited
		},
		{
			name:          "range at file boundary",
			inputStart:    fileSize - 1000,
			inputEnd:      fileSize + 500, // Beyond file
			expectedStart: fileSize - 1000,
			expectedEnd:   fileSize - 1, // Limited to file end
			description:   "Range extending beyond file should be limited to file end",
			shouldLimit:   true,
		},
		{
			name:          "start beyond file corrected",
			inputStart:    fileSize + 1000,
			inputEnd:      fileSize + 2000,
			expectedStart: fileSize - 1, // Corrected to last byte
			expectedEnd:   fileSize - 1,
			description:   "Start beyond file should be corrected to last byte",
			shouldLimit:   true,
		},
		{
			name:          "exactly 32MB range preserved",
			inputStart:    1024 * 1024,                         // 1MB
			inputEnd:      1024*1024 + DefaultMaxRangeSize - 1, // Exactly 32MB
			expectedStart: 1024 * 1024,
			expectedEnd:   1024*1024 + DefaultMaxRangeSize - 1,
			description:   "Exactly 32MB range should be preserved",
			shouldLimit:   false,
		},
		{
			name:          "32MB + 1 byte gets limited",
			inputStart:    1024 * 1024,                     // 1MB
			inputEnd:      1024*1024 + DefaultMaxRangeSize, // 32MB + 1 byte
			expectedStart: 1024 * 1024,
			expectedEnd:   1024*1024 + DefaultMaxRangeSize - 1, // Limited to exactly 32MB
			description:   "Range of 32MB + 1 byte should be limited to 32MB",
			shouldLimit:   true,
		},
		{
			name:          "negative start corrected",
			inputStart:    -1000,
			inputEnd:      5000,
			expectedStart: 0, // Corrected to start of file
			expectedEnd:   5000,
			description:   "Negative start should be corrected to 0",
			shouldLimit:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mvf := createTestVirtualFile(fileSize)

			start, end := mvf.calculateIntelligentRange(tt.inputStart, tt.inputEnd)

			if start != tt.expectedStart {
				t.Errorf("calculateIntelligentRange() start = %d, want %d", start, tt.expectedStart)
			}
			if end != tt.expectedEnd {
				t.Errorf("calculateIntelligentRange() end = %d, want %d", end, tt.expectedEnd)
			}

			// Verify range is valid
			if start > end {
				t.Errorf("calculateIntelligentRange() invalid range: start %d > end %d", start, end)
			}

			// Verify range doesn't exceed file size
			if end >= fileSize {
				t.Errorf("calculateIntelligentRange() end %d >= file size %d", end, fileSize)
			}

			// Verify range size limits
			rangeSize := end - start + 1
			if rangeSize > DefaultMaxRangeSize {
				t.Errorf("calculateIntelligentRange() range size %d > max %d", rangeSize, DefaultMaxRangeSize)
			}

			// Check if limiting was applied as expected
			originalSize := tt.inputEnd - tt.inputStart + 1
			if tt.inputEnd == -1 {
				originalSize = fileSize - tt.inputStart
			}
			limitingApplied := rangeSize < originalSize

			if tt.shouldLimit && !limitingApplied {
				t.Errorf("Expected smart limiting to be applied, but range size %d equals original size", rangeSize)
			}

			t.Logf("%s: input=[%d,%d] -> output=[%d,%d] size=%d, limited=%v",
				tt.description, tt.inputStart, tt.inputEnd, start, end, rangeSize, limitingApplied)
		})
	}
}

func TestSmartRangeEdgeCases(t *testing.T) {
	// Test edge cases and boundary conditions
	tests := []struct {
		name        string
		fileSize    int64
		inputStart  int64
		inputEnd    int64
		description string
	}{
		{
			name:        "tiny file with large request",
			fileSize:    1024, // 1KB file
			inputStart:  0,
			inputEnd:    1024 * 1024, // Request 1MB
			description: "Large request on tiny file should be limited to file size",
		},
		{
			name:        "empty file edge case",
			fileSize:    0,
			inputStart:  0,
			inputEnd:    -1,
			description: "Edge case: empty file with unbounded request",
		},
		{
			name:        "single byte file",
			fileSize:    1,
			inputStart:  0,
			inputEnd:    -1,
			description: "Single byte file with unbounded request",
		},
		{
			name:        "exactly streaming chunk size file",
			fileSize:    DefaultStreamingChunkSize,
			inputStart:  0,
			inputEnd:    -1,
			description: "File exactly matching streaming chunk size",
		},
		{
			name:        "file smaller than streaming chunk",
			fileSize:    DefaultStreamingChunkSize / 2,
			inputStart:  0,
			inputEnd:    -1,
			description: "File smaller than streaming chunk size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mvf := createTestVirtualFile(tt.fileSize)

			start, end := mvf.calculateIntelligentRange(tt.inputStart, tt.inputEnd)

			// Basic validity checks
			if start < 0 {
				t.Errorf("calculateIntelligentRange() start %d < 0", start)
			}

			// For empty files, we expect an invalid range (start > end)
			if tt.fileSize == 0 {
				if start <= end {
					t.Errorf("calculateIntelligentRange() expected invalid range for empty file, got [%d,%d]", start, end)
				}
			} else {
				// For non-empty files, range should be valid
				if start > end {
					t.Errorf("calculateIntelligentRange() start %d > end %d", start, end)
				}
				if end >= tt.fileSize {
					t.Errorf("calculateIntelligentRange() end %d >= file size %d", end, tt.fileSize)
				}
			}

			rangeSize := end - start + 1
			t.Logf("%s: file_size=%d, range=[%d,%d], size=%d",
				tt.description, tt.fileSize, start, end, rangeSize)
		})
	}
}
