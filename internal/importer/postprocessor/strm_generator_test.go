package postprocessor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *database.UserRepository {
	db, err := sql.Open("sqlite3", "file::memory:")
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL UNIQUE,
			email TEXT,
			name TEXT,
			avatar_url TEXT,
			provider TEXT NOT NULL,
			provider_id TEXT,
			password_hash TEXT,
			api_key TEXT,
			is_admin BOOLEAN DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_login DATETIME
		);
	`)
	require.NoError(t, err)

	return database.NewUserRepository(db)
}

func TestCreateStrmFiles_HostConfiguration(t *testing.T) {
	// Setup temporary directories
	tmpDir := t.TempDir()
	metadataDir := filepath.Join(tmpDir, "metadata")
	importDir := filepath.Join(tmpDir, "import")

	err := os.MkdirAll(metadataDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(importDir, 0755)
	require.NoError(t, err)

	// Setup Database and User
	userRepo := setupTestDB(t)
	ctx := context.Background()

	apiKey := "test-api-key"
	adminUser := &database.User{
		UserID:    "admin",
		Name:      nil, // Using pointer
		Provider:  "local",
		APIKey:    &apiKey,
		IsAdmin:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = userRepo.CreateUser(ctx, adminUser)
	require.NoError(t, err)

	// Create a dummy metadata file to simulate a movie file
	// Virtual path: /movies/test.mkv
	// Metadata path: /metadata/movies/test.mkv.meta
	movieDir := filepath.Join(metadataDir, "movies")
	err = os.MkdirAll(movieDir, 0755)
	require.NoError(t, err)

	metaFilePath := filepath.Join(movieDir, "test.mkv.meta")
	err = os.WriteFile(metaFilePath, []byte("metadata content"), 0644)
	require.NoError(t, err)

	// Define test cases
	tests := []struct {
		name         string
		host         string
		expectedHost string
	}{
		{
			name:         "Default localhost",
			host:         "",
			expectedHost: "localhost",
		},
		{
			name:         "Custom IP",
			host:         "192.168.1.100",
			expectedHost: "192.168.1.100",
		},
		{
			name:         "Custom Domain",
			host:         "media.example.com",
			expectedHost: "media.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Config
			cfg := &config.Config{
				Import: config.ImportConfig{
					ImportStrategy: config.ImportStrategySTRM,
					ImportDir:      &importDir,
				},
				Metadata: config.MetadataConfig{
					RootPath: metadataDir,
				},
				WebDAV: config.WebDAVConfig{
					Port: 8080,
					Host: tt.host,
				},
			}

			configGetter := func() *config.Config {
				return cfg
			}

			// Setup Coordinator
			coord := NewCoordinator(Config{
				ConfigGetter: configGetter,
				UserRepo:     userRepo,
			})

			// Call CreateStrmFiles
			item := &database.ImportQueueItem{ID: 1}
			resultingPath := "/movies/test.mkv"

			err := coord.CreateStrmFiles(ctx, item, resultingPath)
			require.NoError(t, err)

			// Check generated file content
			strmPath := filepath.Join(importDir, "movies", "test.mkv.strm")
			content, err := os.ReadFile(strmPath)
			require.NoError(t, err)

			url := string(content)
			expectedPrefix := "http://" + tt.expectedHost + ":8080/api/files/stream"
			assert.True(t, strings.HasPrefix(url, expectedPrefix), "URL %s should start with %s", url, expectedPrefix)

			// Cleanup for next iteration
			os.Remove(strmPath)
		})
	}
}
