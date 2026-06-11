package parser

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"
)

// parser_storm_test.go pins the per-import invariants on the parser side.
// The parser bounds fan-out within a single import job via
// MaxImportConnections; cross-job budgeting is out of scope here.

// fakeFullPoolManager satisfies the full pool.Manager surface so the
// parser can call GetPool, HasPool, and the metric inc helpers without
// nil-deref.
type fakeFullPoolManager struct {
	client pool.NntpClient
}

func newFakeFullPoolManager(client pool.NntpClient) *fakeFullPoolManager {
	return &fakeFullPoolManager{client: client}
}

var _ pool.Manager = (*fakeFullPoolManager)(nil)

func (m *fakeFullPoolManager) GetPool() (pool.NntpClient, error)        { return m.client, nil }
func (m *fakeFullPoolManager) SetProviders(_ []nntppool.Provider) error { return nil }
func (m *fakeFullPoolManager) ClearPool() error                         { return nil }
func (m *fakeFullPoolManager) HasPool() bool                            { return true }
func (m *fakeFullPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m *fakeFullPoolManager) ResetMetrics(_ context.Context, _, _ bool) error { return nil }
func (m *fakeFullPoolManager) ResetProviderErrors(_ context.Context) error     { return nil }
func (m *fakeFullPoolManager) IncArticlesDownloaded()                          {}
func (m *fakeFullPoolManager) UpdateDownloadProgress(_ string, _ int64)        {}
func (m *fakeFullPoolManager) IncArticlesPosted()                              {}
func (m *fakeFullPoolManager) AddProvider(_ nntppool.Provider) error           { return nil }
func (m *fakeFullPoolManager) RemoveProvider(_ string) error                   { return nil }
func (m *fakeFullPoolManager) ResetProviderQuota(_ context.Context, _ string) error {
	return nil
}
func (m *fakeFullPoolManager) SetProviderIDs(_ map[string]string) {}
func (m *fakeFullPoolManager) AcquireImportSlot(_ context.Context) (func(), error) {
	return func() {}, nil
}
func (m *fakeFullPoolManager) SetAdmissionCaps(_ int, _ int)               {}
func (m *fakeFullPoolManager) SetStreamSource(_ pool.StreamActivitySource) {}
func (m *fakeFullPoolManager) NotifyStreamChange()                         {}

// stormConfigGetter returns a ConfigGetter whose MaxImportConnections is
// exactly N. This is the per-import-job cap on wire-call burstiness.
func stormConfigGetter(maxImportConnections int) config.ConfigGetter {
	cfg := config.DefaultConfig()
	cfg.Import.MaxImportConnections = maxImportConnections
	return func() *config.Config { return cfg }
}

// buildSyntheticNzbFiles returns numFiles nzbparser.NzbFile entries each
// pointing at a single fakepool-known message-ID. The parser's
// fetchAllFirstSegments path will issue one Body call per file — these
// are the calls we count.
func buildSyntheticNzbFiles(numFiles int) []nzbparser.NzbFile {
	files := make([]nzbparser.NzbFile, numFiles)
	for i := range files {
		files[i] = nzbparser.NzbFile{
			Filename: segments.MessageID(i) + ".bin",
			Segments: nzbparser.NzbSegments{
				{Bytes: 1024, Number: 1, ID: segments.MessageID(i)},
			},
		}
	}
	return files
}

// TestStorm_ImporterFanOutRespectsMaxImportConnections pins the per-job
// invariant: the parser's first-segment fan-out for a SINGLE import MUST
// stay bounded by MaxImportConnections. Cross-job bounding is out of
// scope for the parser.
func TestStorm_ImporterFanOutRespectsMaxImportConnections(t *testing.T) {
	t.Parallel()
	const (
		filesPerImport       = 20
		maxImportConnections = 6
		bodyLatency          = 60 * time.Millisecond
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	for i := 0; i < filesPerImport; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Latency: bodyLatency,
			Bytes:   make([]byte, 64),
		})
	}

	mgr := newFakeFullPoolManager(fp)
	parser := NewParser(mgr, stormConfigGetter(maxImportConnections))

	files := buildSyntheticNzbFiles(filesPerImport)
	_, _, _ = parser.fetchAllFirstSegments(ctx, files, nil, ParseOptions{})

	mif := fp.MaxInFlight()
	t.Logf("single import × %d files (MaxImportConnections=%d) "+
		"produced MaxInFlight=%d Body calls (invariant: MaxInFlight <= MaxImportConnections)",
		filesPerImport, maxImportConnections, mif)

	if mif > int32(maxImportConnections) {
		t.Errorf("INVARIANT regression: MaxInFlight=%d, want <= %d (MaxImportConnections). "+
			"fetchAllFirstSegments must size its concPool by MaxImportConnections — "+
			"if this fails, a single import is fanning out past its configured cap "+
			"and will saturate the slot semaphore on its own.",
			mif, maxImportConnections)
	}
}

// TestStorm_ImporterParallelImportsAreNotInternallyGated asserts that the
// parser itself does not bound cross-import fan-out. N concurrent imports
// each fan out up to MaxImportConnections, so MaxInFlight scales as
// N × MaxImportConnections. Cross-job admission control, if introduced,
// belongs at a higher layer — not inside the parser.
func TestStorm_ImporterParallelImportsAreNotInternallyGated(t *testing.T) {
	t.Parallel()
	const (
		concurrentImports    = 4
		filesPerImport       = 5
		maxImportConnections = 4
		bodyLatency          = 60 * time.Millisecond
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	for i := 0; i < filesPerImport; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Latency: bodyLatency,
			Bytes:   make([]byte, 64),
		})
	}

	mgr := newFakeFullPoolManager(fp)
	parser := NewParser(mgr, stormConfigGetter(maxImportConnections))

	var wg sync.WaitGroup
	for i := 0; i < concurrentImports; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			files := buildSyntheticNzbFiles(filesPerImport)
			_, _, _ = parser.fetchAllFirstSegments(ctx, files, nil, ParseOptions{})
		}()
	}
	wg.Wait()

	mif := fp.MaxInFlight()
	t.Logf("%d concurrent imports × %d files (MaxImportConnections=%d) "+
		"produced MaxInFlight=%d (parser does NOT internally cap cross-import fan-out)",
		concurrentImports, filesPerImport, maxImportConnections, mif)

	// The parser must NOT silently re-introduce an internal slot. If MaxInFlight
	// stays <= maxImportConnections under N concurrent imports, the parser is
	// gating internally — regression.
	minExpected := int32(maxImportConnections + 1)
	if mif < minExpected {
		t.Errorf("regression: MaxInFlight=%d, expected > %d. Parser appears to be "+
			"gating cross-import fan-out internally.",
			mif, maxImportConnections)
	}
}
