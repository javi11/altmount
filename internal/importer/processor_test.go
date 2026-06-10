package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/multifile"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"
)

type processorTestPoolManager struct {
	client *fakepool.Client
}

func (m processorTestPoolManager) GetPool() (pool.NntpClient, error) { return m.client, nil }
func (m processorTestPoolManager) SetProviders([]nntppool.Provider) error {
	return nil
}
func (m processorTestPoolManager) ClearPool() error { return nil }
func (m processorTestPoolManager) HasPool() bool    { return m.client != nil }
func (m processorTestPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m processorTestPoolManager) ResetMetrics(context.Context, bool, bool) error { return nil }
func (m processorTestPoolManager) ResetProviderErrors(context.Context) error      { return nil }
func (m processorTestPoolManager) IncArticlesDownloaded()                         {}
func (m processorTestPoolManager) UpdateDownloadProgress(string, int64)           {}
func (m processorTestPoolManager) IncArticlesPosted()                             {}
func (m processorTestPoolManager) AddProvider(nntppool.Provider) error            { return nil }
func (m processorTestPoolManager) RemoveProvider(string) error                    { return nil }
func (m processorTestPoolManager) ResetProviderQuota(context.Context, string) error {
	return nil
}
func (m processorTestPoolManager) SetProviderIDs(map[string]string) {}
func (m processorTestPoolManager) AcquireImportSlot(context.Context) (func(), error) {
	return func() {}, nil
}
func (m processorTestPoolManager) SetAdmissionCaps(int, int)                 {}
func (m processorTestPoolManager) SetStreamSource(pool.StreamActivitySource) {}
func (m processorTestPoolManager) NotifyStreamChange()                       {}

func TestPreParseFastFailSkipsOnlyMissingEpisode(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("missing-segment", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}
	n := buildTestNzb([]testNzbFile{
		{name: "Show.S01E01.mkv", segID: "healthy-segment"},
		{name: "Show.S01E02.mkv", segID: "missing-segment"},
		{name: "Show.S01E01.par2", segID: "par2-segment"},
	})
	cfg := config.DefaultConfig()
	cfg.Import.SegmentSamplePercentage = 100

	brokenIdx, missingIDs, err := proc.preParseFastFail(context.Background(), n, cfg, 1, 1)
	if err != nil {
		t.Fatalf("preParseFastFail returned error: %v", err)
	}

	// Only file index 1 (the missing episode) should be broken.
	if len(brokenIdx) != 1 {
		t.Fatalf("brokenIdx len = %d, want 1", len(brokenIdx))
	}
	if _, ok := brokenIdx[1]; !ok {
		t.Error("brokenIdx missing index 1 (Show.S01E02.mkv)")
	}
	// The missing segment ID must be in missingIDs.
	if _, ok := missingIDs["missing-segment"]; !ok {
		t.Error("missingIDs missing 'missing-segment'")
	}
}

// TestPreParseFastFailDoesNotStatPar2 verifies PAR2 segments are skipped entirely
// from the fast-fail Stat sweep: an unreachable PAR2 segment must neither be
// Stat-checked nor mark the import broken.
// TestPreParseFastFailMarksWholeRarSetBroken verifies that one unreachable part
// dooms its entire RAR set (all parts in brokenIdx) without touching a healthy
// sibling set.
func TestPreParseFastFailMarksWholeRarSetBroken(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("a2-missing", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}
	n := buildTestNzb([]testNzbFile{
		{name: "setA.part01.rar", segID: "a1"},
		{name: "setA.part02.rar", segID: "a2-missing"},
		{name: "setB.part01.rar", segID: "b1"},
		{name: "setB.part02.rar", segID: "b2"},
	})
	cfg := config.DefaultConfig()
	cfg.Import.SegmentSamplePercentage = 100

	brokenIdx, _, err := proc.preParseFastFail(context.Background(), n, cfg, 1, 1)
	if err != nil {
		t.Fatalf("preParseFastFail returned error: %v", err)
	}
	if len(brokenIdx) != 2 {
		t.Fatalf("brokenIdx = %v, want both setA parts (indexes 0,1)", brokenIdx)
	}
	for _, idx := range []int{0, 1} {
		if _, ok := brokenIdx[idx]; !ok {
			t.Errorf("brokenIdx missing index %d (setA part)", idx)
		}
	}
	for _, idx := range []int{2, 3} {
		if _, ok := brokenIdx[idx]; ok {
			t.Errorf("brokenIdx contains index %d (healthy setB part), want absent", idx)
		}
	}
}

