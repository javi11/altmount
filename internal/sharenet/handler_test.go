package sharenet_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

// Valid 64-hex release hashes (extractHash now enforces the format).
const (
	hHash       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hUnknownHsh = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func setupHandlerStore(t *testing.T) *sharenet.ReleaseStore {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "MyShow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "ep01.meta"), []byte("meta-0"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "ep02.meta"), []byte("meta-1"), 0o644)

	store := sharenet.NewReleaseStore(root)
	store.Register(hHash, []string{"MyShow/ep01", "MyShow/ep02"})
	return store
}

func TestHandler_ServeManifest(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/share/manifest/"+hHash, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var m sharenet.Manifest
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(m.Metas) != 2 || m.Metas[0].VirtualPath != "MyShow/ep01" || m.Metas[1].VirtualPath != "MyShow/ep02" {
		t.Fatalf("unexpected manifest: %+v", m.Metas)
	}
}

func TestHandler_ServeMetaByIndex(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)

	for i, want := range []string{"meta-0", "meta-1"} {
		req := httptest.NewRequest(http.MethodGet, "/api/share/meta/"+hHash+"/"+strconv.Itoa(i), nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("index %d: expected 200, got %d", i, w.Code)
		}
		body, _ := io.ReadAll(w.Body)
		if string(body) != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, body)
		}
	}
}

func TestHandler_ManifestUnknownHash_404(t *testing.T) {
	h := sharenet.NewHandler(sharenet.NewReleaseStore(t.TempDir()))
	req := httptest.NewRequest(http.MethodGet, "/api/share/manifest/"+hUnknownHsh, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_MetaOutOfRange_404(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/share/meta/"+hHash+"/9", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_MetaBadIndex_400(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/share/meta/"+hHash+"/notanumber", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_MetaMissingIndex_400(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/share/meta/"+hHash, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_InvalidHashFormat_400(t *testing.T) {
	h := sharenet.NewHandler(sharenet.NewReleaseStore(t.TempDir()))
	for _, bad := range []string{"short", "NOTLOWERHEX" + hHash[11:], hHash + "extra"} {
		req := httptest.NewRequest(http.MethodGet, "/api/share/manifest/"+bad, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("hash %q: expected 400, got %d", bad, w.Code)
		}
	}
}

func TestHandler_ManifestMissingHash_400(t *testing.T) {
	h := sharenet.NewHandler(sharenet.NewReleaseStore(t.TempDir()))
	req := httptest.NewRequest(http.MethodGet, "/api/share/manifest/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

