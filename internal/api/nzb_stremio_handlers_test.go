package api

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/testsupport/nzbbuild"
	"github.com/javi11/nzbparser"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestBuildStremioStreamsReturnsSingleFileWithFilenameIdentity(t *testing.T) {
	server, item := newStremioStreamsTestServer(t, []string{
		"The.Movie.2024.1080p.mkv",
	})

	streams, err := server.buildStremioStreams(context.Background(), item, "https://alt.example", "download-key", "Release.Name", nil)
	require.NoError(t, err)
	require.Len(t, streams, 1)

	stream := streams[0]
	require.Equal(t, "The.Movie.2024.1080p.mkv", stream.Title)
	require.Equal(t, "The.Movie.2024.1080p.mkv", stream.Name)
	require.Contains(t, stream.URL, "download_key=download-key")

	parsedURL, err := url.Parse(stream.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/files/stream", parsedURL.Path)
	require.Equal(t, "/complete/TV/Release.Name/The.Movie.2024.1080p.mkv", parsedURL.Query().Get("path"))
}

// TestBuildStremioStreamsRejectsMultiEpisodePackWithoutSelector guards the core
// fix: a season pack resolved without episode context must NOT silently return
// the first episode — it returns errStremioEpisodeAmbiguous instead.
func TestBuildStremioStreamsRejectsMultiEpisodePackWithoutSelector(t *testing.T) {
	server, item := newStremioStreamsTestServer(t, []string{
		"Show.S01E01.mkv",
		"Show.S01E02.mkv",
		"Show.S01E03.mkv",
	})

	streams, err := server.buildStremioStreams(context.Background(), item, "https://alt.example", "download-key", "Release.Name", nil)
	require.ErrorIs(t, err, errStremioEpisodeAmbiguous)
	require.Nil(t, streams)
}

// TestBuildStremioStreamsAllowsMultiPartMovieWithoutSelector ensures the
// ambiguity guard does not break movies split into multiple non-episode files.
func TestBuildStremioStreamsAllowsMultiPartMovieWithoutSelector(t *testing.T) {
	server, item := newStremioStreamsTestServer(t, []string{
		"The.Movie.2024.1080p.mkv",
		"The.Movie.2024.1080p-sample.mkv",
	})

	streams, err := server.buildStremioStreams(context.Background(), item, "https://alt.example", "download-key", "Release.Name", nil)
	require.NoError(t, err)
	require.Len(t, streams, 2)
}

func TestBuildStremioStreamsFiltersSeasonPackByEpisodeSelector(t *testing.T) {
	server, item := newStremioStreamsTestServer(t, []string{
		"Show.S01E01.mkv",
		"Show.S01E02.mkv",
		"Show.S01E03.mkv",
	})

	selector := &stremioEpisodeSelector{Season: 1, Episode: 2}
	streams, err := server.buildStremioStreams(context.Background(), item, "https://alt.example", "download-key", "Release.Name", selector)
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Equal(t, "Show.S01E02.mkv", streams[0].Name)
	require.Equal(t, "Show.S01E02.mkv", streams[0].Title)
}

func TestBuildStremioStreamsFindsMediaFilesInNestedSeasonPackDirectories(t *testing.T) {
	server, item := newStremioStreamsTestServer(t, []string{
		"Season 01/Show.S01E01.mkv",
		"Season 01/Show.S01E02.mkv",
	})

	selector := &stremioEpisodeSelector{Season: 1, Episode: 2}
	streams, err := server.buildStremioStreams(context.Background(), item, "https://alt.example", "download-key", "Release.Name", selector)
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Equal(t, "Show.S01E02.mkv", streams[0].Name)

	parsedURL, err := url.Parse(streams[0].URL)
	require.NoError(t, err)
	require.Equal(t, "/complete/TV/Release.Name/Season 01/Show.S01E02.mkv", parsedURL.Query().Get("path"))
}

func TestBuildStremioStreamsReturnsEmptySliceWhenSelectorDoesNotMatch(t *testing.T) {
	server, item := newStremioStreamsTestServer(t, []string{
		"Show.S01E01.mkv",
	})

	selector := &stremioEpisodeSelector{Season: 1, Episode: 2}
	streams, err := server.buildStremioStreams(context.Background(), item, "https://alt.example", "download-key", "Release.Name", selector)
	require.NoError(t, err)
	require.NotNil(t, streams)
	require.Empty(t, streams)
}

func TestStremioEpisodeSelectorMatchesCommonEpisodeFilenameForms(t *testing.T) {
	selector := &stremioEpisodeSelector{Season: 1, Episode: 2}

	require.True(t, selector.matches("Show.S01E02.mkv"))
	require.True(t, selector.matches("Show.s1e2.mkv"))
	require.True(t, selector.matches("Show.1x02.mkv"))
	require.True(t, selector.matches("Show.S01E01E02.mkv"))
	require.False(t, selector.matches("Show.S01E01.mkv"))
	require.False(t, selector.matches("Show.S02E02.mkv"))
}

// TestStremioEpisodeSelectorMatchesSeasonInDirectoryAndSpelledForms covers the
// hardened fallback: the season is established by a separate token (often the
// parent directory) while the filename carries only an episode token.
func TestStremioEpisodeSelectorMatchesSeasonInDirectoryAndSpelledForms(t *testing.T) {
	selector := &stremioEpisodeSelector{Season: 1, Episode: 5}

	// season from directory, episode-only in filename
	require.True(t, selector.matches("Season 01/Show E05.mkv"))
	require.True(t, selector.matches("S01/Show.E05.mkv"))
	require.False(t, selector.matches("Series 1/Show - 5.mkv")) // "- 5" carries no episode marker
	require.True(t, selector.matches("Season 1/Show.Episode.5.mkv"))
	// explicit combined marker is authoritative and not overridden by the fallback
	require.True(t, selector.matches("Season 01/Show.S01E05.mkv"))
	require.False(t, selector.matches("Season 02/Show E05.mkv")) // season mismatch
	require.False(t, selector.matches("Season 01/Show E06.mkv")) // episode mismatch
	require.False(t, selector.matches("Show.S02E05.mkv"))        // explicit marker, wrong season
}

func TestParseStremioContentID(t *testing.T) {
	tests := []struct {
		raw     string
		imdb    string
		season  int
		episode int
	}{
		{"tt1234567", "tt1234567", 0, 0},
		{"tt1234567:1:5", "tt1234567", 1, 5},
		{"tt1234567:1", "tt1234567", 1, 0},
		{"tt1234567:x:5", "tt1234567", 0, 5},
		{"", "", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			imdb, season, episode := parseStremioContentID(tc.raw)
			require.Equal(t, tc.imdb, imdb)
			require.Equal(t, tc.season, season)
			require.Equal(t, tc.episode, episode)
		})
	}
}

