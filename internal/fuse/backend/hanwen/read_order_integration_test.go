//go:build linux

package hanwen

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/nntppool/v5"
	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/fuse/backend"
	"github.com/kipsilabs/altmount/internal/metadata"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/kipsilabs/altmount/internal/nzbfilesystem"
	"github.com/kipsilabs/altmount/internal/pool"
	"github.com/kipsilabs/altmount/internal/testsupport/nntpserver"
	"github.com/kipsilabs/altmount/internal/testsupport/segments"
	"github.com/kipsilabs/altmount/internal/utils"
	"github.com/stretchr/testify/require"
)

// Exercise the Linux read entrypoint, real virtual file, UsenetReader and
// nntppool against a pipelined NNTP wire server, with no segment cache.
func TestHandle_ReorderedReadsBoundNNTPFetches(t *testing.T) {
	const (
		segSize   = 512 * 1024
		segCount  = 96
		chunk     = 128 * 1024
		delivered = 24 * 1024 * 1024
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := nntpserver.New(nntpserver.Config{ArticleSize: segSize, RTT: time.Millisecond})
	require.NoError(t, err)
	defer srv.Close()
	mgr := pool.NewManager(ctx, nil)
	require.NoError(t, mgr.SetProviders([]nntppool.Provider{{
		Host: "read-order-test", Factory: srv.Dial,
		Connections: 8, MinConnections: 8, Inflight: 4, StreamInflight: 4,
		SkipPing: true, IdleTimeout: time.Hour,
	}}))
	defer mgr.ClearPool()
	ms := metadata.NewMetadataService(t.TempDir())
	meta := &metapb.FileMetadata{FileSize: segCount * segSize, Status: metapb.FileStatus_FILE_STATUS_HEALTHY}
	for i := range segCount {
		meta.SegmentData = append(meta.SegmentData, &metapb.SegmentData{
			Id: segments.MessageID(i), SegmentSize: segSize, EndOffset: segSize - 1,
		})
	}
	require.NoError(t, ms.WriteFileMetadata("test.mkv", meta))
	cfg := config.DefaultConfig()
	cfg.Streaming.MaxPrefetch = 16
	remote := nzbfilesystem.NewMetadataRemoteFile(ms, nil, nil, nil, mgr, func() *config.Config { return cfg }, readOrderTracker{}, nil)
	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)
	found, file, err := remote.OpenFile(ctx, "test.mkv")
	require.NoError(t, err)
	require.True(t, found)
	src := file.(readAtContexter)
	async := backend.NewAsyncReadBuffer(ctx, src, 8*1024*1024, meta.FileSize, nil)
	h := NewHandle(file, slog.Default(), "test.mkv", nil, nil, async)
	defer h.Release(context.Background())
	// The server deliberately returns the same deterministic payload for every
	// article. Verify byte placement across each article boundary independently.
	payload := segments.Payload(0, segSize)
	read := func(off int64) {
		t.Helper()
		result, status := h.Read(ctx, make([]byte, chunk), off)
		require.Zero(t, status)
		got, readStatus := result.Bytes(nil)
		require.Zero(t, readStatus)
		require.Len(t, got, chunk)
		want := make([]byte, chunk)
		for i := range want {
			want[i] = payload[(int(off)+i)%segSize]
		}
		require.True(t, bytes.Equal(got, want), "incorrect data at %d", off)
	}
	for i := range 3 {
		read(int64(i * chunk))
	}
	for off := int64(3 * chunk); off+4*chunk <= delivered; off += 4 * chunk {
		read(off + chunk)
		read(off)
		read(off + 2*chunk)
		read(off + 3*chunk)
	}
	// Include all speculative traffic that drains on release in the bound.
	require.Zero(t, h.Release(context.Background()))
	c := srv.Counters()
	t.Logf("delivered ~%d bytes; NNTP BODY=%d, wire bytes=%d", delivered, c.Bodies, c.BytesWritten)
	// Demand (48 articles), <=16 ahead in the NNTP reader and <=16 in the
	// async buffer, with a small allowance for boundary/draining work.
	require.LessOrEqual(t, c.Bodies, int64(84), "reordered reads repeatedly fetched articles")
	require.LessOrEqual(t, c.BytesWritten, int64(84*segSize*2), "wire amplification exceeds bounded read-ahead and encoding")
	if os.Getenv("ALTMOUNT_TEST_FUSE") == "1" {
		t.Run("kernel_mount", func(t *testing.T) {
			_, file, err := remote.OpenFile(ctx, "test.mkv")
			require.NoError(t, err)
			async := backend.NewAsyncReadBuffer(ctx, file.(readAtContexter), 8*1024*1024, meta.FileSize, nil)
			handle := NewHandle(file, slog.Default(), "test.mkv", nil, nil, async)
			defer handle.Release(context.Background())
			root := &fs.Inode{}
			node := &mountedReadOrderFile{handle: handle, size: uint64(meta.FileSize)}
			mount := t.TempDir()
			server, err := fs.Mount(mount, root, &fs.Options{
				MountOptions: fuse.MountOptions{DirectMount: true, MaxWrite: 1024 * 1024, MaxReadAhead: 8 * 1024 * 1024},
				OnAdd: func(ctx context.Context) {
					root.AddChild("test.mkv", root.NewPersistentInode(ctx, node, fs.StableAttr{Mode: syscall.S_IFREG}), true)
				},
			})
			require.NoError(t, err)
			defer server.Unmount()
			require.NoError(t, server.WaitMount())
			before := srv.Counters()
			got, err := os.ReadFile(filepath.Join(mount, "test.mkv"))
			require.NoError(t, err)
			require.Len(t, got, segCount*segSize)
			for off := 0; off < len(got); off += segSize {
				require.True(t, bytes.Equal(got[off:off+segSize], payload), "mounted read differs at %d", off)
			}
			require.Zero(t, handle.Release(context.Background()))
			after := srv.Counters()
			t.Logf("mounted sequential delivery=%d bytes; NNTP BODY=%d wire bytes=%d", len(got), after.Bodies-before.Bodies, after.BytesWritten-before.BytesWritten)
			require.LessOrEqual(t, after.Bodies-before.Bodies, int64(segCount+32))
		})
	}
}

// Metrics are incidental to this read-order integration.
type readOrderTracker struct{}

func (readOrderTracker) Add(_, _, _, _, _ string, _ int64) string { return "" }
func (readOrderTracker) UpdateProgress(string, int64)             {}
func (readOrderTracker) UpdateDownloadProgress(string, int64)     {}
func (readOrderTracker) UpdateCurrentOffset(string, int64)        {}
func (readOrderTracker) UpdateBufferedOffset(string, int64)       {}
func (readOrderTracker) Remove(string)                            {}
func (readOrderTracker) IncArticlesDownloaded()                   {}
func (readOrderTracker) IncArticlesPosted()                       {}

// The opt-in mounted test leaves kernel async reads enabled and exercises the
// same handle entrypoint as the deterministic reordered workload above.
type mountedReadOrderFile struct {
	fs.Inode
	handle *Handle
	size   uint64
}

func (f *mountedReadOrderFile) Getattr(_ context.Context, _ fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | 0444
	out.Size = f.size
	return 0
}
func (f *mountedReadOrderFile) Read(ctx context.Context, _ fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	return f.handle.Read(ctx, dest, off)
}
func (f *mountedReadOrderFile) Open(context.Context, uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, 0, 0
}
