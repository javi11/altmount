package duplicatelookup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/holes"
	"github.com/kipsilabs/altmount/internal/metadata"
	pb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type catalogFixture struct {
	cat       *Catalog
	meta      *metadata.MetadataService
	repo      *database.HealthRepository
	store     *pb.NzbStore
	storePath string
	req       Request
	opens     int
	data      []byte
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	root := t.TempDir()
	f := &catalogFixture{meta: metadata.NewMetadataService(root), data: []byte{0, 1, 2}}
	db, err := database.NewDB(database.Config{DatabasePath: filepath.Join(t.TempDir(), "catalog.db")})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	f.repo = database.NewHealthRepository(db.Connection(), db.Dialect())
	f.storePath = filepath.Join(root, "release.nzbz")
	f.store = &pb.NzbStore{Files: []*pb.NzbFileEntry{{Subject: `"movie.mkv" yEnc (1/1)`, Groups: []string{"alt.binaries.test"}, Segments: []*pb.NzbSeg{{Id: "id", Bytes: 3, Number: 1}}}}}
	require.NoError(t, f.meta.Store().WriteStore(f.storePath, f.store))
	m := &pb.FileMetadata{FileSize: 3, Status: pb.FileStatus_FILE_STATUS_HEALTHY, OriginalFilename: "movie.mkv", SegmentData: []*pb.SegmentData{{Id: "id", SegmentSize: 3, EndOffset: 2}}}
	require.NoError(t, f.meta.WriteFileMetadataAuto(context.Background(), "movies/renamed.mkv", m, map[string]int64{"id": 0}, f.storePath))
	f.cat = NewCatalog(f.meta, f.repo, func(context.Context, string) (afero.File, error) {
		f.opens++
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "source", f.data, 0600); err != nil {
			return nil, err
		}
		return fs.Open("source")
	})
	hash, err := Fingerprint(bytes.NewReader(f.data), 3)
	require.NoError(t, err)
	f.req = Request{Algorithm: Algorithm, Name: "movie.mkv", Size: 3, Hash: hash}
	return f
}

func TestCatalogRejectsUnsafeMetadata(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*pb.FileMetadata)
	}{
		{"degraded", func(m *pb.FileMetadata) { m.Status = pb.FileStatus_FILE_STATUS_DEGRADED }},
		{"corrupted", func(m *pb.FileMetadata) { m.Status = pb.FileStatus_FILE_STATUS_CORRUPTED }},
		{"encrypted", func(m *pb.FileMetadata) { m.Encryption = pb.Encryption_AES }},
		{"password", func(m *pb.FileMetadata) { m.Password = "secret" }},
		{"nested", func(m *pb.FileMetadata) { m.NestedSources = []*pb.NestedSegmentSource{{}} }},
		{"transformed", func(m *pb.FileMetadata) { m.ClipBoundaries = []*pb.ClipBoundary{{ByteLen: 3}} }},
		{"known holes", func(m *pb.FileMetadata) { m.KnownHoles = []*pb.HoleRun{{Count: 1}} }},
		{"archive slice", func(m *pb.FileMetadata) {
			m.SegmentData[0].StartOffset = 1
			m.FileSize = 2
			m.SegmentRefs = nil
			m.SegmentRuns = nil
			m.StoreRef = ""
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newCatalogFixture(t)
			nzb, err := f.cat.Lookup(context.Background(), f.req)
			require.NoError(t, err)
			require.NotEmpty(t, nzb)
			m, err := f.meta.ReadFileMetadata("movies/renamed.mkv")
			require.NoError(t, err)
			tt.change(m)
			require.NoError(t, f.meta.WriteFileMetadata("movies/renamed.mkv", m))
			nzb, err = f.cat.Lookup(context.Background(), f.req)
			require.NoError(t, err)
			require.Empty(t, nzb)
			require.Equal(t, 1, f.opens)
		})
	}
}

func TestCatalogLiveDatabaseHealthOverridesCachedFingerprint(t *testing.T) {
	f := newCatalogFixture(t)
	nzb, err := f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.NotEmpty(t, nzb)
	require.NoError(t, f.repo.UpdateFileHealth(context.Background(), "movies/renamed.mkv", database.HealthStatusCorrupted, nil, nil, nil, false))
	nzb, err = f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.Empty(t, nzb)
	require.Equal(t, 1, f.opens)
}

