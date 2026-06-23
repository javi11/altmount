package sharenet_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

func setupHandlerStore(t *testing.T) *sharenet.ReleaseStore {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "MyShow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "ep01.meta"), []byte("meta-content"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "ep01.seg"), []byte("seg-content"), 0o644)

	store := sharenet.NewReleaseStore(root)
	store.Register("abc123", "MyShow/ep01")
	return store
}

func TestHandler_ServeMeta(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/share/meta/abc123", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "meta-content" {
		t.Fatalf("expected meta-content, got %q", string(body))
	}
}

func TestHandler_ServeSeg(t *testing.T) {
	store := setupHandlerStore(t)
	h := sharenet.NewHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/share/seg/abc123", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "seg-content" {
		t.Fatalf("expected seg-content, got %q", string(body))
	}
}

func TestHandler_UnknownHash_Returns404(t *testing.T) {
	store := sharenet.NewReleaseStore(t.TempDir())
	h := sharenet.NewHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/share/meta/doesnotexist", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_MissingHash_Returns400(t *testing.T) {
	store := sharenet.NewReleaseStore(t.TempDir())
	h := sharenet.NewHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/share/meta/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