func TestCountDistinctEpisodes(t *testing.T) {
	require.Equal(t, 3, countDistinctEpisodes([]string{"Show.S01E01.mkv", "Show.S01E02.mkv", "Show.S01E03.mkv"}))
	require.Equal(t, 1, countDistinctEpisodes([]string{"Show.S01E01.mkv"}))
	require.Equal(t, 0, countDistinctEpisodes([]string{"The.Movie.2024.mkv", "The.Movie.2024-sample.mkv"}))
	// duplicate episode identity collapses to one
	require.Equal(t, 1, countDistinctEpisodes([]string{"Season 01/Show.S01E01.mkv", "Show.1x01.mkv"}))
}

// withQuery builds a Fiber context whose request carries the given raw query
// string, so request-parsing helpers can be exercised without a live server.
func withQuery(t *testing.T, rawQuery string) (*fiber.Ctx, func()) {
	t.Helper()
	app := fiber.New()
	fctx := &fasthttp.RequestCtx{}
	fctx.Request.SetRequestURI("/?" + rawQuery)
	ctx := app.AcquireCtx(fctx)
	return ctx, func() { app.ReleaseCtx(ctx) }
}

func TestStremioEpisodeSelectorFromRequest(t *testing.T) {
	nzbURL := url.QueryEscape("http://indexer.example/getnzb?id=abc&season=4&episode=8")

	tests := []struct {
		name  string
		query string
		want  *stremioEpisodeSelector
	}{
		{"explicit season/episode", "season=1&episode=5", &stremioEpisodeSelector{Season: 1, Episode: 5}},
		{"combined id", "id=tt1234567:2:7", &stremioEpisodeSelector{Season: 2, Episode: 7}},
		{"combined stremio_id", "stremio_id=tt1234567:3:9", &stremioEpisodeSelector{Season: 3, Episode: 9}},
		{"season/episode embedded in nzb_url", "nzb_url=" + nzbURL, &stremioEpisodeSelector{Season: 4, Episode: 8}},
		{"explicit beats id", "season=1&episode=2&id=tt1234567:5:6", &stremioEpisodeSelector{Season: 1, Episode: 2}},
		{"movie id yields nil", "id=tt1234567", nil},
		{"episode without season yields nil", "episode=5", nil},
		{"nothing yields nil", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, release := withQuery(t, tc.query)
			defer release()
			got := stremioEpisodeSelectorFromRequest(ctx)
			require.Equal(t, tc.want, got)
		})
	}
}

