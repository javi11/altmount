package mediaprobe

import (
	"context"
	"math"
	"testing"
)

func TestProbeMKVLayouts(t *testing.T) {
	seekHead := mkvEl(mkvSeekHeadID, make([]byte, 32))
	info := mkvInfo(5400) // 90 min
	tracks := mkvEl(mkvTracksID, make([]byte, 128))
	cues := mkvEl(mkvCuesID, make([]byte, 256))
	cluster := func(n int) []byte { return mkvEl(mkvClusterID, make([]byte, n)) }

	tests := []struct {
		name    string
		data    []byte
		wantDur float64
		wantErr bool
	}{
		{
			name:    "typical layout",
			data:    buildMKV(seekHead, info, tracks, cluster(2048), cluster(2048), cues),
			wantDur: 5400,
		},
		{
			name:    "cues before clusters",
			data:    buildMKV(info, tracks, cues, cluster(4096)),
			wantDur: 5400,
		},
		{
			name: "unknown-size segment",
			data: append(
				mkvEl(ebmlHeaderID, make([]byte, 20)),
				mkvElUnknownSize(mkvSegmentID, buildMKVChildren(info, tracks, cluster(2048)))...),
			wantDur: 5400,
		},
		{name: "garbage header", data: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, wantErr: true},
		{name: "missing tracks", data: buildMKV(info, cluster(1024)), wantErr: true},
		{name: "truncated", data: buildMKV(info, tracks, cluster(2048))[:40], wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &trackingReaderAt{data: tt.data}
			s, err := Probe(context.Background(), r, int64(len(tt.data)), "movie.mkv")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got structure %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatalf("Probe failed: %v", err)
			}
			if s.Container != "mkv" {
				t.Errorf("container = %q, want mkv", s.Container)
			}
			if math.Abs(s.DurationSeconds-tt.wantDur) > 0.01 {
				t.Errorf("duration = %f, want %f", s.DurationSeconds, tt.wantDur)
			}
			findRange(t, s.Critical, "EBML header")
			findRange(t, s.Critical, "Info")
			findRange(t, s.Critical, "Tracks")

			// Cluster interiors are media payload; the walk may read
			// element headers but never descends into cluster data.
			if total := r.totalBytesRead(); total > 4096 {
				t.Errorf("probe read %d bytes, budget is 4096: %s", total, fmtRanges(s.Payload))
			}
		})
	}
}

// buildMKVChildren concatenates segment children without wrapping them.
func buildMKVChildren(children ...[]byte) []byte {
	var out []byte
	for _, c := range children {
		out = append(out, c...)
	}
	return out
}

