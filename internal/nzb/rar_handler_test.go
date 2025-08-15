package nzb

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/javi11/altmount/internal/database"
)

// TestDirectRarContentReader tests the optimized direct RAR content reader
func TestDirectRarContentReader(t *testing.T) {
	// Test the DirectRarContentReader struct creation
	reader := &DirectRarContentReader{
		ctx:            context.Background(),
		targetFilename: "test.txt",
		fileOffset:     1024,
		fileSize:       2048,
		currentPos:     0,
		maxWorkers:     4,
		log:            slog.Default(),
	}

	// Test initial state
	if reader.currentPos != 0 {
		t.Errorf("Expected initial position 0, got %d", reader.currentPos)
	}

	if reader.fileSize != 2048 {
		t.Errorf("Expected file size 2048, got %d", reader.fileSize)
	}

	if reader.fileOffset != 1024 {
		t.Errorf("Expected file offset 1024, got %d", reader.fileOffset)
	}

	// Test seeking
	newPos, err := reader.Seek(512, 0) // Seek to position 512 from start
	if err != nil {
		t.Errorf("Seek failed: %v", err)
	}

	if newPos != 512 {
		t.Errorf("Expected seek position 512, got %d", newPos)
	}

	if reader.currentPos != 512 {
		t.Errorf("Expected current position 512, got %d", reader.currentPos)
	}

	t.Log("✅ DirectRarContentReader basic functionality test passed")
}

// TestRarPartBoundaryCalculation tests the RAR part boundary calculation logic
func TestRarPartBoundaryCalculation(t *testing.T) {
	reader := &DirectRarContentReader{
		rarFiles: []ParsedFile{
			{Filename: "movie.part001.rar", Size: 1000},
			{Filename: "movie.part002.rar", Size: 1500},
			{Filename: "movie.part003.rar", Size: 800},
		},
		log: slog.Default(),
	}

	err := reader.calculatePartBoundaries()
	if err != nil {
		t.Errorf("Failed to calculate part boundaries: %v", err)
	}

	if len(reader.partBoundaries) != 3 {
		t.Errorf("Expected 3 part boundaries, got %d", len(reader.partBoundaries))
	}

	// Test first part
	if reader.partBoundaries[0].StartOffset != 0 {
		t.Errorf("Part 0 start offset should be 0, got %d", reader.partBoundaries[0].StartOffset)
	}
	if reader.partBoundaries[0].EndOffset != 999 {
		t.Errorf("Part 0 end offset should be 999, got %d", reader.partBoundaries[0].EndOffset)
	}

	// Test second part
	if reader.partBoundaries[1].StartOffset != 1000 {
		t.Errorf("Part 1 start offset should be 1000, got %d", reader.partBoundaries[1].StartOffset)
	}
	if reader.partBoundaries[1].EndOffset != 2499 {
		t.Errorf("Part 1 end offset should be 2499, got %d", reader.partBoundaries[1].EndOffset)
	}

	// Test third part
	if reader.partBoundaries[2].StartOffset != 2500 {
		t.Errorf("Part 2 start offset should be 2500, got %d", reader.partBoundaries[2].StartOffset)
	}
	if reader.partBoundaries[2].EndOffset != 3299 {
		t.Errorf("Part 2 end offset should be 3299, got %d", reader.partBoundaries[2].EndOffset)
	}

	// Test finding part for offset
	part, err := reader.findPartForOffset(1200)
	if err != nil {
		t.Errorf("Failed to find part for offset 1200: %v", err)
	}
	if part.PartIndex != 1 {
		t.Errorf("Expected part index 1 for offset 1200, got %d", part.PartIndex)
	}

	t.Log("✅ RAR part boundary calculation test passed")
}

