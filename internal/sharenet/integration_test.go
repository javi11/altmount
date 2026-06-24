package sharenet_test

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/sharenet"
	"github.com/javi11/nzbparser"
)

// TestIntegration_TwoNode_ShareMetaRebuildStore exercises the full P2P claim
// end-to-end over loopback, with no real DHT and no NNTP:
//
//   - Node A imports a release: builds the NzbStore, writes v3 metas referencing
//     it (the exact path the importer takes), and serves them via the real HTTP
//     handler.
//   - Node B rebuilds the store *locally* from the same NZB, fetches A's metas
//     through the real sharenet Client (DHT mocked to point at A's server),
//     repoints them at its own store, and writes them.
//   - We then prove B reconstructs byte-identical segment data from A's shared
//     refs against its independently-built store — and that no segment data ever
//     crossed the wire.
func TestIntegration_TwoNode_ShareMetaRebuildStore(t *testing.T) {
	ctx := context.Background()

	// A release: two virtual files, each backed by one NZB file. Full-use
	// segments (offset 0 .. size-1) so the writer folds them into SegmentRuns —
	// the common non-archive case.
	nzb := &nzbparser.Nzb{Files: nzbparser.NzbFiles{
		{
			Subject: "file0", Poster: "p", Date: 1, Groups: []string{"alt.bin"},
			Segments: nzbparser.NzbSegments{
				{ID: "seg-a@news", Number: 1, Bytes: 100},
				{ID: "seg-b@news", Number: 2, Bytes: 100},
				{ID: "seg-c@news", Number: 3, Bytes: 50},
			},
		},
		{
			Subject: "file1", Poster: "p", Date: 1, Groups: []string{"alt.bin"},
			Segments: nzbparser.NzbSegments{
				{ID: "seg-d@news", Number: 1, Bytes: 200},
				{ID: "seg-e@news", Number: 2, Bytes: 80},
			},
		},
	}}
	vpaths := []string{"Movies/Title/file0.bin", "Movies/Title/file1.bin"}
	releaseHash := sharenet.ComputeReleaseHash([]byte("the-raw-nzb-bytes"))

	// --- Node A: import + serve ------------------------------------------------
	rootA := t.TempDir()
	msA := metadata.NewMetadataService(rootA)
	storeA, indexA := parser.BuildStore(nzb)
	refA := filepath.Join(rootA, "release.nzbz")
	if err := msA.Store().WriteStore(refA, storeA); err != nil {
		t.Fatalf("node A WriteStore: %v", err)
	}
	for i, vp := range vpaths {
		meta := &metapb.FileMetadata{
			FileSize:    fileSize(nzb.Files[i]),
			SegmentData: segData(nzb.Files[i]),
		}
		// Mirrors the importer: convert inline segments to refs, write v3, inc ref.
		if err := msA.WriteFileMetadataV3(ctx, vp, meta, indexA, refA); err != nil {
			t.Fatalf("node A WriteFileMetadataV3 %s: %v", vp, err)
		}
	}

	shareStoreA := sharenet.NewReleaseStore(rootA)
	shareStoreA.Register(releaseHash, vpaths)
	srv := httptest.NewServer(sharenet.NewHandler(shareStoreA))
	defer srv.Close()

	// --- Node B: rebuild store locally + fetch metas ---------------------------
	rootB := t.TempDir()
	msB := metadata.NewMetadataService(rootB)
	storeB, _ := parser.BuildStore(nzb) // same NZB ⇒ byte-identical store + index
	refB := filepath.Join(rootB, "release.nzbz")
	if err := msB.Store().WriteStore(refB, storeB); err != nil {
		t.Fatalf("node B WriteStore: %v", err)
	}

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 0, sharenet.WithAllowPrivatePeers())

	files, err := client.LookupAndFetch(ctx, releaseHash)
	if err != nil {
		t.Fatalf("node B LookupAndFetch: %v", err)
	}
	if len(files.Metas) != len(vpaths) {
		t.Fatalf("expected %d metas, got %d", len(vpaths), len(files.Metas))
	}

	for i, m := range files.Metas {
		// The core claim: structural metas carry refs/runs, never inline segment
		// data — the message-ids stayed on node A.
		if len(m.Meta.SegmentData) != 0 {
			t.Fatalf("meta %d carried inline segment data over the wire", i)
		}
		if len(m.Meta.SegmentRuns) == 0 && len(m.Meta.SegmentRefs) == 0 {
			t.Fatalf("meta %d has no segment references", i)
		}
		if m.Meta.StoreRef != refA {
			t.Fatalf("meta %d StoreRef = %q; want peer's %q", i, m.Meta.StoreRef, refA)
		}

		// Apply exactly as the processor does: repoint at the local store, write.
		m.Meta.StoreRef = refB
		m.Meta.SourceNzbPath = refB
		if err := msB.WriteFileMetadata(m.VirtualPath, m.Meta); err != nil {
			t.Fatalf("node B WriteFileMetadata %s: %v", m.VirtualPath, err)
		}
	}

	// --- Verify: B reconstructs the exact segments from its own store ----------
	for i, vp := range vpaths {
		got, err := msB.ReadFileMetadata(vp)
		if err != nil {
			t.Fatalf("node B ReadFileMetadata %s: %v", vp, err)
		}
		if got == nil {
			t.Fatalf("node B has no metadata at %s", vp)
		}
		if got.StoreRef != refB {
			t.Fatalf("%s StoreRef = %q; want local %q", vp, got.StoreRef, refB)
		}
		want := nzb.Files[i].Segments
		if len(got.SegmentData) != len(want) {
			t.Fatalf("%s: resolved %d segments; want %d", vp, len(got.SegmentData), len(want))
		}
		for j, seg := range got.SegmentData {
			if seg.Id != want[j].ID {
				t.Errorf("%s seg %d: id = %q; want %q", vp, j, seg.Id, want[j].ID)
			}
			if seg.SegmentSize != int64(want[j].Bytes) {
				t.Errorf("%s seg %d: size = %d; want %d", vp, j, seg.SegmentSize, want[j].Bytes)
			}
		}
	}
}

func segData(f nzbparser.NzbFile) []*metapb.SegmentData {
	out := make([]*metapb.SegmentData, len(f.Segments))
	for i, s := range f.Segments {
		out[i] = &metapb.SegmentData{
			Id:          s.ID,
			SegmentSize: int64(s.Bytes),
			StartOffset: 0,
			EndOffset:   int64(s.Bytes) - 1,
		}
	}
	return out
}

func fileSize(f nzbparser.NzbFile) int64 {
	var total int64
	for _, s := range f.Segments {
		total += int64(s.Bytes)
	}
	return total
}
