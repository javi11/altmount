package rclonecli

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
)

// TestPerformMount_SendsDirCacheTimeAndCacheMinFreeSpace covers two settings
// that config.sample.yaml documents, that GetDefaultConfig fills in, and that
// never reached rclone: they were absent from the vfsOpt map performMount
// builds, so setting them had no effect and produced no error either.
func TestPerformMount_SendsDirCacheTimeAndCacheMinFreeSpace(t *testing.T) {
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding mount/mount body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	cfg := &config.Config{}
	cfg.RClone.VFSCacheMode = "full"
	cfg.RClone.DirCacheTime = "10m"
	cfg.RClone.VFSCacheMinFreeSpace = "1G"

	ready := make(chan struct{})
	close(ready)

	m := &Manager{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:           context.Background(),
		cfg:           config.NewManager(cfg, ""),
		rcPort:        u.Port(),
		mounts:        make(map[string]*MountInfo),
		serverReady:   ready,
		serverStarted: true,
		httpClient:    &http.Client{Timeout: 5 * time.Second},
	}

	if err := m.performMount(context.Background(), "altmount", t.TempDir(), "http://localhost:8080/webdav"); err != nil {
		t.Fatalf("performMount: %v", err)
	}

	vfsOpt, ok := body["vfsOpt"].(map[string]any)
	if !ok {
		t.Fatalf("mount/mount carried no vfsOpt block. body=%v", body)
	}

	// 10m expressed the way rclone's Duration type reports it.
	if got, want := vfsOpt["DirCacheTime"], float64(10*time.Minute); got != want {
		t.Errorf("vfsOpt.DirCacheTime = %v, want %v (10m in nanoseconds)", got, want)
	}

	if got, want := vfsOpt["CacheMinFreeSpace"], "1G"; got != want {
		t.Errorf("vfsOpt.CacheMinFreeSpace = %v, want %q", got, want)
	}
}
