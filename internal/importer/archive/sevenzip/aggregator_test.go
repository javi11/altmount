package sevenzip

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/progress"
	"github.com/stretchr/testify/require"
)

// mockSevenZipProcessor is a test double for the Processor interface that returns
// pre-configured contents without hitting Usenet.
type mockSevenZipProcessor struct {
	contents []Content
}

func (m *mockSevenZipProcessor) AnalyzeSevenZipContentFromNzb(_ context.Context, _ []parser.ParsedFile, _ string, _ *progress.Tracker) ([]Content, error) {
	return m.contents, nil
}

func (m *mockSevenZipProcessor) CreateFileMetadataFromSevenZipContent(content Content, _ string, _ int64, _ string) *metapb.FileMetadata {
	return &metapb.FileMetadata{
		FileSize:    content.Size,
		SegmentData: content.Segments,
		Status:      metapb.FileStatus_FILE_STATUS_HEALTHY,
	}
}

// metaExists checks whether a .meta file exists for the given virtual path under metaRoot.
func metaExists(t *testing.T, metaRoot, virtualPath string) bool {
	t.Helper()
	metaPath := filepath.Join(metaRoot, virtualPath+".meta")
	_, err := os.Stat(metaPath)
	return err == nil
}

func TestProcessArchivePreservesInternalFolderStructure(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name             string
		contents         []Content
		virtualDir       string
		nzbPath          string
		renameToNzbName  bool
		extractedFiles   []parser.ExtractedFileInfo // override; nil = auto-build from contents
		wantMetaPaths    []string
		notWantMetaPaths []string
	}{
		{
			name:       "flat file: no subdirectory",
			virtualDir: "movies/MyMovie",
			nzbPath:    "movies/MyMovie.nzb",
			contents: []Content{
				{InternalPath: "video.mkv", Filename: "video.mkv", Size: 1000,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 999}}},
			},
			wantMetaPaths: []string{"movies/MyMovie/video.mkv"},
		},
		{
			name:       "file inside subdirectory: structure preserved",
			virtualDir: "movies/MyMovie",
			nzbPath:    "movies/MyMovie.nzb",
			contents: []Content{
				{InternalPath: "Extras/bonus.mkv", Filename: "bonus.mkv", Size: 500,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 499}}},
			},
			wantMetaPaths:    []string{"movies/MyMovie/Extras/bonus.mkv"},
			notWantMetaPaths: []string{"movies/MyMovie/bonus.mkv"},
		},
		{
			name:       "multiple files in same subdirectory",
			virtualDir: "tv/Show/Season01",
			nzbPath:    "tv/Show/Season01.nzb",
			contents: []Content{
				{InternalPath: "subs/en.srt", Filename: "en.srt", Size: 100,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 99}}},
				{InternalPath: "subs/fr.srt", Filename: "fr.srt", Size: 120,
					Segments: []*metapb.SegmentData{{Id: "seg2", StartOffset: 0, EndOffset: 119}}},
			},
			wantMetaPaths: []string{
				"tv/Show/Season01/subs/en.srt",
				"tv/Show/Season01/subs/fr.srt",
			},
			notWantMetaPaths: []string{
				"tv/Show/Season01/en.srt",
				"tv/Show/Season01/fr.srt",
			},
		},
		{
			name:            "single file with rename: placed flat ignoring internal subdir",
			virtualDir:      "movies/MyMovie",
			nzbPath:         "movies/MyMovie.nzb",
			renameToNzbName: true,
			contents: []Content{
				{InternalPath: "SubFolder/obfuscated.mkv", Filename: "obfuscated.mkv", Size: 2000,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 1999}}},
			},
			// The rename happens before the pre-extracted check, so supply the post-rename name
			extractedFiles:   []parser.ExtractedFileInfo{{Name: "MyMovie.mkv", Size: 2000}},
			wantMetaPaths:    []string{"movies/MyMovie/MyMovie.mkv"},
			notWantMetaPaths: []string{"movies/MyMovie/SubFolder/obfuscated.mkv"},
		},
		{
			name:            "season pack with rename: similar-size files are left alone",
			virtualDir:      "tv/Show.S01",
			nzbPath:         "tv/Show.S01.nzb",
			renameToNzbName: true,
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Filename: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 2000,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 1999}}},
				{InternalPath: "b3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Filename: "b3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 2100,
					Segments: []*metapb.SegmentData{{Id: "seg2", StartOffset: 0, EndOffset: 2099}}},
			},
			wantMetaPaths: []string{
				"tv/Show.S01/a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv",
				"tv/Show.S01/b3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv",
			},
			notWantMetaPaths: []string{"tv/Show.S01/Show.S01.mkv"},
		},
		{
			name:            "rename: subtitle sharing the payload stem follows it",
			virtualDir:      "movies/MyMovie",
			nzbPath:         "movies/MyMovie.nzb",
			renameToNzbName: true,
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Filename: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 20000,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 19999}}},
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.eng.srt", Filename: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.eng.srt", Size: 100,
					Segments: []*metapb.SegmentData{{Id: "seg2", StartOffset: 0, EndOffset: 99}}},
			},
			extractedFiles: []parser.ExtractedFileInfo{{Name: "MyMovie.mkv", Size: 20000}, {Name: "MyMovie.eng.srt", Size: 100}},
			wantMetaPaths:  []string{"movies/MyMovie/MyMovie.mkv", "movies/MyMovie/MyMovie.eng.srt"},
			notWantMetaPaths: []string{
				"movies/MyMovie/a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv",
				"movies/MyMovie/a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.eng.srt",
			},
		},
		{
			name:            "rename: clean payload name is kept",
			virtualDir:      "movies/MyMovie",
			nzbPath:         "movies/MyMovie.nzb",
			renameToNzbName: true,
			contents: []Content{
				{InternalPath: "My.Movie.2024.PROPER.1080p.mkv", Filename: "My.Movie.2024.PROPER.1080p.mkv", Size: 2000,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 1999}}},
			},
			wantMetaPaths:    []string{"movies/MyMovie/My.Movie.2024.PROPER.1080p.mkv"},
			notWantMetaPaths: []string{"movies/MyMovie/MyMovie.mkv"},
		},
		{
			name:       "windows-style backslash paths normalized",
			virtualDir: "movies/MyMovie",
			nzbPath:    "movies/MyMovie.nzb",
			contents: []Content{
				{InternalPath: `Extras\featurette.mkv`, Filename: "featurette.mkv", Size: 300,
					Segments: []*metapb.SegmentData{{Id: "seg1", StartOffset: 0, EndOffset: 299}}},
			},
			wantMetaPaths:    []string{"movies/MyMovie/Extras/featurette.mkv"},
			notWantMetaPaths: []string{"movies/MyMovie/featurette.mkv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metaRoot := t.TempDir()
			svc := metadata.NewMetadataService(metaRoot)
			proc := &mockSevenZipProcessor{contents: tt.contents}

			// Build extractedFiles so validation is skipped (no pool manager needed).
			// Use the override when provided (e.g. rename cases change baseFilename before the check).
			extracted := tt.extractedFiles
			if extracted == nil {
				extracted = make([]parser.ExtractedFileInfo, len(tt.contents))
				for i, c := range tt.contents {
					extracted[i] = parser.ExtractedFileInfo{
						Name: filepath.Base(c.Filename),
						Size: c.Size,
					}
				}
			}

			err := ProcessArchive(ctx, ProcessArchiveOptions{
				VirtualDir:             tt.virtualDir,
				ArchiveFiles:           []parser.ParsedFile{{Filename: "archive.7z"}},
				Password:               "",
				ReleaseDate:            0,
				NzbPath:                tt.nzbPath,
				Processor:              proc,
				MetadataService:        svc,
				PoolManager:            nil,
				ArchiveProgressTracker: nil,
				AllowedFileExtensions:  nil,
				ExtractedFiles:         extracted,
				MaxPrefetch:            1,
				ReadTimeout:            30 * time.Second,
				ExpandBlurayIso:        false,
				FilterSamples:          false,
				RenameToNzbName:        tt.renameToNzbName,
			})
			require.NoError(t, err)

			for _, vp := range tt.wantMetaPaths {
				require.True(t, metaExists(t, metaRoot, vp), "expected metadata at %s", vp)
			}
			for _, vp := range tt.notWantMetaPaths {
				require.False(t, metaExists(t, metaRoot, vp), "unexpected metadata at %s", vp)
			}
		})
	}
}

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
