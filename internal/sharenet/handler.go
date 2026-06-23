package sharenet

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Handler is a standard http.Handler that serves a release's manifest and its
// per-file v3 .meta bytes to peers. Mount it in Fiber using adaptor.HTTPHandler:
//
//	app.All("/api/share/*", adaptor.HTTPHandler(sharenet.NewHandler(store)))
//
// Endpoints:
//
//	GET /api/share/manifest/{hash}     → JSON {"metas":[{"virtual_path":...}, ...]}
//	GET /api/share/meta/{hash}/{index} → raw v3 .meta bytes for metas[index]
type Handler struct {
	store *ReleaseStore
	mux   *http.ServeMux
}

// Manifest is the JSON returned by GET /api/share/manifest/{hash}.
type Manifest struct {
	Metas []ManifestEntry `json:"metas"`
}

// ManifestEntry describes one shareable .meta within a release.
type ManifestEntry struct {
	VirtualPath string `json:"virtual_path"`
}

// NewHandler creates a Handler backed by store.
func NewHandler(store *ReleaseStore) *Handler {
	h := &Handler{store: store, mux: http.NewServeMux()}
	h.mux.HandleFunc("/api/share/manifest/", h.serveManifest)
	h.mux.HandleFunc("/api/share/meta/", h.serveMeta)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) serveManifest(w http.ResponseWriter, r *http.Request) {
	hash := extractHash(strings.TrimPrefix(r.URL.Path, "/api/share/manifest/"))
	if hash == "" {
		http.Error(w, "missing release hash", http.StatusBadRequest)
		return
	}
	paths, ok := h.store.Paths(hash)
	if !ok {
		http.NotFound(w, r)
		return
	}
	manifest := Manifest{Metas: make([]ManifestEntry, len(paths))}
	for i, p := range paths {
		manifest.Metas[i] = ManifestEntry{VirtualPath: p}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(manifest)
}

// serveMeta handles GET /api/share/meta/{hash}/{index}.
func (h *Handler) serveMeta(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/share/meta/")
	hashPart, idxPart, found := strings.Cut(rest, "/")
	hash := extractHash(hashPart)
	if hash == "" || !found {
		http.Error(w, "expected /meta/{hash}/{index}", http.StatusBadRequest)
		return
	}
	index, err := strconv.Atoi(idxPart)
	if err != nil {
		http.Error(w, "invalid meta index", http.StatusBadRequest)
		return
	}
	data, err := h.store.ReadMeta(hash, index)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

// extractHash rejects empty hashes and path-traversal attempts.
func extractHash(hash string) string {
	if hash == "" || strings.ContainsAny(hash, "/\\") || strings.Contains(hash, "..") {
		return ""
	}
	return hash
}
