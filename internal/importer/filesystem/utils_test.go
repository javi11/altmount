package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SeparateFiles — 7z archive
// ---------------------------------------------------------------------------

func TestSeparateFiles_7z_NewFormat(t *testing.T) {
	// New format: name.7z.001 … name.7z.068
	// Only the first part has Is7zArchive=true (magic bytes); the rest have it false.
	files := make([]parser.ParsedFile, 0, 70)
	files = append(files, parser.ParsedFile{Filename: "movie.7z.001", Is7zArchive: true})
	for i := 2; i <= 68; i++ {
		files = append(files, parser.ParsedFile{Filename: "movie.7z.001", Is7zArchive: false})
	}
	files = append(files, parser.ParsedFile{Filename: "movie.7z.par2", IsPar2Archive: true})
	files = append(files, parser.ParsedFile{Filename: "movie.7z.vol00+01.par2", IsPar2Archive: true})

	regular, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(regular) != 0 {
		t.Errorf("expected 0 regular files, got %d", len(regular))
	}
	if len(archive) != 68 {
		t.Errorf("expected 68 archive files, got %d", len(archive))
	}
	if len(par2) != 2 {
		t.Errorf("expected 2 par2 files, got %d", len(par2))
	}
}

func TestSeparateFiles_7z_OldFormat(t *testing.T) {
	// Old format: name.7z (vol 1) + name.002 … name.068 (vols 2-68), no Is7zArchive on parts.
	files := make([]parser.ParsedFile, 0, 70)
	files = append(files, parser.ParsedFile{Filename: "movie.7z", Is7zArchive: true})
	for i := 2; i <= 68; i++ {
		files = append(files, parser.ParsedFile{Filename: "movie.001", Is7zArchive: false})
	}
	files = append(files, parser.ParsedFile{Filename: "movie.7z.par2", IsPar2Archive: true})

	regular, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(regular) != 0 {
		t.Errorf("expected 0 regular files, got %d", len(regular))
	}
	if len(archive) != 68 {
		t.Errorf("expected 68 archive files, got %d", len(archive))
	}
	if len(par2) != 1 {
		t.Errorf("expected 1 par2 file, got %d", len(par2))
	}
}

func TestSeparateFiles_7z_SingleFile(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "movie.7z", Is7zArchive: true},
	}
	regular, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(regular) != 0 {
		t.Errorf("expected 0 regular, got %d", len(regular))
	}
	if len(archive) != 1 {
		t.Errorf("expected 1 archive, got %d", len(archive))
	}
	if len(par2) != 0 {
		t.Errorf("expected 0 par2, got %d", len(par2))
	}
}

func TestSeparateFiles_7z_Par2NotMisclassified(t *testing.T) {
	// A .7z.par2 file with Is7zArchive=true (magic bytes coincidence) must land in par2.
	files := []parser.ParsedFile{
		{Filename: "movie.7z.001", Is7zArchive: true},
		{Filename: "movie.7z.par2", Is7zArchive: true, IsPar2Archive: true},
		{Filename: "movie.7z.vol01+02.par2", IsPar2Archive: true},
	}

	_, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(archive) != 1 {
		t.Errorf("expected 1 archive file, got %d", len(archive))
	}
	if len(par2) != 2 {
		t.Errorf("expected 2 par2 files, got %d", len(par2))
	}
	for _, f := range par2 {
		if !IsPar2File(f.Filename) && !f.IsPar2Archive {
			t.Errorf("non-par2 file ended up in par2 slice: %s", f.Filename)
		}
	}
}

func TestSeparateFiles_7z_AllIs7zArchiveFalse(t *testing.T) {
	// Edge case from old-format NZBs where the parser couldn't set Is7zArchive on ANY file
	// (e.g. obfuscated filenames + no magic bytes). SeparateFiles must still classify all
	// non-par2 as archive because the NZB-level type is NzbType7zArchive.
	files := []parser.ParsedFile{
		{Filename: "abc123.016", Is7zArchive: false},
		{Filename: "abc123.017", Is7zArchive: false},
		{Filename: "abc123.018", Is7zArchive: false},
		{Filename: "abc123.par2", IsPar2Archive: true},
	}

	regular, archive, par2 := SeparateFiles(files, parser.NzbType7zArchive)

	if len(regular) != 0 {
		t.Errorf("expected 0 regular files, got %d", len(regular))
	}
	if len(archive) != 3 {
		t.Errorf("expected 3 archive files, got %d", len(archive))
	}
	if len(par2) != 1 {
		t.Errorf("expected 1 par2 file, got %d", len(par2))
	}
}

