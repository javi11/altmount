//go:build linux

package hanwen

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- in-memory nzbFS fake -------------------------------------------------

type fakeInfo struct {
	name string
	size int64
	dir  bool
}

func (fi fakeInfo) Name() string { return fi.name }
func (fi fakeInfo) Size() int64  { return fi.size }
func (fi fakeInfo) Mode() os.FileMode {
	if fi.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (fi fakeInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (fi fakeInfo) IsDir() bool        { return fi.dir }
func (fi fakeInfo) Sys() any           { return nil }

// fakeAferoFile is a snapshot of an entry taken at open time: Stat keeps
// reporting the size the handle was opened with, mirroring a real handle that
// is pinned to one revision of the file.
type fakeAferoFile struct {
	info     fakeInfo
	children []os.FileInfo
	closed   bool
}

func (f *fakeAferoFile) Close() error               { f.closed = true; return nil }
func (f *fakeAferoFile) Read(p []byte) (int, error) { return 0, io.EOF }
func (f *fakeAferoFile) ReadAt([]byte, int64) (int, error) {
	return 0, io.EOF
}
func (f *fakeAferoFile) Seek(int64, int) (int64, error)     { return 0, nil }
func (f *fakeAferoFile) Write([]byte) (int, error)          { return 0, syscall.EPERM }
func (f *fakeAferoFile) WriteAt([]byte, int64) (int, error) { return 0, syscall.EPERM }
func (f *fakeAferoFile) Name() string                       { return f.info.name }
func (f *fakeAferoFile) Readdir(int) ([]os.FileInfo, error) { return f.children, nil }
func (f *fakeAferoFile) Readdirnames(int) ([]string, error) {
	names := make([]string, 0, len(f.children))
	for _, c := range f.children {
		names = append(names, c.Name())
	}
	return names, nil
}
func (f *fakeAferoFile) Stat() (os.FileInfo, error)      { return f.info, nil }
func (f *fakeAferoFile) Sync() error                     { return nil }
func (f *fakeAferoFile) Truncate(int64) error            { return syscall.EPERM }
func (f *fakeAferoFile) WriteString(string) (int, error) { return 0, syscall.EPERM }

// fakeNzbFS is a minimal in-memory nzbFS. Paths are stored without a leading
// separator, matching what filepath.Join produces from the empty root path.
type fakeNzbFS struct {
	mu      sync.Mutex
	entries map[string]fakeInfo
}

func newFakeNzbFS() *fakeNzbFS {
	return &fakeNzbFS{entries: map[string]fakeInfo{"": {name: "", dir: true}}}
}

func (fs *fakeNzbFS) addFile(path string, size int64) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.entries[path] = fakeInfo{name: filepath.Base(path), size: size}
}

func (fs *fakeNzbFS) addDir(path string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.entries[path] = fakeInfo{name: filepath.Base(path), dir: true}
}

// replaceFile simulates an in-place replacement: same virtual path, new size.
func (fs *fakeNzbFS) replaceFile(path string, size int64) {
	fs.addFile(path, size)
}

func (fs *fakeNzbFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	info, ok := fs.entries[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return info, nil
}

func (fs *fakeNzbFS) Open(_ context.Context, name string) (afero.File, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	info, ok := fs.entries[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	f := &fakeAferoFile{info: info}
	if info.dir {
		f.children = fs.childrenLocked(name)
	}
	return f, nil
}

func (fs *fakeNzbFS) childrenLocked(dir string) []os.FileInfo {
	prefix := dir
	if prefix != "" {
		prefix += string(filepath.Separator)
	}
	var out []os.FileInfo
	for path, info := range fs.entries {
		if path == dir || !strings.HasPrefix(path, prefix) {
			continue
		}
		if strings.Contains(strings.TrimPrefix(path, prefix), string(filepath.Separator)) {
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func (fs *fakeNzbFS) Mkdir(_ context.Context, name string, _ os.FileMode) error {
	fs.addDir(name)
	return nil
}

func (fs *fakeNzbFS) Rename(_ context.Context, oldName, newName string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	info, ok := fs.entries[oldName]
	if !ok {
		return os.ErrNotExist
	}
	delete(fs.entries, oldName)
	info.name = filepath.Base(newName)
	fs.entries[newName] = info
	return nil
}

func (fs *fakeNzbFS) Remove(_ context.Context, name string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.entries[name]; !ok {
		return os.ErrNotExist
	}
	delete(fs.entries, name)
	return nil
}

// recordingTracker captures the size each Open reports for its stream.
type recordingTracker struct {
	mu    sync.Mutex
	sizes []int64
}

func (t *recordingTracker) AddStream(filePath, source, userName, clientIP, userAgent string, totalSize int64) *nzbfilesystem.ActiveStream {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sizes = append(t.sizes, totalSize)
	return &nzbfilesystem.ActiveStream{ID: filePath, FilePath: filePath, TotalSize: totalSize}
}

func (t *recordingTracker) UpdateProgress(string, int64) {}
func (t *recordingTracker) Remove(string)                {}

func (t *recordingTracker) recorded() []int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]int64(nil), t.sizes...)
}

// newTestFS builds a live go-fuse node tree (no kernel mount required) so
// Lookup runs through the real bridge, inode allocation and child registration.
func newTestFS(t *testing.T, backing *fakeNzbFS, tracker *recordingTracker) (fuse.RawFileSystem, *Dir) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := NewDir(backing, "", logger, 0, 0, tracker, 0, false)
	raw := fs.NewNodeFS(root, &fs.Options{})
	require.NotNil(t, raw)

	return raw, root
}

func lookupEntry(t *testing.T, raw fuse.RawFileSystem, parentNodeID uint64, name string) *fuse.EntryOut {
	t.Helper()

	var out fuse.EntryOut
	status := raw.Lookup(nil, &fuse.InHeader{NodeId: parentNodeID}, name, &out)
	require.True(t, status.Ok(), "Lookup(%q) failed: %v", name, status)

	return &out
}

// childOps returns the node implementation registered under parent for name.
func childOps(t *testing.T, parent *fs.Inode, name string) fs.InodeEmbedder {
	t.Helper()

	child := parent.GetChild(name)
	require.NotNil(t, child, "child %q is not registered on the parent inode", name)

	return child.Operations()
}

func childDir(t *testing.T, parent *fs.Inode, name string) *Dir {
	t.Helper()

	dir, ok := childOps(t, parent, name).(*Dir)
	require.True(t, ok, "child %q is not a *Dir", name)

	return dir
}

func childFile(t *testing.T, parent *fs.Inode, name string) *File {
	t.Helper()

	file, ok := childOps(t, parent, name).(*File)
	require.True(t, ok, "child %q is not a *File", name)

	return file
}

func openFile(t *testing.T, f *File) *Handle {
	t.Helper()

	fh, _, errno := f.Open(context.Background(), syscall.O_RDONLY)
	require.Equal(t, syscall.Errno(0), errno)

	handle, ok := fh.(*Handle)
	require.True(t, ok)

	return handle
}

// --- tests ----------------------------------------------------------------

// Readdir and Lookup must agree on inodes when both run through the real node
// implementations, not just when stableInode is called twice.
func TestLookupAndReaddirInodeParity(t *testing.T) {
	backing := newFakeNzbFS()
	backing.addDir("movies")
	backing.addFile(filepath.Join("movies", "Inception.2010.mkv"), 4096)
	backing.addFile(filepath.Join("movies", "Arrival.2016.mkv"), 8192)
	backing.addDir(filepath.Join("movies", "extras"))

	raw, root := newTestFS(t, backing, nil)

	moviesEntry := lookupEntry(t, raw, 1, "movies")
	assert.Equal(t, stableInode("movies"), moviesEntry.Ino)

	moviesDir := childDir(t, &root.Inode, "movies")

	stream, errno := moviesDir.Readdir(context.Background())
	require.Equal(t, syscall.Errno(0), errno)
	defer stream.Close()

	readdirInos := map[string]uint64{}
	for stream.HasNext() {
		entry, errno := stream.Next()
		require.Equal(t, syscall.Errno(0), errno)
		readdirInos[entry.Name] = entry.Ino
	}
	require.Len(t, readdirInos, 3)

	for name, readdirIno := range readdirInos {
		entry := lookupEntry(t, raw, moviesEntry.NodeId, name)
		assert.Equal(t, readdirIno, entry.Ino, "Readdir/Lookup inode mismatch for %q", name)
		assert.Greater(t, entry.Ino, uint64(1), "%q must not use a reserved inode", name)
	}
}

// A retained node keeps its inode across lookups, and a later open reports the
// replacement size rather than the size captured at first lookup.
func TestRetainedNodeSeesReplacementSize(t *testing.T) {
	const name = "Dune.2021.mkv"
	path := filepath.Join("movies", name)

	backing := newFakeNzbFS()
	backing.addDir("movies")
	backing.addFile(path, 1000)

	tracker := &recordingTracker{}
	raw, root := newTestFS(t, backing, tracker)

	moviesEntry := lookupEntry(t, raw, 1, "movies")
	first := lookupEntry(t, raw, moviesEntry.NodeId, name)
	file := childFile(t, &childDir(t, &root.Inode, "movies").Inode, name)

	handle := openFile(t, file)
	require.Equal(t, []int64{1000}, tracker.recorded())
	require.Equal(t, syscall.Errno(0), handle.Release(context.Background()))

	// In-place replacement at the same virtual path.
	backing.replaceFile(path, 2000)

	second := lookupEntry(t, raw, moviesEntry.NodeId, name)
	assert.Equal(t, first.Ino, second.Ino, "inode must stay stable across a replacement")
	assert.Same(t, file, childFile(t, &childDir(t, &root.Inode, "movies").Inode, name),
		"the node must be retained, not rebuilt")

	handle2 := openFile(t, file)
	defer func() { _ = handle2.Release(context.Background()) }()

	assert.Equal(t, []int64{1000, 2000}, tracker.recorded(),
		"the new open must report the replacement size")
	assert.Equal(t, int64(2000), file.size.Load())
}

// An already-open handle stays pinned to the size it was opened with while a
// replacement changes what new opens see.
func TestExistingHandleUnaffectedByReplacement(t *testing.T) {
	const name = "Tenet.2020.mkv"
	path := filepath.Join("movies", name)

	backing := newFakeNzbFS()
	backing.addDir("movies")
	backing.addFile(path, 500)

	raw, root := newTestFS(t, backing, &recordingTracker{})

	moviesEntry := lookupEntry(t, raw, 1, "movies")
	moviesDir := childDir(t, &root.Inode, "movies")
	_ = lookupEntry(t, raw, moviesEntry.NodeId, name)
	file := childFile(t, &moviesDir.Inode, name)

	old := openFile(t, file)
	oldSnapshot, err := old.file.Stat()
	require.NoError(t, err)

	backing.replaceFile(path, 900)

	fresh := openFile(t, file)
	freshSnapshot, err := fresh.file.Stat()
	require.NoError(t, err)

	assert.Equal(t, int64(500), oldSnapshot.Size(), "existing handle must stay internally consistent")
	assert.Equal(t, int64(900), freshSnapshot.Size(), "new open must see the replacement size")

	require.Equal(t, syscall.Errno(0), old.Release(context.Background()))
	require.Equal(t, syscall.Errno(0), fresh.Release(context.Background()))
}

// Concurrent opens of a retained node while the backing size changes must not
// race on File.size (run under -race).
func TestConcurrentOpensNoRaceOnSize(t *testing.T) {
	const name = "Heat.1995.mkv"
	path := filepath.Join("movies", name)

	backing := newFakeNzbFS()
	backing.addDir("movies")
	backing.addFile(path, 1<<20)

	raw, root := newTestFS(t, backing, &recordingTracker{})

	moviesEntry := lookupEntry(t, raw, 1, "movies")
	_ = lookupEntry(t, raw, moviesEntry.NodeId, name)
	file := childFile(t, &childDir(t, &root.Inode, "movies").Inode, name)

	const workers = 16
	stop := make(chan struct{})

	var replacer sync.WaitGroup
	replacer.Add(1)
	go func() {
		defer replacer.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			backing.replaceFile(path, int64(1<<20)+int64(i%64))
		}
	}()

	var opens sync.WaitGroup
	opens.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer opens.Done()
			for j := 0; j < 50; j++ {
				fh, _, errno := file.Open(context.Background(), syscall.O_RDONLY)
				if errno != 0 {
					t.Errorf("Open failed: %v", errno)
					return
				}
				_ = file.size.Load()
				_ = fh.(*Handle).Release(context.Background())
			}
		}()
	}

	opens.Wait()
	close(stop)
	replacer.Wait()

	assert.GreaterOrEqual(t, file.size.Load(), int64(1<<20))
}