// TestPreParseFastFailAllRarSetsBrokenReturnsNoFilesProcessed verifies the
// logical-unit early exit: when every RAR set has a missing part, the import
// fails before parsing.
func TestPreParseFastFailAllRarSetsBrokenReturnsNoFilesProcessed(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("a2-missing", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	client.SetBehavior("b2-missing", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}
	n := buildTestNzb([]testNzbFile{
		{name: "setA.part01.rar", segID: "a1"},
		{name: "setA.part02.rar", segID: "a2-missing"},
		{name: "setB.part01.rar", segID: "b1"},
		{name: "setB.part02.rar", segID: "b2-missing"},
	})
	cfg := config.DefaultConfig()
	cfg.Import.SegmentSamplePercentage = 100

	_, _, err := proc.preParseFastFail(context.Background(), n, cfg, 1, 1)
	if !errors.Is(err, multifile.ErrNoFilesProcessed) {
		t.Fatalf("preParseFastFail error = %v, want ErrNoFilesProcessed", err)
	}
}

func TestPreParseFastFailDoesNotStatPar2(t *testing.T) {
	client := fakepool.New()
	// The PAR2 segment would error if it were ever Stat-checked.
	client.SetBehavior("par2-segment", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}
	n := buildTestNzb([]testNzbFile{
		{name: "Show.S01E01.mkv", segID: "healthy-segment"},
		{name: "Show.S01E01.par2", segID: "par2-segment"},
	})
	cfg := config.DefaultConfig()
	cfg.Import.SegmentSamplePercentage = 100

	brokenIdx, missingIDs, err := proc.preParseFastFail(context.Background(), n, cfg, 1, 1)
	if err != nil {
		t.Fatalf("preParseFastFail returned error: %v", err)
	}
	if len(brokenIdx) != 0 {
		t.Fatalf("brokenIdx = %v, want empty (PAR2 must not break the import)", brokenIdx)
	}
	if len(missingIDs) != 0 {
		t.Fatalf("missingIDs = %v, want empty", missingIDs)
	}
	// Only the media file's single segment should have been Stat-checked.
	if got := client.StatCalls(); got != 1 {
		t.Fatalf("StatCalls = %d, want 1 (PAR2 segment must not be Stat-checked)", got)
	}
}

func TestPreParseFastFailAllMissingReturnsNoFilesProcessed(t *testing.T) {
	client := fakepool.New()
	client.SetDefaultBehavior(fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}
	n := buildTestNzb([]testNzbFile{
		{name: "Show.S01E01.mkv", segID: "missing-1"},
		{name: "Show.S01E02.mkv", segID: "missing-2"},
		{name: "Show.S01E01.par2", segID: "par2-segment"},
	})
	cfg := config.DefaultConfig()
	cfg.Import.SegmentSamplePercentage = 100

	_, _, err := proc.preParseFastFail(context.Background(), n, cfg, 1, 1)
	if !errors.Is(err, multifile.ErrNoFilesProcessed) {
		t.Fatalf("preParseFastFail error = %v, want ErrNoFilesProcessed", err)
	}
}

// TestPreParseFastFailHealthyReleaseSkipsPerFileSweep verifies the phase-1
// release probe short-circuits on a healthy release: it Stats only a bounded
// sample (≤55) regardless of file count and never escalates to the per-file
// sweep, so the "Checking segment availability" stage stays cheap.
func TestPreParseFastFailHealthyReleaseSkipsPerFileSweep(t *testing.T) {
	client := fakepool.New() // all segments reachable by default
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}

	const fileCount = 100
	files := make([]testNzbFile, fileCount)
	for i := range files {
		files[i] = testNzbFile{
			name:  fmt.Sprintf("Show.S01E%02d.mkv", i),
			segID: fmt.Sprintf("seg-%d", i),
		}
	}
	n := buildTestNzb(files)
	cfg := config.DefaultConfig() // default 1% sampling, capped at 55

	brokenIdx, missingIDs, err := proc.preParseFastFail(context.Background(), n, cfg, 1, 1)
	if err != nil {
		t.Fatalf("preParseFastFail returned error: %v", err)
	}
	if brokenIdx != nil {
		t.Errorf("brokenIdx = %v, want nil (healthy release)", brokenIdx)
	}
	if missingIDs != nil {
		t.Errorf("missingIDs = %v, want nil (healthy release)", missingIDs)
	}
	// The probe samples the whole release once (capped at 55). With no missing
	// segment it must NOT escalate, so total Stats stay well under fileCount.
	if got := client.StatCalls(); got > 55 {
		t.Errorf("StatCalls = %d, want ≤55 (release probe only, no per-file sweep)", got)
	}
}