// TestRarFileSegmentLoader tests the segment loader implementation
func TestRarFileSegmentLoader(t *testing.T) {
	segments := database.NzbSegments{
		{Number: 1, Bytes: 1024, MessageID: "message1", Groups: []string{"alt.binaries.test"}},
		{Number: 2, Bytes: 2048, MessageID: "message2", Groups: []string{"alt.binaries.test"}},
	}

	loader := rarFileSegmentLoader{segs: segments}

	// Test segment count
	if loader.GetSegmentCount() != 2 {
		t.Errorf("Expected 2 segments, got %d", loader.GetSegmentCount())
	}

	// Test first segment
	seg, groups, ok := loader.GetSegment(0)
	if !ok {
		t.Error("Failed to get first segment")
	}
	if seg.Number != 1 {
		t.Errorf("Expected segment number 1, got %d", seg.Number)
	}
	if seg.Bytes != 1024 {
		t.Errorf("Expected segment bytes 1024, got %d", seg.Bytes)
	}
	if seg.ID != "message1" {
		t.Errorf("Expected segment ID 'message1', got '%s'", seg.ID)
	}
	if len(groups) != 1 || groups[0] != "alt.binaries.test" {
		t.Errorf("Expected groups [alt.binaries.test], got %v", groups)
	}

	// Test invalid index
	_, _, ok = loader.GetSegment(10)
	if ok {
		t.Error("Should not return segment for invalid index")
	}

	t.Log("✅ RarFileSegmentLoader test passed")
}

// TestDirectRarContentReaderMultiVolumeReading tests reading files that span multiple RAR parts
func TestDirectRarContentReaderMultiVolumeReading(t *testing.T) {
	// Create a mock scenario where a file spans two RAR parts
	reader := &DirectRarContentReader{
		ctx:            context.Background(),
		targetFilename: "large_file.txt",
		fileOffset:     500,   // File starts at offset 500 in the RAR stream
		fileSize:       3000,  // File is 3000 bytes total
		currentPos:     0,     // Start reading from beginning of file
		maxWorkers:     4,
		log:            slog.Default(),
		rarFiles: []ParsedFile{
			{Filename: "movie.part001.rar", Size: 1000}, // Part 0: bytes 0-999
			{Filename: "movie.part002.rar", Size: 2000}, // Part 1: bytes 1000-2999  
			{Filename: "movie.part003.rar", Size: 1500}, // Part 2: bytes 3000-4499
		},
	}

	// Calculate part boundaries
	err := reader.calculatePartBoundaries()
	if err != nil {
		t.Fatalf("Failed to calculate part boundaries: %v", err)
	}

	// Test finding the correct part for file start (offset 500)
	part, err := reader.findPartForOffset(reader.fileOffset)
	if err != nil {
		t.Fatalf("Failed to find part for file start offset %d: %v", reader.fileOffset, err)
	}
	
	if part.PartIndex != 0 {
		t.Errorf("Expected part index 0 for offset %d, got %d", reader.fileOffset, part.PartIndex)
	}

	// Test finding part for middle of file (offset 500 + 1500 = 2000)
	middleOffset := reader.fileOffset + 1500
	part, err = reader.findPartForOffset(middleOffset)
	if err != nil {
		t.Fatalf("Failed to find part for middle offset %d: %v", middleOffset, err)
	}
	
	if part.PartIndex != 1 {
		t.Errorf("Expected part index 1 for offset %d, got %d", middleOffset, part.PartIndex)
	}

	// Test finding part for end of file (offset 500 + 3000 - 1 = 3499)
	endOffset := reader.fileOffset + reader.fileSize - 1
	part, err = reader.findPartForOffset(endOffset)
	if err != nil {
		t.Fatalf("Failed to find part for end offset %d: %v", endOffset, err)
	}
	
	if part.PartIndex != 2 {
		t.Errorf("Expected part index 2 for offset %d, got %d", endOffset, part.PartIndex)
	}

	// Test seeking across part boundaries
	// Seek to position that would be in part 1 (1500 bytes into the file)
	newPos, err := reader.Seek(1500, io.SeekStart)
	if err != nil {
		t.Fatalf("Failed to seek to position 1500: %v", err)
	}
	
	if newPos != 1500 {
		t.Errorf("Expected seek position 1500, got %d", newPos)
	}

	// Verify the position is tracked correctly
	if reader.currentPos != 1500 {
		t.Errorf("Expected current position 1500, got %d", reader.currentPos)
	}

	// Test seeking near end of file (in part 2)
	newPos, err = reader.Seek(2800, io.SeekStart) 
	if err != nil {
		t.Fatalf("Failed to seek to position 2800: %v", err)
	}
	
	if newPos != 2800 {
		t.Errorf("Expected seek position 2800, got %d", newPos)
	}

	// Test seeking beyond file size (should fail)
	_, err = reader.Seek(5000, io.SeekStart)
	if err == nil {
		t.Error("Expected error when seeking beyond file size")
	}

	t.Log("✅ DirectRarContentReader multi-volume reading test passed")
}