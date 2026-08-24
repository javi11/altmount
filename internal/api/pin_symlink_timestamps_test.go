package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	_ "github.com/mattn/go-sqlite3"
)

func setupPinTimestampsFixture(t *testing.T) (*fiber.App, *database.HealthRepository, string) {
	t.Helper()

	dir := t.TempDir()
	db, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE file_health (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL UNIQUE,
			library_path TEXT,
			status TEXT NOT NULL,
			last_checked DATETIME,
			last_error TEXT,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			repair_retry_count INTEGER DEFAULT 0,
			max_repair_retries INTEGER DEFAULT 3,
			source_nzb_path TEXT,
			error_details TEXT,
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_date DATETIME,
			scheduled_check_at DATETIME,
			priority INTEGER DEFAULT 0,
			streaming_failure_count INTEGER DEFAULT 0,
			is_masked BOOLEAN DEFAULT FALSE,
			indexer TEXT DEFAULT NULL,
			download_id TEXT DEFAULT NULL
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	repo := database.NewHealthRepository(db, database.DialectSQLite)

	pin := "2020-01-01T01:00:00Z"
	cfg := config.DefaultConfig(dir)
	cfg.Import.PinSymlinkTimestamp = &pin

	server := &Server{
		configManager: config.NewManager(cfg, filepath.Join(dir, "config.yaml")),
		healthRepo:    repo,
	}

	app := fiber.New()
	app.Post("/health/pin-symlink-timestamps", server.handlePinLibrarySymlinkTimestamps)
	return app, repo, dir
}

func seedHealthRecord(t *testing.T, repo *database.HealthRepository, filePath, libraryPath string) {
	t.Helper()
	if err := repo.UpdateFileHealth(context.Background(), filePath, "healthy", nil, nil, nil, false); err != nil {
		t.Fatalf("seed health record %s: %v", filePath, err)
	}
	if libraryPath != "" {
		if err := repo.UpdateLibraryPath(context.Background(), filePath, libraryPath); err != nil {
			t.Fatalf("update library path: %v", err)
		}
	}
}

func TestPinLibrarySymlinkTimestampsOnlyTouchesSymlinks(t *testing.T) {
	app, repo, dir := setupPinTimestampsFixture(t)

	// 1. Real symlink -> pinned in place.
	symlinkTarget := filepath.Join(dir, "mount", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(symlinkTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(symlinkTarget, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	targetBefore, err := os.Stat(symlinkTarget)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "library", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(symlinkTarget, link); err != nil {
		t.Fatal(err)
	}
	seedHealthRecord(t, repo, symlinkTarget, link)

	// 2. Regular file at library path (ARR hardlink/copy import) -> untouched.
	hardSource := filepath.Join(dir, "mount", "hardlink.mkv")
	if err := os.WriteFile(hardSource, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hardFile := filepath.Join(dir, "library", "hardlink.mkv")
	if err := os.WriteFile(hardFile, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	hardBefore, err := os.Stat(hardFile)
	if err != nil {
		t.Fatal(err)
	}
	seedHealthRecord(t, repo, hardSource, hardFile)

	// 3. Missing library path -> skipped_missing, no error.
	seedHealthRecord(t, repo, filepath.Join(dir, "mount", "gone.mkv"), filepath.Join(dir, "library", "gone.mkv"))

	// 4. Record without stored LibraryPath -> skipped_no_path.
	seedHealthRecord(t, repo, filepath.Join(dir, "mount", "nopath.mkv"), "")

	resp, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/health/pin-symlink-timestamps", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Message           string   `json:"message"`
			Pinned            int      `json:"pinned"`
			SkippedNoPath     int      `json:"skipped_no_path"`
			SkippedMissing    int      `json:"skipped_missing"`
			SkippedNotSymlink int      `json:"skipped_not_symlink"`
			ErrorCount        int      `json:"error_count"`
			Errors            []string `json:"errors"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	if !out.Success {
		t.Fatalf("request failed: %+v", out)
	}
	if out.Data.Pinned != 1 || out.Data.SkippedNotSymlink != 1 ||
		out.Data.SkippedMissing != 1 || out.Data.SkippedNoPath != 1 || out.Data.ErrorCount != 0 {
		t.Fatalf("unexpected summary: %+v", out.Data)
	}
	if !strings.Contains(out.Data.Message, "Pinned 1 of 4") {
		t.Fatalf("message = %q", out.Data.Message)
	}

	// Symlink still a symlink, mtime pinned to the configured value.
	li, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatal("library entry is no longer a symlink")
	}
	want, _ := time.Parse(time.RFC3339, "2020-01-01T01:00:00Z")
	if !li.ModTime().Equal(want) {
		t.Fatalf("symlink mtime = %v, want %v", li.ModTime(), want)
	}

	// Target file mtime untouched by the pin operation.
	targetAfter, err := os.Stat(symlinkTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !targetAfter.ModTime().Equal(targetBefore.ModTime()) {
		t.Fatalf("target mtime changed: %v -> %v", targetBefore.ModTime(), targetAfter.ModTime())
	}

	// Regular file at library path untouched (content and mtime).
	content, err := os.ReadFile(hardFile)
	if err != nil || string(content) != "y" {
		t.Fatalf("regular file modified: %v %q", err, content)
	}
	hardAfter, err := os.Stat(hardFile)
	if err != nil {
		t.Fatal(err)
	}
	if !hardAfter.ModTime().Equal(hardBefore.ModTime()) {
		t.Fatalf("regular file mtime changed: %v -> %v", hardBefore.ModTime(), hardAfter.ModTime())
	}
}

func TestPinLibrarySymlinkTimestampsRequiresConfig(t *testing.T) {
	app, _, dir := setupPinTimestampsFixture(t)
	_ = dir

	// Overwrite the manager with a config that has no pinned date.
	cfg := config.DefaultConfig(t.TempDir())
	app2 := fiber.New()
	server := &Server{
		configManager: config.NewManager(cfg, filepath.Join(t.TempDir(), "config.yaml")),
		healthRepo:    nil,
	}
	app2.Post("/health/pin-symlink-timestamps", server.handlePinLibrarySymlinkTimestamps)

	_ = app
	resp, err := app2.Test(httptest.NewRequest(fiber.MethodPost, "/health/pin-symlink-timestamps", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
