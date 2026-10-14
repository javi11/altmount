package nzbfilesystem

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/metadata"
	"github.com/kipsilabs/altmount/internal/nzbfilesystem/segcache"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
	"github.com/kipsilabs/altmount/internal/testsupport/segments"
	"github.com/kipsilabs/altmount/internal/utils"
	"github.com/stretchr/testify/require"
)

// Tracking at the FUSE layer owns registration, OUT metrics, and removal.
// The metadata reader must still attribute actual NNTP downloads to its ID.
type fuseDownloadTracker struct {
	noopStreamTracker
	mu                     sync.Mutex
	downloads              map[string]int64
	adds, outputs, removes int
}

func (s *fuseDownloadTracker) Add(_, _, _, _, _ string, _ int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adds++
	return "duplicate"
}
func (s *fuseDownloadTracker) UpdateProgress(string, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputs++
}
func (s *fuseDownloadTracker) UpdateCurrentOffset(string, int64)  { s.UpdateProgress("", 0) }
func (s *fuseDownloadTracker) UpdateBufferedOffset(string, int64) { s.UpdateProgress("", 0) }
func (s *fuseDownloadTracker) Remove(string)                      { s.mu.Lock(); defer s.mu.Unlock(); s.removes++ }
func (s *fuseDownloadTracker) UpdateDownloadProgress(id string, n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloads[id] += n
}

func TestFUSEStreamDownloadAttribution(t *testing.T) {
	for _, key := range []string{"id", "read-at-id", "active-stream", "suppressed-without-id"} {
		t.Run(key, func(t *testing.T) {
			const size = 1024
			ms := metadata.NewMetadataService(t.TempDir())
			writeMetaForTestFile(t, ms, "video.mkv", 1, size)
			fp := fakepool.New()
			configurePoolForFile(fp, 1, size, fakepool.SegmentBehavior{})
			cfg := config.DefaultConfig()
			getCfg := func() *config.Config { return cfg }
			cache := segcache.NewSource(getCfg)
			tracker := &fuseDownloadTracker{downloads: make(map[string]int64)}
			remote := NewMetadataRemoteFile(ms, nil, nil, nil, newFakePoolManager(fp), getCfg, tracker, cache)
			for _, cached := range []bool{false, true} {
				id := "uncached-handle"
				if cached {
					id = "cached-handle"
				}
				ctx := context.WithValue(context.Background(), utils.SuppressStreamTrackingKey, true)
				switch key {
				case "id", "read-at-id":
					ctx = context.WithValue(ctx, utils.StreamIDKey, id)
				case "active-stream":
					ctx = context.WithValue(ctx, utils.ActiveStreamKey, &ActiveStream{ID: id})
				case "suppressed-without-id":
					id = ""
				}
				exists, file, err := remote.OpenFile(ctx, "video.mkv")
				require.NoError(t, err)
				require.True(t, exists)
				mvf := file.(*MetadataVirtualFile)
				if !cached && key != "read-at-id" {
					got, err := io.ReadAll(mvf)
					require.NoError(t, err)
					require.Equal(t, segments.FileBytes(1, size), got)
				} else {
					got := make([]byte, size)
					n, err := mvf.ReadAtContext(ctx, got, 0)
					require.NoError(t, err)
					require.Equal(t, size, n)
					require.Equal(t, segments.FileBytes(1, size), got)
				}
				if !cached {
					require.Eventually(t, func() bool {
						tracker.mu.Lock()
						defer tracker.mu.Unlock()
						return tracker.downloads[id] == int64(size)
					}, time.Second, time.Millisecond, "NNTP bytes must reach the FUSE stream")
					require.Eventually(t, func() bool { _, ok := cache.Store().Get(segments.MessageID(0)); return ok }, time.Second, time.Millisecond)
				}
				_, err = mvf.Seek(1, io.SeekStart)
				require.NoError(t, err)
				require.NoError(t, file.Close())
				tracker.mu.Lock()
				downloaded := tracker.downloads[id]
				adds, outputs, removes := tracker.adds, tracker.outputs, tracker.removes
				tracker.mu.Unlock()
				if !cached {
					require.Equal(t, int64(size), downloaded, "NNTP bytes must reach the FUSE stream")
				} else if id != "" {
					require.Zero(t, downloaded, "cache hits must not count as downloaded bytes")
				}
				require.Zero(t, adds, "underlying open must not register duplicate streams")
				require.Zero(t, outputs, "FUSE owns OUT counters and playback offsets")
				require.Zero(t, removes, "underlying close must not remove the FUSE stream")
			}
			require.Equal(t, int64(1), fp.BodyCalls()+fp.BodyPriorityCalls(), "warm reads should use the cache")
		})
	}
}