func TestFastFailConcurrency(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name     string
		cfg      *config.Config
		fallback int
		want     int
	}{
		{
			name:     "sums enabled providers (nil Enabled counts as enabled)",
			cfg:      &config.Config{Providers: []config.ProviderConfig{{MaxConnections: 10, Enabled: &enabled}, {MaxConnections: 20}}},
			fallback: 5,
			want:     30,
		},
		{
			name:     "skips disabled providers",
			cfg:      &config.Config{Providers: []config.ProviderConfig{{MaxConnections: 10, Enabled: &disabled}, {MaxConnections: 20, Enabled: &enabled}}},
			fallback: 5,
			want:     20,
		},
		{
			name:     "falls back when no capacity configured",
			cfg:      &config.Config{},
			fallback: 7,
			want:     7,
		},
		{
			name:     "caps at 100",
			cfg:      &config.Config{Providers: []config.ProviderConfig{{MaxConnections: 500, Enabled: &enabled}}},
			fallback: 5,
			want:     100,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fastFailConcurrency(tt.cfg, tt.fallback); got != tt.want {
				t.Fatalf("fastFailConcurrency = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProcessMultiFilePreservesReleaseFolderWhenOnlyOneFileRemains(t *testing.T) {
	client := fakepool.New()
	metaRoot := t.TempDir()
	cfg := config.DefaultConfig()
	proc := &Processor{
		metadataService:   metadata.NewMetadataService(metaRoot),
		poolManager:       processorTestPoolManager{client: client},
		configGetter:      func() *config.Config { return cfg },
		validationTimeout: 100 * time.Millisecond,
	}

	result, writtenPaths, err := proc.processMultiFile(
		context.Background(),
		"tv/Show/Season 01",
		[]parser.ParsedFile{processorTestParsedFile("Show.S01E01.mkv", "healthy-segment")},
		nil,
		"Show.S01.nzb",
		1,
		[]string{".mkv"},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("processMultiFile returned error: %v", err)
	}

	if result != "tv/Show/Season 01/Show.S01" {
		t.Fatalf("result = %q, want release folder", result)
	}
	wantPath := "tv/Show/Season 01/Show.S01/Show.S01E01.mkv"
	if len(writtenPaths) != 1 || writtenPaths[0] != wantPath {
		t.Fatalf("writtenPaths = %v, want %q", writtenPaths, wantPath)
	}
	if _, err := os.Stat(filepath.Join(metaRoot, wantPath+".meta")); err != nil {
		t.Fatalf("metadata for surviving episode not written in release folder: %v", err)
	}
}

func processorTestParsedFile(filename, segmentID string) parser.ParsedFile {
	return parser.ParsedFile{
		Filename: filename,
		Size:     100,
		Segments: []*metapb.SegmentData{
			{Id: segmentID, StartOffset: 0, EndOffset: 99},
		},
		ReleaseDate: time.Unix(1, 0),
	}
}

func TestApplyNzbRename(t *testing.T) {
	tests := []struct {
		name             string
		renameToNzbName  bool
		nzbName          string
		originalFilename string
		expected         string
	}{
		{
			name:             "false: single file not renamed",
			renameToNzbName:  false,
			nzbName:          "release.nzb",
			originalFilename: "obfuscated.mkv",
			expected:         "obfuscated.mkv",
		},
		{
			name:             "true: single file renamed",
			renameToNzbName:  true,
			nzbName:          "release.nzb",
			originalFilename: "obfuscated.mkv",
			expected:         "release.mkv",
		},
		{
			name:             "false: preserves path with subdirectory",
			renameToNzbName:  false,
			nzbName:          "movie.nzb",
			originalFilename: "sub/file.mkv",
			expected:         "sub/file.mkv",
		},
		{
			name:             "true: renames leaf only, preserves subdirectory",
			renameToNzbName:  true,
			nzbName:          "movie.nzb",
			originalFilename: "sub/file.mkv",
			expected:         "sub/movie.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := []parser.ParsedFile{{Filename: tt.originalFilename}}
			result := applyNzbRename(tt.renameToNzbName, tt.nzbName, files)
			if result[0].Filename != tt.expected {
				t.Fatalf("applyNzbRename(%v, %q, [{%q}]) = %q, want %q",
					tt.renameToNzbName, tt.nzbName, tt.originalFilename, result[0].Filename, tt.expected)
			}
		})
	}
}

func TestNormalizeReleaseFilename(t *testing.T) {
	tests := []struct {
		name             string
		nzbFilename      string
		originalFilename string
		expected         string
	}{
		{
			name:             "keeps single extension",
			nzbFilename:      "file.mkv.nzb",
			originalFilename: "file.mkv",
			expected:         "file.mkv",
		},
		{
			name:             "adds missing extension from original",
			nzbFilename:      "obfuscated.nzb",
			originalFilename: "video.mkv",
			expected:         "obfuscated.mkv",
		},
		{
			name:             "avoids duplicate when nzb already has ext",
			nzbFilename:      "[TEST].mp4.nzb",
			originalFilename: "random.mp4",
			expected:         "[TEST].mp4",
		},
		{
			name:             "preserves nzb basename but uses original ext",
			nzbFilename:      "movie.1080p.NZB",
			originalFilename: "source.mkv",
			expected:         "movie.1080p.mkv",
		},
		{
			name:             "no extension in original",
			nzbFilename:      "sample.nzb",
			originalFilename: "filename",
			expected:         "sample",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeReleaseFilename(tt.nzbFilename, tt.originalFilename)
			if got != tt.expected {
				t.Fatalf("normalizeReleaseFilename(%q, %q) = %q, want %q", tt.nzbFilename, tt.originalFilename, got, tt.expected)
			}
		})
	}
}

func TestNormalizeSingleFileVirtualDir(t *testing.T) {
	tests := []struct {
		name        string
		virtualDir  string
		releaseName string
		filename    string
		expected    string
	}{
		{
			name:        "keeps season folder",
			virtualDir:  "/Media/Animes/Show/Season 01",
			releaseName: "Show.S01E01.1080p",
			filename:    "Show.S01E01.1080p.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
		{
			name:        "flattens when ends with filename",
			virtualDir:  "/Media/Animes/Show/Season 01/Show.S01E01.1080p.mkv",
			releaseName: "Show.S01E01.1080p",
			filename:    "Show.S01E01.1080p.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
		{
			name:        "flattens when ends with release name",
			virtualDir:  "/Media/Animes/Show/Season 01/Show.S01E01.1080p",
			releaseName: "Show.S01E01.1080p",
			filename:    "episode.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
		{
			name:        "root stays root",
			virtualDir:  "/",
			releaseName: "Anything",
			filename:    "file.mkv",
			expected:    "/",
		},
		{
			name:        "does not flatten when file has path",
			virtualDir:  "/Media/Animes/Show/Season 01",
			releaseName: "Show.S01E01.1080p",
			filename:    "sub/Show.S01E01.1080p.mkv",
			expected:    "/Media/Animes/Show/Season 01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSingleFileVirtualDir(tt.virtualDir, tt.releaseName, tt.filename)
			if got != tt.expected {
				t.Fatalf("normalizeSingleFileVirtualDir(%q, %q, %q) = %q, want %q", tt.virtualDir, tt.releaseName, tt.filename, got, tt.expected)
			}
		})
	}
}

func TestDetermineFileLocation(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		baseDir        string
		expectedParent string
		expectedName   string
	}{
		{
			name:           "simple file",
			filename:       "movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "nested file",
			filename:       "folder/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base/folder",
			expectedName:   "movie.mkv",
		},
		{
			name:           "redundant folder (exact match)",
			filename:       "movie.mkv/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "redundant folder with leading slash",
			filename:       "/movie.mkv/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "redundant folder with backslashes",
			filename:       `movie.mkv\\movie.mkv`,
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
		{
			name:           "nested redundant folder",
			filename:       "series/season1/episode1.mkv/episode1.mkv",
			baseDir:        "/base",
			expectedParent: "/base/series/season1",
			expectedName:   "episode1.mkv",
		},
		{
			name:           "non-redundant folder (almost match)",
			filename:       "movie/movie.mkv",
			baseDir:        "/base",
			expectedParent: "/base",
			expectedName:   "movie.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := parser.ParsedFile{Filename: tt.filename}
			parent, name := filesystem.DetermineFileLocation(file, tt.baseDir)
			if parent != tt.expectedParent {
				t.Errorf("DetermineFileLocation parent = %q, want %q", parent, tt.expectedParent)
			}
			if name != tt.expectedName {
				t.Errorf("DetermineFileLocation name = %q, want %q", name, tt.expectedName)
			}
		})
	}
}

type testNzbFile struct {
	name  string
	segID string
}

func buildTestNzb(files []testNzbFile) *nzbparser.Nzb {
	nzbFiles := make(nzbparser.NzbFiles, len(files))
	for i, f := range files {
		nzbFiles[i] = nzbparser.NzbFile{
			Filename: f.name,
			Subject:  f.name,
			Segments: nzbparser.NzbSegments{
				{Bytes: 100, Number: 1, ID: f.segID},
			},
		}
	}
	return &nzbparser.Nzb{Files: nzbFiles}
}

func TestCalculateVirtualDirectory(t *testing.T) {
	tests := []struct {
		name         string
		nzbPath      string
		relativePath string
		expected     string
	}{
		{
			name:         "file in root of relative path",
			nzbPath:      "/downloads/sonarr/Movie.mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/",
		},
		{
			name:         "file in subfolder",
			nzbPath:      "/downloads/sonarr/MovieFolder/Movie.mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/MovieFolder",
		},
		{
			name:         "empty relative path",
			nzbPath:      "/downloads/Movie.mkv",
			relativePath: "",
			expected:     "/",
		},
		{
			name:         "file with spaces",
			nzbPath:      "/downloads/sonarr/Movie Name (2023).mkv",
			relativePath: "/downloads/sonarr",
			expected:     "/",
		},
		{
			name:         "file in persistent .nzbs directory",
			nzbPath:      "/config/.nzbs/MovieFolder/Movie.nzb",
			relativePath: "/config",
			expected:     "/MovieFolder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filesystem.CalculateVirtualDirectory(tt.nzbPath, tt.relativePath)
			if result != tt.expected {
				t.Errorf("CalculateVirtualDirectory(%q, %q) = %q, want %q", tt.nzbPath, tt.relativePath, result, tt.expected)
			}
		})
	}
}
