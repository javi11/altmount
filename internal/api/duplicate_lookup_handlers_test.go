package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/kipsilabs/altmount/internal/auth"
	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/duplicatelookup"
	"github.com/kipsilabs/altmount/internal/metadata"
	pb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type duplicateLookupStub struct {
	data    []byte
	err     error
	calls   int
	request duplicatelookup.Request
}

func (s *duplicateLookupStub) Lookup(_ context.Context, r duplicatelookup.Request) ([]byte, error) {
	s.calls++
	s.request = r
	return s.data, s.err
}

func TestDuplicateLookupHTTP(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	const valid = `{"algorithm":"sample-sha256-v1","name":"Movie.mkv","size":42,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	for _, tt := range []struct {
		name, key, body string
		data            []byte
		err             error
		code, calls     int
	}{
		{name: "missing key", body: valid, code: 401},
		{name: "invalid key", key: "invalid", body: valid, code: 401},
		{name: "invalid JSON", key: key, body: `{`, code: 400},
		{name: "unknown field", key: key, body: strings.TrimSuffix(valid, "}") + `,"path":"/etc/passwd"}`, code: 400},
		{name: "multiple objects", key: key, body: valid + valid, code: 400},
		{name: "wrong algorithm", key: key, body: strings.Replace(valid, "sample-sha256-v1", "unknown", 1), code: 400},
		{name: "path forbidden", key: key, body: strings.Replace(valid, "Movie.mkv", "../Movie.mkv", 1), code: 400},
		{name: "negative size", key: key, body: strings.Replace(valid, `:42`, `:-1`, 1), code: 400},
		{name: "match", key: key, body: valid, data: []byte(`<nzb><file/></nzb>`), code: 200, calls: 1},
		{name: "miss", key: key, body: valid, code: 204, calls: 1},
		{name: "service failure", key: key, body: valid, err: errors.New("storage unavailable"), code: 503, calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.API.KeyOverride = key
			stub := &duplicateLookupStub{data: tt.data, err: tt.err}
			s := &Server{configManager: &mockConfigManager{cfg: cfg}, duplicateLookup: stub}
			app := fiber.New()
			app.Post("/lookup", s.handleDuplicateLookup)
			req := httptest.NewRequest(http.MethodPost, "/lookup", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", tt.key)
			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, tt.code, resp.StatusCode)
			require.Equal(t, tt.calls, stub.calls)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			if tt.code == 200 {
				require.Equal(t, tt.data, body)
				require.Equal(t, "application/x-nzb", resp.Header.Get("Content-Type"))
				require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
				require.Equal(t, "Movie.mkv", stub.request.Name)
			}
			if tt.code == 204 {
				require.Empty(t, body)
			}
			if tt.code == 503 {
				require.NotContains(t, string(body), "storage unavailable")
			}
		})
	}
}

func TestDuplicateLookupRouteUsesAPIKeyWithoutJWT(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.API.AllowedOrigins = []string{"http://localhost"}
	cfg.API.KeyOverride = "0123456789abcdef0123456789abcdef"
	db, err := database.NewDB(database.Config{DatabasePath: t.TempDir() + "/auth.db"})
	require.NoError(t, err)
	defer db.Close()
	users := database.NewUserRepository(db.Connection(), db.Dialect())
	authCfg := auth.DefaultConfig()
	authCfg.JWTSecret = "test-only-jwt-secret"
	authService, err := auth.NewService(authCfg, users)
	require.NoError(t, err)
	s := &Server{config: DefaultConfig(), configManager: &mockConfigManager{cfg: cfg}, authService: authService, userRepo: users, duplicateLookup: &duplicateLookupStub{}}
	app := fiber.New()
	s.SetupRoutes(app)
	for _, key := range []string{cfg.API.KeyOverride, ""} {
		req := httptest.NewRequest(http.MethodPost, "/api/files/duplicate-lookup", strings.NewReader(`{"algorithm":"sample-sha256-v1","name":"Movie.mkv","size":42,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key)
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		resp.Body.Close()
		if key != "" {
			require.Equal(t, 204, resp.StatusCode)
		} else {
			require.Equal(t, 401, resp.StatusCode)
		}
	}
}

func newDuplicateLookupCatalogServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.API.KeyOverride = "0123456789abcdef0123456789abcdef"
	db, err := database.NewDB(database.Config{DatabasePath: filepath.Join(t.TempDir(), "lookup.db")})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	meta := metadata.NewMetadataService(t.TempDir())
	storePath := filepath.Join(t.TempDir(), "original.nzbz")
	store := &pb.NzbStore{Files: []*pb.NzbFileEntry{{Subject: `"movie.mkv" yEnc (1/1)`, Poster: "source@example.test", Groups: []string{"alt.binaries.test"}, Segments: []*pb.NzbSeg{{Id: "original-article-id", Bytes: 3, Number: 1}}}}}
	require.NoError(t, meta.Store().WriteStore(storePath, store))
	m := &pb.FileMetadata{FileSize: 3, OriginalFilename: "movie.mkv", Status: pb.FileStatus_FILE_STATUS_HEALTHY, SegmentData: []*pb.SegmentData{{Id: "original-article-id", SegmentSize: 3, EndOffset: 2}}}
	require.NoError(t, meta.WriteFileMetadataAuto(context.Background(), "movies/renamed_1.mkv", m, map[string]int64{"original-article-id": 0}, storePath))
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "source", []byte{0, 1, 2}, 0600))
	repo := database.NewHealthRepository(db.Connection(), db.Dialect())
	catalog := duplicatelookup.NewCatalog(meta, repo, func(context.Context, string) (afero.File, error) { return fs.Open("source") })
	return &Server{configManager: &mockConfigManager{cfg: cfg}, duplicateLookup: catalog}
}

func TestDuplicateLookupCatalogHTTP(t *testing.T) {
	s := newDuplicateLookupCatalogServer(t)
	app := fiber.New()
	app.Post("/lookup", s.handleDuplicateLookup)
	// Fixed cross-implementation vector; the original virtual path has been renamed.
	r := duplicatelookup.Request{Algorithm: "sample-sha256-v1", Name: "movie.mkv", Size: 3, Hash: "91faf454819e22599248399163fc7ae5a917c5e573b1fe2de6f7fe05872a6b9b"}
	for _, name := range []string{"movie.mkv", "renamed_1.mkv"} {
		r.Name = name
		body, err := json.Marshal(r)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/lookup", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "0123456789abcdef0123456789abcdef")
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		if name == "movie.mkv" {
			require.Equal(t, 200, resp.StatusCode)
			require.Contains(t, string(data), "original-article-id")
			require.Contains(t, string(data), "movie.mkv")
			require.Contains(t, string(data), "alt.binaries.test")
		} else {
			require.Equal(t, 204, resp.StatusCode)
			require.Empty(t, data)
		}
	}
}