func TestClassifyMKV(t *testing.T) {
	info := mkvInfo(3600)
	tracks := mkvEl(mkvTracksID, make([]byte, 128))
	cues := mkvEl(mkvCuesID, make([]byte, 256))
	cluster := mkvEl(mkvClusterID, make([]byte, 8192))
	data := buildMKV(info, tracks, cluster, cues)

	s, err := Probe(context.Background(), &trackingReaderAt{data: data}, int64(len(data)), "movie.mkv")
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	clusters := findRange(t, s.Payload, "Clusters")
	tracksRange := findRange(t, s.Critical, "Tracks")
	infoRange := findRange(t, s.Critical, "Info")

	t.Run("missing cluster interior is degraded", func(t *testing.T) {
		mid := (clusters.Start + clusters.End) / 2
		cls := ClassifyAgainst(s, []ByteRange{{Start: mid, End: mid + 512}})
		if cls.Verdict != VerdictDegraded {
			t.Fatalf("verdict = %s (%s), want degraded", cls.Verdict, cls.Reason)
		}
		if len(cls.AffectedTime) == 0 {
			t.Errorf("expected an affected time window")
		}
	})

	t.Run("missing tracks is fatal", func(t *testing.T) {
		cls := ClassifyAgainst(s, []ByteRange{{Start: tracksRange.Start, End: tracksRange.Start + 10}})
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s (%s), want fatal", cls.Verdict, cls.Reason)
		}
	})

	t.Run("missing info is fatal", func(t *testing.T) {
		cls := ClassifyAgainst(s, []ByteRange{{Start: infoRange.Start + 2, End: infoRange.Start + 6}})
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s, want fatal", cls.Verdict)
		}
	})

	t.Run("missing EBML header is fatal", func(t *testing.T) {
		cls := ClassifyAgainst(s, []ByteRange{{Start: 0, End: 3}})
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s, want fatal", cls.Verdict)
		}
	})

	t.Run("cues within cluster tail space is degraded", func(t *testing.T) {
		// In this layout Cues follow the first Cluster, so they live inside
		// the "Clusters" tail range — degraded either way.
		cls := ClassifyAgainst(s, []ByteRange{{Start: clusters.End - 100, End: clusters.End}})
		if cls.Verdict != VerdictDegraded {
			t.Fatalf("verdict = %s (%s), want degraded", cls.Verdict, cls.Reason)
		}
	})

	t.Run("cues-only loss reports seek-only reason", func(t *testing.T) {
		// Layout with Cues before the first Cluster so they get their own
		// SeekOnly range.
		d := buildMKV(info, tracks, cues, cluster)
		st, err := Probe(context.Background(), &trackingReaderAt{data: d}, int64(len(d)), "movie.mkv")
		if err != nil {
			t.Fatalf("Probe failed: %v", err)
		}
		cuesRange := findRange(t, st.SeekOnly, "Cues")
		cls := ClassifyAgainst(st, []ByteRange{{Start: cuesRange.Start + 5, End: cuesRange.Start + 50}})
		if cls.Verdict != VerdictDegraded {
			t.Fatalf("verdict = %s (%s), want degraded", cls.Verdict, cls.Reason)
		}
		if len(cls.AffectedTime) != 0 {
			t.Errorf("seek-only loss should have no affected playback window, got %+v", cls.AffectedTime)
		}
	})

	t.Run("webm extension uses mkv parser", func(t *testing.T) {
		st, err := Probe(context.Background(), &trackingReaderAt{data: data}, int64(len(data)), "clip.webm")
		if err != nil {
			t.Fatalf("Probe failed for webm: %v", err)
		}
		if st.Container != "mkv" {
			t.Errorf("container = %q, want mkv", st.Container)
		}
	})
}

func TestClassifyAgainstEdgeCases(t *testing.T) {
	s := &Structure{
		Container: "mp4",
		FileSize:  1000,
		Critical:  []ByteRange{{Start: 0, End: 99, Label: "moov"}},
		Payload:   []ByteRange{{Start: 100, End: 999, Label: "mdat"}},
	}

	t.Run("nil structure", func(t *testing.T) {
		cls := ClassifyAgainst(nil, []ByteRange{{Start: 0, End: 1}})
		if cls.Verdict != VerdictUnknown {
			t.Fatalf("verdict = %s, want unknown", cls.Verdict)
		}
	})

	t.Run("no missing ranges", func(t *testing.T) {
		cls := ClassifyAgainst(s, nil)
		if cls.Verdict != VerdictUnknown {
			t.Fatalf("verdict = %s, want unknown", cls.Verdict)
		}
	})

	t.Run("range outside mapped structure", func(t *testing.T) {
		gap := &Structure{
			Container: "mp4",
			FileSize:  1000,
			Critical:  []ByteRange{{Start: 0, End: 99}},
			Payload:   []ByteRange{{Start: 200, End: 999}}, // 100-199 unmapped
		}
		cls := ClassifyAgainst(gap, []ByteRange{{Start: 150, End: 160}})
		if cls.Verdict != VerdictUnknown {
			t.Fatalf("verdict = %s, want unknown", cls.Verdict)
		}
	})

	t.Run("range spanning payload boundary stays degraded", func(t *testing.T) {
		multi := &Structure{
			Container: "mp4",
			FileSize:  1000,
			Critical:  []ByteRange{{Start: 0, End: 99}},
			Payload:   []ByteRange{{Start: 100, End: 499}, {Start: 500, End: 999}},
		}
		cls := ClassifyAgainst(multi, []ByteRange{{Start: 450, End: 550}})
		if cls.Verdict != VerdictDegraded {
			t.Fatalf("verdict = %s (%s), want degraded", cls.Verdict, cls.Reason)
		}
	})

	t.Run("range beyond file size is clamped", func(t *testing.T) {
		cls := ClassifyAgainst(s, []ByteRange{{Start: 500, End: 5000}})
		if cls.Verdict != VerdictDegraded {
			t.Fatalf("verdict = %s (%s), want degraded", cls.Verdict, cls.Reason)
		}
	})
}
