package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/health"
	"github.com/kipsilabs/altmount/internal/metadata"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
)

// The Library Audit endpoints: a dry-run scan listing orphans and dead
// symlinks, and a cleanup that deletes the selected ones.
func TestLibraryAuditEndpoints(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "media")
	mountDir := filepath.Join(root, "altmount")
	require.NoError(t, os.MkdirAll(libDir, 0o755))

	db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: filepath.Join(root, "audit.db")})
	require.NoError(t, err)
	defer db.Close()

	meta := metadata.NewMetadataService(filepath.Join(root, "metadata"))
	cfg := config.DefaultConfig()
	cfg.Metadata.RootPath = filepath.Join(root, "metadata")
	cfg.MountPath = mountDir
	cfg.Health.LibraryDir = &libDir
	mgr := config.NewManager(cfg, "")
	worker := health.NewLibrarySyncWorker(meta, database.NewHealthRepository(db.Connection(), db.Dialect()), mgr.GetConfig, mgr, nil)

	for _, rel := range []string{"Movies/Linked/linked.mkv", "Movies/Orphan/orphan.mkv"} {
		fm := meta.CreateFileMetadata(500, "x.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY, nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
		fm.CreatedAt = time.Now().Add(-72 * time.Hour).Unix()
		require.NoError(t, meta.WriteFileMetadata(rel, fm))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "Movies"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(mountDir, "Movies/Linked/linked.mkv"), filepath.Join(libDir, "Movies/linked.mkv")))
	dead := filepath.Join(libDir, "Movies/dead.mkv")
	require.NoError(t, os.Symlink(filepath.Join(mountDir, "Movies/Gone/gone.mkv"), dead))

	s := &Server{librarySyncWorker: worker, configManager: &mockConfigManager{cfg: cfg}}
	app := fiber.New()
	app.Post("/scan", s.handleLibraryAuditScan)
	app.Post("/cleanup", s.handleLibraryAuditCleanup)

	post := func(path string, body any) (int, map[string]any) {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			require.NoError(t, json.NewEncoder(&buf).Encode(body))
		}
		req := httptest.NewRequest(http.MethodPost, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return resp.StatusCode, out
	}

	code, out := post("/scan", nil)
	require.Equal(t, http.StatusOK, code)
	data := out["data"].(map[string]any)
	orphans := data["orphans"].([]any)
	require.Len(t, orphans, 1)
	assert.Equal(t, "Movies/Orphan/orphan.mkv", orphans[0].(map[string]any)["path"])
	deadLinks := data["dead_symlinks"].([]any)
	require.Len(t, deadLinks, 1)
	assert.Equal(t, dead, deadLinks[0].(map[string]any)["path"])

	code, _ = post("/cleanup", LibraryAuditRequest{})
	assert.Equal(t, http.StatusBadRequest, code, "an empty selection is rejected")

	code, out = post("/cleanup", LibraryAuditRequest{Orphans: []string{"Movies/Orphan/orphan.mkv"}, DeadSymlinks: []string{dead}})
	require.Equal(t, http.StatusOK, code)
	res := out["data"].(map[string]any)
	assert.EqualValues(t, 1, res["orphans_deleted"])
	assert.EqualValues(t, 1, res["dead_symlinks_removed"])
	assert.False(t, meta.FileExists("Movies/Orphan/orphan.mkv"))
	assert.True(t, meta.FileExists("Movies/Linked/linked.mkv"))
}

func TestLibraryAuditRequestGrace(t *testing.T) {
	zero, two := 0.0, 2.0
	assert.Equal(t, health.DefaultAuditGracePeriod, LibraryAuditRequest{}.grace())
	assert.Equal(t, time.Duration(0), LibraryAuditRequest{GraceHours: &zero}.grace())
	assert.Equal(t, 2*time.Hour, LibraryAuditRequest{GraceHours: &two}.grace())
}
