package sharenet

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler is a standard http.Handler that serves .meta and .seg files to peers.
// Mount it in Fiber using adaptor.HTTPHandler:
//
//	app.All("/api/share/*", adaptor.HTTPHandler(sharenet.NewHandler(store)))
type Handler struct {
	store *ReleaseStore
	mux   *http.ServeMux
}

// NewHandler creates a Handler backed by store.
func NewHandler(store *ReleaseStore) *Handler {
	h := &Handler{store: store, mux: http.NewServeMux()}
	h.mux.HandleFunc("/api/share/info/", h.serveInfo)
	h.mux.HandleFunc("/api/share/meta/", h.serveMeta)
	h.mux.HandleFunc("/api/share/seg/", h.serveSeg)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) serveInfo(w http.ResponseWriter, r *http.Request) {
	hash := extractHash(r.URL.Path, "/api/share/info/")
	if hash == "" {
		http.Error(w, "missing release hash", http.StatusBadRequest)
		return
	}
	virtualPath, ok := h.store.Lookup(hash)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"virtual_path": virtualPath})
}

func (h *Handler) serveMeta(w http.ResponseWriter, r *http.Request) {
	hash := extractHash(r.URL.Path, "/api/share/meta/")
	if hash == "" {
		http.Error(w, "missing release hash", http.StatusBadRequest)
		return
	}
	data, err := h.store.ReadMeta(hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

func (h *Handler) serveSeg(w http.ResponseWriter, r *http.Request) {
	hash := extractHash(r.URL.Path, "/api/share/seg/")
	if hash == "" {
		http.Error(w, "missing release hash", http.StatusBadRequest)
		return
	}
	data, err := h.store.ReadSeg(hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

func extractHash(path, prefix string) string {
	hash := strings.TrimPrefix(path, prefix)
	// Reject traversal attempts and empty hashes.
	if hash == "" || strings.ContainsAny(hash, "/\\") || strings.Contains(hash, "..") {
		return ""
	}
	return hash
}
