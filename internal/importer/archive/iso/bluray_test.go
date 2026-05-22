package iso

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// mkEntry builds a single-extent isoFileEntry — the common case for tests.
func mkEntry(path string, lba uint32, size uint64) isoFileEntry {
	return isoFileEntry{
		path:    path,
		size:    size,
		extents: []isoExtent{{lba: lba, length: size}},
	}
}

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
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(short))),
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 110, uint64(len(long))),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
			mkEntry("BDMV/STREAM/00002.M2TS", 300, 2_000_000),
			mkEntry("BDMV/STREAM/00003.M2TS", 400, 3_000_000),
			mkEntry("BDMV/STREAM/00010.M2TS", 500, 500_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files)
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
			mkEntry("movie.mkv", 100, 1_000_000),
		}
		if got := ResolveMainFeature(context.Background(), bytes.NewReader(make([]byte, 16*iso9660SectorSize)), files); got != nil {
			t.Errorf("expected nil for non-BDMV disc, got %+v", got)
		}
	})

	t.Run("BDMV with no parseable MPLS returns nil", func(t *testing.T) {
		t.Parallel()
		rs := makeImage(t, map[uint32][]byte{
			100: []byte("not a real mpls"),
		})
		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, 15),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
		}
		if got := ResolveMainFeature(context.Background(), rs, files); got != nil {
			t.Errorf("expected nil for unparseable MPLS, got %+v", got)
		}
	})

	t.Run("3D BD: playlist resolves against SSIF when M2TS missing", func(t *testing.T) {
		t.Parallel()
		// Avatar-2-style 3D-only release: BDMV/STREAM/*.M2TS holds only
		// extras (tiny). The real main feature lives in BDMV/STREAM/SSIF/
		// and is referenced by its own MPLS. The resolver must index SSIF
		// so the long playlist resolves and wins.
		extras := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00010", InTime: 0, OutTime: 90 * 45000}, // 90s extra
		}, nil)
		mainFeature3D := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 60 * 60 * 45000},
			{ClipName: "00101", InTime: 0, OutTime: 60 * 60 * 45000},
			{ClipName: "00102", InTime: 0, OutTime: 12 * 60 * 45000}, // 132 min total
		}, nil)

		rs := makeImage(t, map[uint32][]byte{
			100: extras,
			110: mainFeature3D,
		})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(extras))),
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 110, uint64(len(mainFeature3D))),
			// Only the extras live as M2TS:
			mkEntry("BDMV/STREAM/00010.M2TS", 200, 50_000_000),
			// Main feature is SSIF only:
			mkEntry("BDMV/STREAM/SSIF/00100.SSIF", 300, 25_000_000_000),
			mkEntry("BDMV/STREAM/SSIF/00101.SSIF", 400, 25_000_000_000),
			mkEntry("BDMV/STREAM/SSIF/00102.SSIF", 500, 5_000_000_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil — SSIF index missing?")
		}
		if got.PlaylistName != "BDMV/PLAYLIST/00800.MPLS" {
			t.Errorf("PlaylistName = %q, want 00800.MPLS (3D main feature)", got.PlaylistName)
		}
		if len(got.Streams) != 3 {
			t.Fatalf("Streams len = %d, want 3 SSIF clips", len(got.Streams))
		}
		wantOrder := []string{
			"BDMV/STREAM/SSIF/00100.SSIF",
			"BDMV/STREAM/SSIF/00101.SSIF",
			"BDMV/STREAM/SSIF/00102.SSIF",
		}
		for i, s := range got.Streams {
			if s.path != wantOrder[i] {
				t.Errorf("Streams[%d].path = %q, want %q", i, s.path, wantOrder[i])
			}
		}
	})

	t.Run("hybrid 3D BD: prefers M2TS over SSIF when both exist", func(t *testing.T) {
		t.Parallel()
		// Both 2D MPLS (refs M2TS) and 3D MPLS (refs SSIF) point at clips
		// of the same name. With both files present, the M2TS version is
		// the right pick: smaller bytes, universal playback. The resolver
		// should select it even if the 3D playlist is marginally longer.
		mainFeature := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 60 * 60 * 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{100: mainFeature})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 100, uint64(len(mainFeature))),
			mkEntry("BDMV/STREAM/00100.M2TS", 200, 20_000_000_000),
			mkEntry("BDMV/STREAM/SSIF/00100.SSIF", 300, 40_000_000_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}
		if len(got.Streams) != 1 {
			t.Fatalf("Streams len = %d, want 1", len(got.Streams))
		}
		if got.Streams[0].path != "BDMV/STREAM/00100.M2TS" {
			t.Errorf("picked %q, want M2TS over SSIF", got.Streams[0].path)
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
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(data))),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
		}
		if got := ResolveMainFeature(context.Background(), rs, files); got != nil {
			t.Errorf("expected nil when MPLS references unknown clip, got %+v", got)
		}
	})
}
