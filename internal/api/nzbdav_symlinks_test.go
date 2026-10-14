package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise the handler, real migration lookup, and filesystem together: a file:
// row may contain nzbdav's filename rather than the filename AltMount imported.
func TestMigrateNzbdavSymlinksValidatesTargets(t *testing.T) {
	for _, rcloneLink := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("rclonelink=%t/dry_run=%t", rcloneLink, dryRun), func(t *testing.T) {
				// Database migrations configure goose's global filesystem and dialect,
				// so initialize these databases sequentially.
				library, mount := t.TempDir(), t.TempDir()
				db, err := database.NewDB(database.Config{DatabasePath: filepath.Join(t.TempDir(), "test.db")})
				require.NoError(t, err)
				t.Cleanup(func() { db.Close() })
				finalPath := "/movies/Release"
				releaseDir := filepath.Join(mount, "movies", "Release")
				require.NoError(t, os.MkdirAll(releaseDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(releaseDir, "existing.mkv"), []byte("video"), 0o644))
				// A renamed file must not cause a guess at a replacement target.
				require.NoError(t, os.WriteFile(filepath.Join(releaseDir, "renamed (2).mkv"), []byte("video"), 0o644))
				require.NoError(t, os.Symlink("absent.mkv", filepath.Join(releaseDir, "dangling.mkv")))
				require.NoError(t, os.Symlink("loop.mkv", filepath.Join(releaseDir, "loop.mkv")))
				links := make(map[string]string)
				for _, name := range []string{"existing", "renamed", "dangling", "loop"} {
					_, err := db.MigrationRepo.Upsert(context.Background(), &database.ImportMigration{
						Source: "nzbdav", ExternalID: strings.ToUpper(name),
						RelativePath: "file:" + name + ".mkv", FinalPath: &finalPath,
						Status: database.ImportMigrationStatusImported,
					})
					require.NoError(t, err)
					link := filepath.Join(library, name+".mkv")
					original := "/mnt/nzbdav/.ids/" + name
					if rcloneLink {
						link += ".rclonelink"
						require.NoError(t, os.WriteFile(link, []byte(original+"\n"), 0o644))
					} else {
						require.NoError(t, os.Symlink(original, link))
					}
					links[name] = link
				}
				server := &Server{configManager: config.NewManager(&config.Config{MountPath: mount}, ""), migrationRepo: db.MigrationRepo}
				app := fiber.New()
				app.Post("/migrate", server.handleMigrateNzbdavSymlinks)
				body, err := json.Marshal(map[string]any{
					"library_path": library, "source_mount_path": "/mnt/nzbdav", "dry_run": dryRun,
				})
				require.NoError(t, err)
				req := httptest.NewRequest(http.MethodPost, "/migrate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				require.NoError(t, err)
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				var result struct {
					Data struct {
						Scanned, Matched, Rewritten int
						MissingTargets              []string `json:"missing_targets"`
						Errors                      []string
					}
				}
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
				assert.Equal(t, 4, result.Data.Scanned)
				assert.Equal(t, 1, result.Data.Matched)
				wantRewritten := 1
				if dryRun {
					wantRewritten = 0
				}
				assert.Equal(t, wantRewritten, result.Data.Rewritten)
				assert.Len(t, result.Data.MissingTargets, 2)
				for _, name := range []string{"renamed", "dangling"} {
					assert.Contains(t, strings.Join(result.Data.MissingTargets, "\n"), links[name])
					assert.Contains(t, strings.Join(result.Data.MissingTargets, "\n"), filepath.Join(releaseDir, name+".mkv"))
				}
				assert.Len(t, result.Data.Errors, 1)
				assert.Contains(t, strings.Join(result.Data.Errors, "\n"), links["loop"])
				for name, link := range links {
					want := "/mnt/nzbdav/.ids/" + name
					if name == "existing" && !dryRun {
						want = filepath.Join(releaseDir, "existing.mkv")
					}
					if rcloneLink {
						content, err := os.ReadFile(link)
						require.NoError(t, err)
						if name != "existing" || dryRun {
							want += "\n"
						}
						assert.Equal(t, want, string(content))
					} else {
						target, err := os.Readlink(link)
						require.NoError(t, err)
						assert.Equal(t, want, target)
					}
					_, err := os.Lstat(link + ".new")
					assert.True(t, os.IsNotExist(err), "temporary link should not remain")
				}
			})
		}
	}
}
