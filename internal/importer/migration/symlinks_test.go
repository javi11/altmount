package migration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/importer/migration"
)

// mockLookup implements SymlinkLookup for testing.
type mockLookup struct {
	paths map[string]string // guid → finalPath; absent means not found
	err   error             // if non-nil, always return this error
}

func (m *mockLookup) LookupFinalPath(_ context.Context, _, externalID string) (string, bool, error) {
	if m.err != nil {
		return "", false, m.err
	}
	fp, ok := m.paths[externalID]
	return fp, ok, nil
}

func (m *mockLookup) MarkSymlinksMigrated(_ context.Context, _ []int64) error {
	return nil
}

func TestRewriteLibrarySymlinks(t *testing.T) {
	t.Parallel()

	const (
		sourceMountPath = "/mnt/nzbdav"
		altmountPath    = "/mnt/altmount"
		source          = "nzbdav"
	)

	tests := []struct {
		name                   string
		setup                  func(t *testing.T, dir string) // create symlinks inside dir
		lookup                 *mockLookup
		dryRun                 bool
		wantScanned            int
		wantMatched            int
		wantRewritten          int
		wantUnmatched          int
		wantErrors             int
		wantSkippedWrongPrefix int
		// optional post-check on filesystem state
		postCheck func(t *testing.T, dir string)
	}{
		{
			name: "match and rewrite",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				// Create a symlink pointing to sourceMountPath/.ids/abc123
				target := sourceMountPath + "/.ids/abc123"
				link := filepath.Join(dir, "movie.mkv")
				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup: &mockLookup{
				paths: map[string]string{
					// GUID is normalised to uppercase before lookup.
					"ABC123": "/movies/Movie (2020)/Movie (2020).mkv",
				},
			},
			dryRun:        false,
			wantScanned:   1,
			wantMatched:   1,
			wantRewritten: 1,
			postCheck: func(t *testing.T, dir string) {
				t.Helper()
				link := filepath.Join(dir, "movie.mkv")
				got, err := os.Readlink(link)
				if err != nil {
					t.Fatalf("readlink after rewrite: %v", err)
				}
				want := filepath.Join(altmountPath, "movies/Movie (2020)/Movie (2020).mkv")
				if got != want {
					t.Errorf("symlink target: got %q, want %q", got, want)
				}
			},
		},
		{
			name: "unmatched guid",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				target := sourceMountPath + "/.ids/unknown-guid"
				if err := os.Symlink(target, filepath.Join(dir, "unknown.mkv")); err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup:        &mockLookup{paths: map[string]string{}},
			wantScanned:   1,
			wantMatched:   0,
			wantRewritten: 0,
			wantUnmatched: 1,
		},
		{
			name: "non-nzbdav symlink skipped",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				// Target doesn't contain sourceMountPath/.ids/
				if err := os.Symlink("/some/other/path/file.mkv", filepath.Join(dir, "other.mkv")); err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup:                 &mockLookup{paths: map[string]string{}},
			wantScanned:            1,
			wantMatched:             0,
			wantRewritten:           0,
			wantUnmatched:           0,
			wantSkippedWrongPrefix: 1,
		},
		{
			name: "dry run - no filesystem change",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				target := sourceMountPath + "/.ids/dryrun-guid"
				link := filepath.Join(dir, "dry.mkv")
				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup: &mockLookup{
				paths: map[string]string{
					"DRYRUN-GUID": "/movies/Dry/Dry.mkv",
				},
			},
			dryRun:        true,
			wantScanned:   1,
			wantMatched:   1,
			wantRewritten: 0,
			postCheck: func(t *testing.T, dir string) {
				t.Helper()
				link := filepath.Join(dir, "dry.mkv")
				got, err := os.Readlink(link)
				if err != nil {
					t.Fatalf("readlink after dry run: %v", err)
				}
				// Target must remain unchanged.
				want := sourceMountPath + "/.ids/dryrun-guid"
				if got != want {
					t.Errorf("dry run: symlink target changed: got %q, want %q", got, want)
				}
			},
		},
		{
			name: "rclonelink match and rewrite",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				// Simulate rclone .rclonelink file: plain text containing the symlink target.
				// nzbdav .ids/ paths use lowercase UUIDs; the lookup normalises to uppercase.
				content := sourceMountPath + "/.ids/8/c/0/9/b/8c09b35b-2868-4fb0-9ce3-35e6abbca785"
				err := os.WriteFile(filepath.Join(dir, "episode.mkv.rclonelink"), []byte(content), 0o644)
				if err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup: &mockLookup{
				paths: map[string]string{
					// Key must be uppercase — that's how import_migrations stores the DavItem ID.
					"8C09B35B-2868-4FB0-9CE3-35E6ABBCA785": "/tv/Show S01/Show.S01E01.mkv",
				},
			},
			dryRun:        false,
			wantScanned:   1,
			wantMatched:   1,
			wantRewritten: 1,
			postCheck: func(t *testing.T, dir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(dir, "episode.mkv.rclonelink"))
				if err != nil {
					t.Fatalf("readfile after rewrite: %v", err)
				}
				want := filepath.Join(altmountPath, "tv/Show S01/Show.S01E01.mkv")
				if string(content) != want {
					t.Errorf("rclonelink content: got %q, want %q", string(content), want)
				}
			},
		},
		{
			name: "rclonelink dry run - no filesystem change",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				// Use a realistic lowercase UUID as nzbdav .ids/ paths contain.
				content := sourceMountPath + "/.ids/d/r/y/r/c/dryrc1one-0000-4fb0-9ce3-35e6abbca785"
				err := os.WriteFile(filepath.Join(dir, "movie.mkv.rclonelink"), []byte(content), 0o644)
				if err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup: &mockLookup{
				paths: map[string]string{
					"DRYRC1ONE-0000-4FB0-9CE3-35E6ABBCA785": "/movies/Dry/Dry.mkv",
				},
			},
			dryRun:        true,
			wantScanned:   1,
			wantMatched:   1,
			wantRewritten: 0,
			postCheck: func(t *testing.T, dir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(dir, "movie.mkv.rclonelink"))
				if err != nil {
					t.Fatalf("readfile after dry run: %v", err)
				}
				want := sourceMountPath + "/.ids/d/r/y/r/c/dryrc1one-0000-4fb0-9ce3-35e6abbca785"
				if string(content) != want {
					t.Errorf("dry run: rclonelink changed: got %q, want %q", string(content), want)
				}
			},
		},
		{
			name:  "context cancellation stops walk",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				// Create one symlink so the walk has an entry to process.
				target := sourceMountPath + "/.ids/guid-cancel"
				if err := os.Symlink(target, filepath.Join(dir, "file")); err != nil {
					t.Fatalf("setup: %v", err)
				}
			},
			lookup: &mockLookup{paths: map[string]string{"guid-cancel": "/movies/x.mkv"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tc.setup(t, dir)

			ctx := context.Background()

			if tc.name == "context cancellation stops walk" {
				// Cancel context immediately to verify walk stops.
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}

			report, err := migration.RewriteLibrarySymlinks(
				ctx,
				dir,
				sourceMountPath,
				altmountPath,
				source,
				tc.lookup,
				tc.dryRun,
			)

			if tc.name == "context cancellation stops walk" {
				// We just verify it returns a context error and doesn't panic.
				if err == nil || !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled error, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if report.Scanned != tc.wantScanned {
				t.Errorf("Scanned: got %d, want %d", report.Scanned, tc.wantScanned)
			}
			if report.Matched != tc.wantMatched {
				t.Errorf("Matched: got %d, want %d", report.Matched, tc.wantMatched)
			}
			if report.Rewritten != tc.wantRewritten {
				t.Errorf("Rewritten: got %d, want %d", report.Rewritten, tc.wantRewritten)
			}
			if len(report.Unmatched) != tc.wantUnmatched {
				t.Errorf("Unmatched: got %d, want %d (entries: %v)", len(report.Unmatched), tc.wantUnmatched, report.Unmatched)
			}
			if len(report.Errors) != tc.wantErrors {
				t.Errorf("Errors: got %d, want %d (entries: %v)", len(report.Errors), tc.wantErrors, report.Errors)
			}
			if report.SkippedWrongPrefix != tc.wantSkippedWrongPrefix {
				t.Errorf("SkippedWrongPrefix: got %d, want %d", report.SkippedWrongPrefix, tc.wantSkippedWrongPrefix)
			}

			if tc.postCheck != nil {
				tc.postCheck(t, dir)
			}
		})
	}
}
