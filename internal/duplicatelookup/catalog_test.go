package duplicatelookup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/metadata"
	pb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/spf13/afero"
)

func TestCatalogPersistentFingerprintAndInvalidation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	meta := metadata.NewMetadataService(root)
	db, e := database.NewDB(database.Config{DatabasePath: filepath.Join(t.TempDir(), "db")})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	repo := database.NewHealthRepository(db.Connection(), database.DialectSQLite)
	store := filepath.Join(root, "release.nzb")
	s := &pb.NzbStore{Files: []*pb.NzbFileEntry{{Subject: `"movie.mkv" yEnc (1/1)`, Groups: []string{"alt.binaries.test"}, Segments: []*pb.NzbSeg{{Id: "id", Bytes: 3, Number: 1}}}}}
	if e := meta.Store().WriteStore(store, s); e != nil {
		t.Fatal(e)
	}
	m := &pb.FileMetadata{FileSize: 3, Status: pb.FileStatus_FILE_STATUS_HEALTHY, OriginalFilename: "movie.mkv", SegmentData: []*pb.SegmentData{{Id: "id", SegmentSize: 3, EndOffset: 2}}}
	if e := meta.WriteFileMetadataAuto(ctx, "movies/movie_1.mkv", m, map[string]int64{"id": 0}, store); e != nil {
		t.Fatal(e)
	}
	reads := 0
	opener := func(context.Context, string) (afero.File, error) {
		reads++
		fs := afero.NewMemMapFs()
		afero.WriteFile(fs, "f", []byte{0, 1, 2}, 0600)
		return fs.Open("f")
	}
	cat := NewCatalog(meta, repo, opener)
	hash, _ := Fingerprint(bytes.NewReader([]byte{0, 1, 2}), 3)
	r := Request{Algorithm: Algorithm, Name: "movie.mkv", Size: 3, Hash: hash}
	for range 2 {
		xml, e := cat.Lookup(ctx, r)
		if e != nil || !bytes.Contains(xml, []byte("alt.binaries.test")) {
			t.Fatalf("lookup=%s err=%v", xml, e)
		}
	}
	if reads != 1 {
		t.Fatalf("reads=%d", reads)
	}
	cat = NewCatalog(meta, repo, opener)
	if xml, e := cat.Lookup(ctx, r); e != nil || len(xml) == 0 {
		t.Fatalf("persisted lookup: %s %v", xml, e)
	}
	if reads != 1 {
		t.Fatalf("fingerprint not persisted: %d", reads)
	}
	for _, change := range []func(*Request){func(r *Request) { r.Name = "movie_1.mkv" }, func(r *Request) { r.Size++ }, func(r *Request) { r.Hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }} {
		miss := r
		change(&miss)
		if xml, e := cat.Lookup(ctx, miss); e != nil || len(xml) != 0 {
			t.Fatalf("miss=%s err=%v", xml, e)
		}
	}
	unrelated := meta.GetMetadataFilePath("unrelated.bin")
	os.WriteFile(unrelated, []byte("broken"), 0600)
	if xml, e := cat.Lookup(ctx, r); e != nil || len(xml) == 0 {
		t.Fatalf("unchanged catalog rescanned unrelated files: %s %v", xml, e)
	}
	os.Remove(unrelated)
	m, e = meta.ReadFileMetadata("movies/movie_1.mkv")
	if e != nil {
		t.Fatal(e)
	}
	m.Status = pb.FileStatus_FILE_STATUS_DEGRADED
	if e = meta.WriteFileMetadata("movies/movie_1.mkv", m); e != nil {
		t.Fatal(e)
	}
	if xml, e := cat.Lookup(ctx, r); e != nil || len(xml) != 0 {
		t.Fatalf("degraded matched %s %v", xml, e)
	}
	m.Status = pb.FileStatus_FILE_STATUS_HEALTHY
	m.ModifiedAt++
	meta.WriteFileMetadata("movies/movie_1.mkv", m)
	cat.refreshedAt = time.Time{} // newly eligible files are discovered on the next refresh
	if xml, e := cat.Lookup(ctx, r); e != nil || len(xml) == 0 {
		t.Fatalf("restored=%s %v", xml, e)
	}
	if reads != 2 {
		t.Fatalf("metadata replacement reused hash: %d", reads)
	}
	if e := os.Remove(store); e != nil {
		t.Fatal(e)
	}
	if xml, e := cat.Lookup(ctx, r); e == nil || len(xml) != 0 {
		t.Fatalf("missing cached source matched: %s %v", xml, e)
	}
}
