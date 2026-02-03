package webdav

import (
	"context"
	"os"
	"sync/atomic"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/utils"
	"golang.org/x/net/webdav"
)

type monitoredFileSystem struct {
	fs webdav.FileSystem
}

func (m *monitoredFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return m.fs.Mkdir(ctx, name, perm)
}

func (m *monitoredFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	f, err := m.fs.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}

	// Check if this is a monitored stream
	if streamVal := ctx.Value(utils.ActiveStreamKey); streamVal != nil {
		if stream, ok := streamVal.(*nzbfilesystem.ActiveStream); ok {
			// Update total size if available
			if stat, err := f.Stat(); err == nil {
				atomic.StoreInt64(&stream.TotalSize, stat.Size())
			}
			return &monitoredFile{File: f, stream: stream, ctx: ctx}, nil
		}
	}

	return f, nil
}

func (m *monitoredFileSystem) RemoveAll(ctx context.Context, name string) error {
	return m.fs.RemoveAll(ctx, name)
}

func (m *monitoredFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	return m.fs.Rename(ctx, oldName, newName)
}

func (m *monitoredFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return m.fs.Stat(ctx, name)
}

type monitoredFile struct {
	webdav.File
	stream *nzbfilesystem.ActiveStream
	ctx    context.Context
}

func (m *monitoredFile) Read(p []byte) (n int, err error) {
	if err := m.ctx.Err(); err != nil {
		return 0, err
	}
	n, err = m.File.Read(p)
	if n > 0 {
		atomic.AddInt64(&m.stream.BytesSent, int64(n))
		atomic.AddInt64(&m.stream.CurrentOffset, int64(n))
	}
	return n, err
}

func (m *monitoredFile) Seek(offset int64, whence int) (int64, error) {
	if err := m.ctx.Err(); err != nil {
		return 0, err
	}
	newOffset, err := m.File.Seek(offset, whence)
	if err == nil {
		atomic.StoreInt64(&m.stream.CurrentOffset, newOffset)
	}
	return newOffset, err
}

func (m *monitoredFile) Write(p []byte) (n int, err error) {
	if err := m.ctx.Err(); err != nil {
		return 0, err
	}
	return m.File.Write(p)
}

func (m *monitoredFile) Close() error {
	return m.File.Close()
}
