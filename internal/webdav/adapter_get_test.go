package webdav

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// failingFile serves size bytes of zeros but fails with failErr once failAt
// bytes have been read, the way a usenet reader does when an article fetch
// gives up mid-stream.
type failingFile struct {
	size    int64
	pos     int64
	failAt  int64
	failErr error
}

func (f *failingFile) Read(p []byte) (int, error) {
	if f.failErr != nil && f.pos >= f.failAt {
		return 0, f.failErr
	}
	remaining := f.size - f.pos
	if f.failErr != nil {
		remaining = min(remaining, f.failAt-f.pos)
	}
	if remaining <= 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), remaining))
	clear(p[:n])
	f.pos += int64(n)
	return n, nil
}

func (f *failingFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.pos = f.size + offset
	}
	return f.pos, nil
}

func (f *failingFile) Close() error                       { return nil }
func (f *failingFile) Write([]byte) (int, error)          { return 0, errors.New("read-only") }
func (f *failingFile) Readdir(int) ([]os.FileInfo, error) { return nil, errors.New("not a dir") }
func (f *failingFile) Stat() (os.FileInfo, error)         { return fileStat{size: f.size}, nil }

type fileStat struct{ size int64 }

func (s fileStat) Name() string       { return "movie.mkv" }
func (s fileStat) Size() int64        { return s.size }
func (s fileStat) Mode() os.FileMode  { return 0o644 }
func (s fileStat) ModTime() time.Time { return time.Unix(0, 0) }
func (s fileStat) IsDir() bool        { return false }
func (s fileStat) Sys() any           { return nil }

type singleFileFS struct{ file *failingFile }

func (s singleFileFS) Mkdir(context.Context, string, os.FileMode) error { return errors.New("ro") }
func (s singleFileFS) RemoveAll(context.Context, string) error          { return errors.New("ro") }
func (s singleFileFS) Rename(context.Context, string, string) error     { return errors.New("ro") }
func (s singleFileFS) Stat(context.Context, string) (os.FileInfo, error) {
	return fileStat{size: s.file.size}, nil
}
func (s singleFileFS) OpenFile(context.Context, string, int, os.FileMode) (File, error) {
	return s.file, nil
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func serveGet(t *testing.T, file *failingFile, rangeHeader string) *httptest.ResponseRecorder {
	t.Helper()
	methods := &webdavMethods{fs: singleFileFS{file: file}, prefix: "/webdav/"}
	req := httptest.NewRequest(http.MethodGet, "/webdav/bench/movie.mkv", nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	rec := httptest.NewRecorder()
	methods.handleGet(rec, req)
	return rec
}

func TestHandleGetWarnsWhenStreamEndsOnReadError(t *testing.T) {
	logs := captureLogs(t)
	fetchErr := errors.New("nntp: all providers exhausted")
	file := &failingFile{size: 4 << 20, failAt: 1 << 20, failErr: fetchErr}

	rec := serveGet(t, file, "bytes=0-")

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Body.Len(); got != 1<<20 {
		t.Fatalf("body bytes = %d, want the 1 MiB served before the failure", got)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "stream ended on read error") {
		t.Fatalf("want a WARN that the stream ended on a read error, got logs:\n%s", out)
	}
	for _, want := range []string{"path=bench/movie.mkv", "bytes_served=1048576", "all providers exhausted"} {
		if !strings.Contains(out, want) {
			t.Errorf("WARN is missing %q:\n%s", want, out)
		}
	}
}

func TestHandleGetDoesNotWarnOnCleanRead(t *testing.T) {
	logs := captureLogs(t)
	file := &failingFile{size: 2 << 20}

	rec := serveGet(t, file, "bytes=0-")

	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 2<<20 {
		t.Fatalf("status = %d body = %d, want 206 with the whole file", rec.Code, rec.Body.Len())
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("unexpected WARN on a clean read:\n%s", logs.String())
	}
}
