package multifile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
)

type testPoolManager struct {
	client *fakepool.Client
}

func (m testPoolManager) GetPool() (pool.NntpClient, error) { return m.client, nil }
func (m testPoolManager) SetProviders([]nntppool.Provider) error {
	return nil
}
func (m testPoolManager) ClearPool() error { return nil }
func (m testPoolManager) HasPool() bool    { return m.client != nil }
func (m testPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m testPoolManager) ResetMetrics(context.Context, bool, bool) error { return nil }
func (m testPoolManager) ResetProviderErrors(context.Context) error      { return nil }
func (m testPoolManager) IncArticlesDownloaded()                         {}
func (m testPoolManager) UpdateDownloadProgress(string, int64)           {}
func (m testPoolManager) IncArticlesPosted()                             {}
func (m testPoolManager) AddProvider(nntppool.Provider) error            { return nil }
func (m testPoolManager) RemoveProvider(string) error                    { return nil }
func (m testPoolManager) ResetProviderQuota(context.Context, string) error {
	return nil
}
func (m testPoolManager) SetProviderIDs(map[string]string) {}
func (m testPoolManager) AcquireImportSlot(context.Context) (func(), error) {
	return func() {}, nil
}
func (m testPoolManager) SetAdmissionCaps(int, int)                 {}
func (m testPoolManager) SetStreamSource(pool.StreamActivitySource) {}
func (m testPoolManager) NotifyStreamChange()                       {}

func TestProcessRegularFilesSkipsFileWithMissingSegments(t *testing.T) {
	ctx := context.Background()
	metaRoot := t.TempDir()
	svc := metadata.NewMetadataService(metaRoot)
	client := fakepool.New()
	client.SetBehavior("missing-segment", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []parser.ParsedFile{
		parsedTestFile("Show.S01E01.mkv", "healthy-segment"),
		parsedTestFile("Show.S01E02.mkv", "missing-segment"),
	}

	writtenPaths, err := ProcessRegularFiles(
		ctx,
		"tv/Show/Season 01",
		files,
		nil,
		"Show.S01.nzb",
		svc,
		testPoolManager{client: client},
		1,
		100,
		[]string{".mkv"},
		100*time.Millisecond,
		nil,
		true,
	)
	if err != nil {
		t.Fatalf("ProcessRegularFiles returned error: %v", err)
	}

	if len(writtenPaths) != 1 || writtenPaths[0] != "tv/Show/Season 01/Show.S01E01.mkv" {
		t.Fatalf("writtenPaths = %v, want only healthy episode", writtenPaths)
	}
	if !metadataExists(t, metaRoot, "tv/Show/Season 01/Show.S01E01.mkv") {
		t.Fatal("healthy episode metadata was not written")
	}
	if metadataExists(t, metaRoot, "tv/Show/Season 01/Show.S01E02.mkv") {
		t.Fatal("failed episode metadata was written")
	}
}

func TestProcessRegularFilesFailsWhenAllFilesHaveMissingSegments(t *testing.T) {
	ctx := context.Background()
	metaRoot := t.TempDir()
	svc := metadata.NewMetadataService(metaRoot)
	client := fakepool.New()
	client.SetDefaultBehavior(fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []parser.ParsedFile{
		parsedTestFile("Show.S01E01.mkv", "missing-1"),
		parsedTestFile("Show.S01E02.mkv", "missing-2"),
	}

	writtenPaths, err := ProcessRegularFiles(
		ctx,
		"tv/Show/Season 01",
		files,
		nil,
		"Show.S01.nzb",
		svc,
		testPoolManager{client: client},
		1,
		100,
		[]string{".mkv"},
		100*time.Millisecond,
		nil,
		true,
	)
	if err == nil {
		t.Fatal("ProcessRegularFiles returned nil error, want all-files-failed error")
	}
	if !errors.Is(err, ErrNoFilesProcessed) {
		t.Fatalf("ProcessRegularFiles error = %v, want ErrNoFilesProcessed", err)
	}
	if len(writtenPaths) != 0 {
		t.Fatalf("writtenPaths = %v, want none", writtenPaths)
	}
}

func parsedTestFile(filename, segmentID string) parser.ParsedFile {
	return parser.ParsedFile{
		Filename: filename,
		Size:     100,
		Segments: []*metapb.SegmentData{
			{Id: segmentID, StartOffset: 0, EndOffset: 99},
		},
		ReleaseDate: time.Unix(1, 0),
	}
}

func metadataExists(t *testing.T, metaRoot, virtualPath string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(metaRoot, virtualPath+".meta"))
	return err == nil
}
