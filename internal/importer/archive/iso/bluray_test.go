package iso

import (
	"bytes"
	"io"
	"testing"
)

// makeImage assembles an in-memory disc image by placing each piece of
// data at the sector index given in its key. The returned reader can be
// used as if it were a real ISO read-seeker.
func makeImage(t *testing.T, pieces map[uint32][]byte) io.ReadSeeker {
	t.Helper()
	var maxSect uint32
	for s, b := range pieces {
		end := s + uint32((len(b)+iso9660SectorSize-1)/iso9660SectorSize)
		if end > maxSect {
			maxSect = end
		}
	}
	if maxSect == 0 {
		maxSect = 1
	}
	img := make([]byte, int(maxSect)*iso9660SectorSize)
	for s, b := range pieces {
		copy(img[int(s)*iso9660SectorSize:], b)
	}
	return bytes.NewReader(img)
}

func TestResolveMainFeature(t *testing.T) {
	t.Parallel()

	t.Run("picks longest playlist", func(t *testing.T) {
		t.Parallel()
		// Two playlists:
		//   00001.MPLS  → 1 clip, short (extras playlist)
		//   00800.MPLS  → 3 clips, long  (main feature)
		short := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00010", InTime: 0, OutTime: 45000},
		}, nil)
		long := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00001", InTime: 0, OutTime: 90 * 45000},
			{ClipName: "00002", InTime: 0, OutTime: 60 * 45000},
			{ClipName: "00003", InTime: 0, OutTime: 30 * 45000},
		}, nil)

		rs := makeImage(t, map[uint32][]byte{
			100: short,
			110: long,
		})

		// File listing: two playlists and four M2TS clips (one extra).
		files := []isoFileEntry{
			{path: "BDMV/PLAYLIST/00001.MPLS", lba: 100, size: uint64(len(short))},
			{path: "BDMV/PLAYLIST/00800.MPLS", lba: 110, size: uint64(len(long))},
			{path: "BDMV/STREAM/00001.M2TS", lba: 200, size: 1_000_000},
			{path: "BDMV/STREAM/00002.M2TS", lba: 300, size: 2_000_000},
			{path: "BDMV/STREAM/00003.M2TS", lba: 400, size: 3_000_000},
			{path: "BDMV/STREAM/00010.M2TS", lba: 500, size: 500_000},
		}

		got := ResolveMainFeature(rs, files)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}
		if got.PlaylistName != "BDMV/PLAYLIST/00800.MPLS" {
			t.Errorf("PlaylistName = %q, want 00800.MPLS", got.PlaylistName)
		}
		if len(got.Streams) != 3 {
			t.Fatalf("Streams len = %d, want 3", len(got.Streams))
		}
		wantOrder := []string{"BDMV/STREAM/00001.M2TS", "BDMV/STREAM/00002.M2TS", "BDMV/STREAM/00003.M2TS"}
		for i, s := range got.Streams {
			if s.path != wantOrder[i] {
				t.Errorf("Streams[%d].path = %q, want %q", i, s.path, wantOrder[i])
			}
		}
	})

	t.Run("non-BDMV disc returns nil", func(t *testing.T) {
		t.Parallel()
		files := []isoFileEntry{
			{path: "movie.mkv", lba: 100, size: 1_000_000},
		}
		if got := ResolveMainFeature(bytes.NewReader(make([]byte, 16*iso9660SectorSize)), files); got != nil {
			t.Errorf("expected nil for non-BDMV disc, got %+v", got)
		}
	})

	t.Run("BDMV with no parseable MPLS returns nil", func(t *testing.T) {
		t.Parallel()
		rs := makeImage(t, map[uint32][]byte{
			100: []byte("not a real mpls"),
		})
		files := []isoFileEntry{
			{path: "BDMV/PLAYLIST/00001.MPLS", lba: 100, size: 15},
			{path: "BDMV/STREAM/00001.M2TS", lba: 200, size: 1_000_000},
		}
		if got := ResolveMainFeature(rs, files); got != nil {
			t.Errorf("expected nil for unparseable MPLS, got %+v", got)
		}
	})

	t.Run("playlist referencing missing M2TS yields nil", func(t *testing.T) {
		t.Parallel()
		// Playlist references a clip that has no corresponding M2TS entry.
		data := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "99999", InTime: 0, OutTime: 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{
			100: data,
		})
		files := []isoFileEntry{
			{path: "BDMV/PLAYLIST/00001.MPLS", lba: 100, size: uint64(len(data))},
			{path: "BDMV/STREAM/00001.M2TS", lba: 200, size: 1_000_000},
		}
		if got := ResolveMainFeature(rs, files); got != nil {
			t.Errorf("expected nil when MPLS references unknown clip, got %+v", got)
		}
	})
}