func newStremioStreamsTestServer(t *testing.T, names []string) (*Server, *database.ImportQueueItem) {
	t.Helper()

	root := t.TempDir()
	storagePath := "/complete/TV/Release.Name"
	metadataDir := filepath.Join(root, strings.TrimPrefix(storagePath, "/"))
	require.NoError(t, os.MkdirAll(metadataDir, 0755))
	for _, name := range names {
		metaPath := filepath.Join(metadataDir, name+".meta")
		require.NoError(t, os.MkdirAll(filepath.Dir(metaPath), 0755))
		require.NoError(t, os.WriteFile(metaPath, []byte("test"), 0644))
	}

	server := &Server{metadataService: metadata.NewMetadataService(root)}
	return server, &database.ImportQueueItem{
		ID:          42,
		StoragePath: &storagePath,
	}
}

// buildNzbBytes renders the given files (and optional meta) to NZB bytes as they
// would arrive over the wire, so deriveNzbNameFromContent can re-parse them.
func buildNzbBytes(t *testing.T, meta map[string]string, files ...nzbbuild.File) []byte {
	t.Helper()
	n := nzbbuild.Build(files...)
	if meta != nil {
		n.Meta = meta
	}
	data, err := nzbparser.Write(n)
	require.NoError(t, err)
	return data
}

func seg(id string, bytes int) nzbbuild.Segment { return nzbbuild.Segment{ID: id, Bytes: bytes} }

func TestDeriveNzbNameFromContent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "meta name is preferred when present and clean",
			data: buildNzbBytes(t,
				map[string]string{"name": "The.Great.Movie.2024.1080p.WEB-DL"},
				nzbbuild.File{Subject: `[1/1] "obfuscatedgarbage.mkv" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("a@x", 1000)}},
			),
			want: "The.Great.Movie.2024.1080p.WEB-DL",
		},
		{
			name: "largest media file subject wins when no usable meta",
			data: buildNzbBytes(t, nil,
				nzbbuild.File{Subject: `[1/3] "Sample.The.Movie.mp4" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("s@x", 1000)}},
				nzbbuild.File{Subject: `[2/3] "The.Sheep.Detectives.2026.2160p.mkv" yEnc (1/2)`, Segments: []nzbbuild.Segment{seg("m1@x", 500000), seg("m2@x", 500000)}},
				nzbbuild.File{Subject: `[3/3] "The.Sheep.Detectives.2026.2160p.par2" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("p@x", 2000)}},
			),
			want: "The.Sheep.Detectives.2026.2160p",
		},
		{
			name: "obfuscated meta falls through to media file",
			data: buildNzbBytes(t,
				map[string]string{"name": "deadbeefdeadbeefdeadbeefdeadbeef"},
				nzbbuild.File{Subject: `[1/1] "The.Clean.Release.2025.1080p.mkv" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("c@x", 5000)}},
			),
			want: "The.Clean.Release.2025.1080p",
		},
		{
			name: "all obfuscated returns empty",
			data: buildNzbBytes(t, nil,
				nzbbuild.File{Subject: `[1/2] "abcdef0123456789abcdef0123456789.mkv" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("o@x", 1000)}},
				nzbbuild.File{Subject: `[2/2] "abcdef0123456789abcdef0123456789.par2" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("op@x", 500)}},
			),
			want: "",
		},
		{
			name: "no media files returns empty",
			data: buildNzbBytes(t, nil,
				nzbbuild.File{Subject: `[1/1] "The.Release.2024.par2" yEnc (1/1)`, Segments: []nzbbuild.Segment{seg("np@x", 500)}},
			),
			want: "",
		},
		{
			name: "invalid nzb bytes returns empty",
			data: []byte("not an nzb"),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveNzbNameFromContent(tc.data)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestGenericDownloadNameIsObfuscated guards the trigger condition in handleNzbStreams:
// the generic transport name "download" must be classified obfuscated so the
// content-derived name override kicks in.
func TestGenericDownloadNameIsObfuscated(t *testing.T) {
	require.True(t, fileinfo.IsProbablyObfuscated("download"),
		`expected "download" to be treated as obfuscated so the name override triggers`)
}
