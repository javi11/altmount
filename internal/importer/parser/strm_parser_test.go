package parser

import (
	"strings"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrmParser_ParseStrmFile(t *testing.T) {
	parser := NewStrmParser()

	tests := []struct {
		name               string
		strmContent        string
		expectError        bool
		expectedFilename   string
		expectedSize       int64
		expectedEncryption string
	}{
		{
			name:               "valid NXG link with rclone encryption",
			strmContent:        `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&cipher=rclone&file_size=6942851782&name=Beautiful+Wedding+%282024%29+%5Bimdb-tt26546123%5D%5BWEBDL-1080p%5D%5B8bit%5D%5Bh264%5D%5BEAC3+5.1%5D-HDZ.mkv&password=qR95KQ0w7VxvGs3&salt=85yLF0Q7Mrijw9Rs4z3V16e2`,
			expectError:        false,
			expectedFilename:   "Beautiful Wedding (2024) [imdb-tt26546123][WEBDL-1080p][8bit][h264][EAC3 5.1]-HDZ.mkv", // URL decoded
			expectedSize:       6942851782,
			expectedEncryption: "rclone",
		},
		{
			name:               "valid NXG link without encryption",
			strmContent:        `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&file_size=6944718848&name=test_file.mkv`,
			expectError:        false,
			expectedFilename:   "test_file.mkv",
			expectedSize:       6944718848,
			expectedEncryption: "none",
		},
		{
			name:        "invalid URL scheme",
			strmContent: `http://example.com/test`,
			expectError: true,
		},
		{
			name:        "missing required parameter h",
			strmContent: `nxglnk://?chunk_size=1048576&file_size=1000000&name=test_file.mkv`,
			expectError: true,
		},
		{
			name:        "missing required parameter chunk_size",
			strmContent: `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&file_size=1000000&name=test_file.mkv`,
			expectError: true,
		},
		{
			name:        "missing required parameter file_size",
			strmContent: `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&name=test_file.mkv`,
			expectError: true,
		},
		{
			name:        "missing required parameter name",
			strmContent: `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&file_size=1000000`,
			expectError: true,
		},
		{
			name:        "invalid chunk_size",
			strmContent: `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=invalid&file_size=1000000&name=test_file.mkv`,
			expectError: true,
		},
		{
			name:        "invalid file_size",
			strmContent: `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&file_size=invalid&name=test_file.mkv`,
			expectError: true,
		},
		{
			name:        "empty STRM file",
			strmContent: ``,
			expectError: true,
		},
		{
			name: "STRM file with comments and blank lines",
			strmContent: `# This is a comment

nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&file_size=6944718848&name=test_file.mkv
# Another comment`,
			expectError:        false,
			expectedFilename:   "test_file.mkv",
			expectedSize:       6944718848,
			expectedEncryption: "none",
		},
		{
			name:               "RAR file detection",
			strmContent:        `nxglnk://?h=TzlIY1lxNVFNQ0MyOXE6NjYyMzow&chunk_size=1048576&file_size=6944718848&name=test_archive.rar`,
			expectError:        false,
			expectedFilename:   "test_archive.rar",
			expectedSize:       6944718848,
			expectedEncryption: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.strmContent)

			parsed, err := parser.ParseStrmFile(reader, "test.strm")

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)

			// Check basic structure
			assert.Equal(t, NzbTypeStrm, parsed.Type)
			assert.Equal(t, "test.strm", parsed.Filename)
			assert.Equal(t, 1, len(parsed.Files))
			assert.Equal(t, tt.expectedSize, parsed.TotalSize)

			// Check file details
			file := parsed.Files[0]
			assert.Equal(t, tt.expectedFilename, file.Filename)
			assert.Equal(t, tt.expectedSize, file.Size)
			assert.True(t, len(file.Segments) > 0, "should have segments")

			// Check encryption
			switch tt.expectedEncryption {
			case "rclone":
				assert.Equal(t, "RCLONE", file.Encryption.String())
				assert.NotEmpty(t, file.Password)
				assert.NotEmpty(t, file.Salt)
			default:
				assert.Equal(t, "NONE", file.Encryption.String())
			}

			// Check RAR detection
			if strings.HasSuffix(tt.expectedFilename, ".rar") {
				assert.True(t, file.IsRarArchive)
			}

			// Validate segments
			var totalSize int64
			for i, segment := range file.Segments {
				assert.NotEmpty(t, segment.Id, "segment %d should have ID", i)
				assert.GreaterOrEqual(t, segment.StartOffset, int64(0), "segment %d start offset should be >= 0", i)
				assert.Greater(t, segment.EndOffset, segment.StartOffset, "segment %d end offset should be > start offset", i)
				totalSize += segment.EndOffset - segment.StartOffset + 1
			}

			// For unencrypted files, segment total should match file size
			if tt.expectedEncryption == "none" {
				assert.Equal(t, tt.expectedSize, totalSize, "total segment size should match file size")
			}
		})
	}
}

func TestStrmParser_ValidateStrmFile(t *testing.T) {
	parser := NewStrmParser()

	tests := []struct {
		name        string
		parsed      *ParsedNzb
		expectError bool
	}{
		{
			name: "valid STRM file",
			parsed: &ParsedNzb{
				Type: NzbTypeStrm,
				Files: []ParsedFile{{
					Size: 1000,
					Segments: []*metapb.SegmentData{{
						Id:          "test",
						StartOffset: 0,
						EndOffset:   999,
					}},
				}},
				TotalSize:     1000,
				SegmentsCount: 1,
			},
			expectError: false,
		},
		{
			name: "wrong type",
			parsed: &ParsedNzb{
				Type: NzbTypeSingleFile,
				Files: []ParsedFile{{
					Size: 1000,
					Segments: []*metapb.SegmentData{{
						Id:          "test",
						StartOffset: 0,
						EndOffset:   999,
					}},
				}},
				TotalSize:     1000,
				SegmentsCount: 1,
			},
			expectError: true,
		},
		{
			name: "multiple files",
			parsed: &ParsedNzb{
				Type: NzbTypeStrm,
				Files: []ParsedFile{
					{Size: 1000, Segments: []*metapb.SegmentData{{
						Id:          "test1",
						StartOffset: 0,
						EndOffset:   999,
					}}},
					{Size: 2000, Segments: []*metapb.SegmentData{{
						Id:          "test2",
						StartOffset: 0,
						EndOffset:   1999,
					}}},
				},
				TotalSize:     3000,
				SegmentsCount: 2,
			},
			expectError: true,
		},
		{
			name: "zero total size",
			parsed: &ParsedNzb{
				Type: NzbTypeStrm,
				Files: []ParsedFile{{
					Size: 0,
					Segments: []*metapb.SegmentData{{
						Id:          "test",
						StartOffset: 0,
						EndOffset:   999,
					}},
				}},
				TotalSize:     0,
				SegmentsCount: 1,
			},
			expectError: true,
		},
		{
			name: "no segments",
			parsed: &ParsedNzb{
				Type:          NzbTypeStrm,
				Files:         []ParsedFile{{Size: 1000, Segments: []*metapb.SegmentData{}}},
				TotalSize:     1000,
				SegmentsCount: 0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.ValidateStrmFile(tt.parsed)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
