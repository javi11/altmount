package repair

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

const par2SegID = "<par2-0@altmount>"

// fakeMeta serves a single file's metadata.
type fakeMeta struct{ m *metapb.FileMetadata }

func (f fakeMeta) ReadFileMetadata(string) (*metapb.FileMetadata, error) { return f.m, nil }

// fakeFetcher serves present article bodies and reports a configured set as
// permanently missing.
type fakeFetcher struct {
	present map[string][]byte
	missing map[string]bool
}

func (f fakeFetcher) Fetch(_ context.Context, id string) ([]byte, bool, error) {
	if f.missing[id] {
		return nil, true, nil
	}
	if d, ok := f.present[id]; ok {
		return d, false, nil
	}
	return nil, false, fmt.Errorf("unexpected fetch for %s", id)
}

type mapSink struct{ got map[string][]byte }

func (m *mapSink) Put(id string, data []byte) error {
	m.got[id] = append([]byte(nil), data...)
	return nil
}

// loadPar2Fixture reads the shared par2 fixtures (data + recovery bytes).
func loadPar2Fixture(t *testing.T) (data, par2Bytes []byte) {
	t.Helper()
	base := filepath.Join("par2", "testdata")
	var err error
	data, err = os.ReadFile(filepath.Join(base, "data.bin"))
	if err != nil {
		t.Fatalf("read data.bin: %v", err)
	}
	for _, n := range []string{"recovery.par2", "recovery.vol0+8.par2"} {
		b, err := os.ReadFile(filepath.Join(base, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		par2Bytes = append(par2Bytes, b...)
	}
	return data, par2Bytes
}

// buildFixtureMeta tiles data into segments of segSize and attaches the PAR2
// recovery data as a single PAR2 segment. Returns the metadata plus the present
// segment bodies keyed by message-ID.
func buildFixtureMeta(data, par2Bytes []byte, segSize int) (*metapb.FileMetadata, map[string][]byte, []string) {
	present := map[string][]byte{par2SegID: par2Bytes}
	var segData []*metapb.SegmentData
	var ids []string
	for off := 0; off < len(data); off += segSize {
		end := off + segSize
		if end > len(data) {
			end = len(data)
		}
		id := fmt.Sprintf("<seg%d@altmount>", off/segSize)
		segData = append(segData, &metapb.SegmentData{
			Id:          id,
			StartOffset: int64(off),
			EndOffset:   int64(end - 1), // inclusive
			SegmentSize: int64(end - off),
		})
		present[id] = append([]byte(nil), data[off:end]...)
		ids = append(ids, id)
	}
	meta := &metapb.FileMetadata{
		FileSize:    int64(len(data)),
		SegmentData: segData,
		Par2Files: []*metapb.Par2FileReference{{
			Filename: "recovery.par2",
			SegmentData: []*metapb.SegmentData{{
				Id:          par2SegID,
				StartOffset: 0,
				EndOffset:   int64(len(par2Bytes) - 1),
				SegmentSize: int64(len(par2Bytes)),
			}},
		}},
	}
	return meta, present, ids
}

func TestServiceRepairFile(t *testing.T) {
	data, par2Bytes := loadPar2Fixture(t)
	meta, present, ids := buildFixtureMeta(data, par2Bytes, 5000)

	missingIDs := []string{ids[1], ids[5]}
	missing := map[string]bool{}
	for _, id := range missingIDs {
		missing[id] = true
		delete(present, id) // the fetcher must never be asked for these
	}

	sink := &mapSink{got: map[string][]byte{}}
	svc := NewService(fakeMeta{meta}, fakeFetcher{present: present, missing: missing}, sink, nil, nil)

	out, err := svc.RepairFile(context.Background(), "/movies/data.bin")
	if err != nil {
		t.Fatalf("RepairFile: %v", err)
	}
	if out.MissingSegments != 2 || len(out.RecoveredSegments) != 2 {
		t.Fatalf("outcome = %+v, want 2 missing / 2 recovered", out)
	}

	// Reconstructed bytes must equal the original segment ranges.
	for _, id := range missingIDs {
		var off, end int
		fmt.Sscanf(id, "<seg%d@altmount>", &off)
		start := off * 5000
		end = start + 5000
		if end > len(data) {
			end = len(data)
		}
		if !bytes.Equal(sink.got[id], data[start:end]) {
			t.Fatalf("segment %s reconstructed incorrectly", id)
		}
	}
}

func TestServiceNoPar2(t *testing.T) {
	meta := &metapb.FileMetadata{FileSize: 10, SegmentData: []*metapb.SegmentData{{Id: "a", EndOffset: 9}}}
	svc := NewService(fakeMeta{meta}, fakeFetcher{}, &mapSink{got: map[string][]byte{}}, nil, nil)
	if _, err := svc.RepairFile(context.Background(), "/x"); err != ErrNoPar2 {
		t.Fatalf("got %v, want ErrNoPar2", err)
	}
}

func TestServicePar2Mismatch(t *testing.T) {
	data, par2Bytes := loadPar2Fixture(t)
	meta, present, _ := buildFixtureMeta(data, par2Bytes, 5000)
	meta.FileSize = 12345 // lie about size → PAR2 set won't match
	svc := NewService(fakeMeta{meta}, fakeFetcher{present: present}, &mapSink{got: map[string][]byte{}}, nil, nil)
	if _, err := svc.RepairFile(context.Background(), "/x"); err != ErrPar2Mismatch {
		t.Fatalf("got %v, want ErrPar2Mismatch", err)
	}
}

func TestServiceNoMissingIsNoop(t *testing.T) {
	data, par2Bytes := loadPar2Fixture(t)
	meta, present, _ := buildFixtureMeta(data, par2Bytes, 5000)
	sink := &mapSink{got: map[string][]byte{}}
	svc := NewService(fakeMeta{meta}, fakeFetcher{present: present}, sink, nil, nil)
	out, err := svc.RepairFile(context.Background(), "/x")
	if err != nil {
		t.Fatalf("RepairFile: %v", err)
	}
	if out.MissingSegments != 0 || len(sink.got) != 0 {
		t.Fatalf("expected no-op, got %+v / %d sunk", out, len(sink.got))
	}
}
