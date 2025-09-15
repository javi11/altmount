package nzbfilesystem

import (
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
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