// ---------------------------------------------------------------------------
// SeparateFiles — RAR archive
// ---------------------------------------------------------------------------

func TestSeparateFiles_RAR_MultiPart(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "movie.part01.rar", IsRarArchive: true},
		{Filename: "movie.part02.rar", IsRarArchive: true},
		{Filename: "movie.part03.rar", IsRarArchive: true},
		{Filename: "movie.part01.rar.par2", IsPar2Archive: true},
	}

	regular, archive, par2 := SeparateFiles(files, parser.NzbTypeRarArchive)

	if len(regular) != 0 {
		t.Errorf("expected 0 regular, got %d", len(regular))
	}
	if len(archive) != 3 {
		t.Errorf("expected 3 archive files, got %d", len(archive))
	}
	if len(par2) != 1 {
		t.Errorf("expected 1 par2 file, got %d", len(par2))
	}
}

// ---------------------------------------------------------------------------
// SeparateFiles — non-archive types
// ---------------------------------------------------------------------------

func TestSeparateFiles_MultiFile(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "episode.S01E01.mkv"},
		{Filename: "episode.S01E02.mkv"},
		{Filename: "episode.nfo"},
		{Filename: "episode.par2", IsPar2Archive: true},
	}

	regular, archive, par2 := SeparateFiles(files, parser.NzbTypeMultiFile)

	if len(regular) != 3 {
		t.Errorf("expected 3 regular, got %d", len(regular))
	}
	if len(archive) != 0 {
		t.Errorf("expected 0 archive, got %d", len(archive))
	}
	if len(par2) != 1 {
		t.Errorf("expected 1 par2, got %d", len(par2))
	}
}

// ---------------------------------------------------------------------------
// EnsureUniqueVirtualPath
// ---------------------------------------------------------------------------

func newTestMetadataService(t *testing.T) *metadata.MetadataService {
	t.Helper()
	return metadata.NewMetadataService(t.TempDir())
}

func writeHealthyMeta(t *testing.T, ms *metadata.MetadataService, virtualPath string) {
	t.Helper()
	dir := filepath.Dir(virtualPath)
	if dir != "" && dir != "/" && dir != "." {
		require.NoError(t, os.MkdirAll(ms.GetMetadataDirectoryPath(dir), 0755))
	}
	fileMeta := ms.CreateFileMetadata(
		1000, "/fake/nzb.nzb.gz",
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(virtualPath, fileMeta))
}

func TestEnsureUniqueVirtualPath_NoConflict(t *testing.T) {
	ms := newTestMetadataService(t)
	result := EnsureUniqueVirtualPath("/complete/tv/show.S01E01.mkv", ms)
	assert.Equal(t, "/complete/tv/show.S01E01.mkv", result)
}

func TestEnsureUniqueVirtualPath_OneConflict(t *testing.T) {
	ms := newTestMetadataService(t)
	writeHealthyMeta(t, ms, "/complete/tv/show.S01E01.mkv")
	result := EnsureUniqueVirtualPath("/complete/tv/show.S01E01.mkv", ms)
	assert.Equal(t, "/complete/tv/show.S01E01_1.mkv", result)
}

func TestEnsureUniqueVirtualPath_TwoConflicts(t *testing.T) {
	ms := newTestMetadataService(t)
	writeHealthyMeta(t, ms, "/complete/tv/show.S01E01.mkv")
	writeHealthyMeta(t, ms, "/complete/tv/show.S01E01_1.mkv")
	result := EnsureUniqueVirtualPath("/complete/tv/show.S01E01.mkv", ms)
	assert.Equal(t, "/complete/tv/show.S01E01_2.mkv", result)
}

func TestEnsureUniqueVirtualPath_UnhealthyNotDeduplicated(t *testing.T) {
	ms := newTestMetadataService(t)
	dir := "/complete/tv"
	require.NoError(t, os.MkdirAll(ms.GetMetadataDirectoryPath(dir), 0755))
	fileMeta := ms.CreateFileMetadata(
		1000, "/fake/nzb.nzb.gz",
		metapb.FileStatus_FILE_STATUS_CORRUPTED,
		nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata("/complete/tv/show.S01E01.mkv", fileMeta))
	result := EnsureUniqueVirtualPath("/complete/tv/show.S01E01.mkv", ms)
	assert.Equal(t, "/complete/tv/show.S01E01.mkv", result)
}
