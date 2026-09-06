package propfind

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeInfo struct {
	name string
	dir  bool
}

func (f fakeInfo) Name() string { return f.name }
func (f fakeInfo) Size() int64  { return 0 }
func (f fakeInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (f fakeInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

type fakeFile struct{ children []os.FileInfo }

func (f *fakeFile) Close() error { return nil }
func (f *fakeFile) Readdir(int) ([]os.FileInfo, error) {
	out := f.children
	f.children = nil
	return out, nil
}

type fakeFS struct{}

func (fakeFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	switch name {
	case "/", "/root":
		return fakeInfo{name: "root", dir: true}, nil
	case "/root/sub":
		return fakeInfo{name: "sub", dir: true}, nil
	}
	return nil, os.ErrNotExist
}

func (fakeFS) OpenFile(_ context.Context, name string, _ int, _ os.FileMode) (FSFile, error) {
	return &fakeFile{children: []os.FileInfo{fakeInfo{name: "sub", dir: true}, fakeInfo{name: "a.mkv"}}}, nil
}

// Clients commonly match the bare `<D:collection/>` form; the namespace is already
// declared on the multistatus root, so repeating it there only breaks them.
func TestPropfindCollectionElementIsBare(t *testing.T) {
	req := httptest.NewRequest("PROPFIND", "/webdav/root", nil)
	req.Header.Set("Depth", "1")
	rec := httptest.NewRecorder()

	status, err := HandlePropfind(fakeFS{}, rec, req, "/webdav")
	if status != 0 || err != nil {
		t.Fatalf("HandlePropfind: status=%d err=%v", status, err)
	}
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `xmlns:D="DAV:"`) {
		t.Fatalf("multistatus root must declare xmlns:D, got: %s", body)
	}
	if strings.Contains(body, `<D:collection xmlns`) {
		t.Fatalf("collection element must not redeclare the namespace, got: %s", body)
	}
	if strings.Count(body, `<D:collection/>`) != 2 {
		t.Fatalf("expected two bare collection elements (root + sub), got: %s", body)
	}
}