func TestCatalogStoreReplacementRecomputesFingerprint(t *testing.T) {
	f := newCatalogFixture(t)
	nzb, err := f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.NotEmpty(t, nzb)
	// Same data IDs, a changed release store must still invalidate the cached identity.
	f.store.Files[0].Groups = []string{"alt.binaries.changed"}
	require.NoError(t, f.meta.Store().WriteStore(f.storePath, f.store))
	nzb, err = f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.Contains(t, string(nzb), "alt.binaries.changed")
	require.Equal(t, 2, f.opens)
	nzb, err = f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.NotEmpty(t, nzb)
	require.Equal(t, 2, f.opens, "new identity must be persisted")
}

func TestCatalogRejectsMultiFileAndArchiveReleases(t *testing.T) {
	for _, name := range []string{"multi", "archive", "empty sidecar"} {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t)
			switch name {
			case "multi":
				f.store.Files = append(f.store.Files, &pb.NzbFileEntry{Subject: `"other.mkv" yEnc (1/1)`, Segments: []*pb.NzbSeg{{Id: "other", Bytes: 3, Number: 1}}})
			case "archive":
				f.store.Files[0].Subject = `"movie.rar" yEnc (1/1)`
			case "empty sidecar":
				f.store.Files = append(f.store.Files, &pb.NzbFileEntry{Subject: `"movie.par2" yEnc (1/1)`})
			}
			require.NoError(t, f.meta.Store().WriteStore(f.storePath, f.store))
			nzb, err := f.cat.Lookup(context.Background(), f.req)
			require.NoError(t, err)
			require.Empty(t, nzb)
			require.Zero(t, f.opens)
		})
	}
}

func TestCatalogPreservesPAR2AndRecoversExistingOriginalName(t *testing.T) {
	f := newCatalogFixture(t)
	f.store.Files = append(f.store.Files, &pb.NzbFileEntry{Subject: `"movie.par2" yEnc (1/1)`, Groups: []string{"alt.binaries.recovery"}, Segments: []*pb.NzbSeg{{Id: "par2-id", Bytes: 4, Number: 1}}})
	require.NoError(t, f.meta.Store().WriteStore(f.storePath, f.store))
	m, err := f.meta.ReadFileMetadata("movies/renamed.mkv")
	require.NoError(t, err)
	m.OriginalFilename = ""
	require.NoError(t, f.meta.WriteFileMetadata("movies/renamed.mkv", m))
	nzb, err := f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.Contains(t, string(nzb), "par2-id")
	require.Contains(t, string(nzb), "movie.mkv")
}

func TestCatalogReadFailureAndCancellation(t *testing.T) {
	f := newCatalogFixture(t)
	f.cat.open = func(context.Context, string) (afero.File, error) { return nil, errors.New("provider unavailable") }
	nzb, err := f.cat.Lookup(context.Background(), f.req)
	require.ErrorContains(t, err, "provider unavailable")
	require.Empty(t, nzb)
	f.cat.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = f.cat.Lookup(ctx, f.req)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	<-f.cat.gate
}

func TestCatalogMissingMetadataIsNeverReused(t *testing.T) {
	f := newCatalogFixture(t)
	nzb, err := f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.NotEmpty(t, nzb)
	require.NoError(t, os.Remove(f.meta.GetMetadataFilePath("movies/renamed.mkv")))
	nzb, err = f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.Empty(t, nzb)
}

func TestCatalogRejectsSyntheticTailGap(t *testing.T) {
	f := newCatalogFixture(t)
	gap := holes.PlaceholderID(2, "id")
	f.store.Files[0].Segments = append(f.store.Files[0].Segments, &pb.NzbSeg{Id: gap, Bytes: 3, Number: 2})
	require.NoError(t, f.meta.Store().WriteStore(f.storePath, f.store))
	m := &pb.FileMetadata{FileSize: 6, Status: pb.FileStatus_FILE_STATUS_HEALTHY, OriginalFilename: "movie.mkv", SegmentData: []*pb.SegmentData{{Id: "id", SegmentSize: 3, EndOffset: 2}, {Id: gap, SegmentSize: 3, EndOffset: 2}}}
	require.NoError(t, f.meta.WriteFileMetadataAuto(context.Background(), "movies/renamed.mkv", m, map[string]int64{"id": 0, gap: 1}, f.storePath))
	f.data = []byte{0, 1, 2, 0, 0, 0}
	f.req.Size = 6
	var err error
	f.req.Hash, err = Fingerprint(bytes.NewReader(f.data), 6)
	require.NoError(t, err)
	nzb, err := f.cat.Lookup(context.Background(), f.req)
	require.NoError(t, err)
	require.Empty(t, nzb)
	require.Zero(t, f.opens)
}
