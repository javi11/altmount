package mediaprobe

import (
	"context"
	"math"
	"testing"
)

func TestProbeMP4Layouts(t *testing.T) {
	ftyp := mp4Box_("ftyp", []byte("isomiso2"))
	moov := mp4Box_("moov", mp4Box_("mvhd", mvhdV0(1000, 7_200_000)))
	mdat := mp4Box_("mdat", make([]byte, 4096))

	tests := []struct {
		name        string
		data        []byte
		wantDur     float64
		moovLast    bool
		wantErr     bool
		largeMdat   bool
		wantPayload string
	}{
		{name: "faststart", data: buildMP4(ftyp, moov, mdat), wantDur: 7200},
		{name: "moov at end", data: buildMP4(ftyp, mdat, moov), wantDur: 7200, moovLast: true},
		{name: "with free box", data: buildMP4(ftyp, moov, mp4Box_("free", make([]byte, 64)), mdat), wantDur: 7200},
		{name: "largesize mdat", data: buildMP4(ftyp, moov, mp4LargeBox("mdat", make([]byte, 4096))), wantDur: 7200, largeMdat: true},
		{name: "mvhd v1", data: buildMP4(ftyp, mp4Box_("moov", mp4Box_("mvhd", mvhdV1(90000, 90000*3600))), mdat), wantDur: 3600},
		{name: "garbage header", data: []byte{0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, wantErr: true},
		{name: "no moov", data: buildMP4(ftyp, mdat), wantErr: true},
		{name: "box exceeds file", data: buildMP4(ftyp, moov)[:20], wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &trackingReaderAt{data: tt.data}
			s, err := Probe(context.Background(), r, int64(len(tt.data)), "movie.mp4")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got structure %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatalf("Probe failed: %v", err)
			}
			if s.Container != "mp4" {
				t.Errorf("container = %q, want mp4", s.Container)
			}
			if math.Abs(s.DurationSeconds-tt.wantDur) > 0.01 {
				t.Errorf("duration = %f, want %f", s.DurationSeconds, tt.wantDur)
			}
			findRange(t, s.Critical, "ftyp")
			findRange(t, s.Critical, "moov")
			mdatRange := findRange(t, s.Payload, "mdat")

			// The walk must never read media payload: assert no read
			// touched mdat's interior (beyond its 16-byte header).
			headerLen := int64(8)
			if tt.largeMdat {
				headerLen = 16
			}
			r.assertNoReadIn(t, ByteRange{Start: mdatRange.Start + headerLen, End: mdatRange.End}, "mdat payload")
			if total := r.totalBytesRead(); total > 4096 {
				t.Errorf("probe read %d bytes, budget is 4096", total)
			}
		})
	}
}

func TestClassifyMP4(t *testing.T) {
	ftyp := mp4Box_("ftyp", []byte("isomiso2"))
	moov := mp4Box_("moov", mp4Box_("mvhd", mvhdV0(1000, 3_600_000))) // 1h
	mdat := mp4Box_("mdat", make([]byte, 8192))

	fastStart := buildMP4(ftyp, moov, mdat)
	moovAtEnd := buildMP4(ftyp, mdat, moov)

	probe := func(t *testing.T, data []byte) *Structure {
		t.Helper()
		s, err := Probe(context.Background(), &trackingReaderAt{data: data}, int64(len(data)), "movie.mp4")
		if err != nil {
			t.Fatalf("Probe failed: %v", err)
		}
		return s
	}

	t.Run("missing mdat interior is degraded", func(t *testing.T) {
		s := probe(t, fastStart)
		mdatRange := findRange(t, s.Payload, "mdat")
		mid := (mdatRange.Start + mdatRange.End) / 2
		cls := ClassifyAgainst(s, []ByteRange{{Start: mid, End: mid + 100}})
		if cls.Verdict != VerdictDegraded {
			t.Fatalf("verdict = %s (%s), want degraded", cls.Verdict, cls.Reason)
		}
		if len(cls.AffectedTime) != 1 {
			t.Fatalf("affected time = %+v, want one window", cls.AffectedTime)
		}
		if cls.AffectedTime[0].FromSec <= 0 || cls.AffectedTime[0].ToSec > 3600 {
			t.Errorf("affected window out of bounds: %+v", cls.AffectedTime[0])
		}
	})

	t.Run("missing moov is fatal", func(t *testing.T) {
		s := probe(t, fastStart)
		moovRange := findRange(t, s.Critical, "moov")
		cls := ClassifyAgainst(s, []ByteRange{{Start: moovRange.Start + 4, End: moovRange.Start + 200}})
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s (%s), want fatal", cls.Verdict, cls.Reason)
		}
	})

	t.Run("missing ftyp is fatal", func(t *testing.T) {
		s := probe(t, fastStart)
		cls := ClassifyAgainst(s, []ByteRange{{Start: 0, End: 7}})
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s, want fatal", cls.Verdict)
		}
	})

	t.Run("moov-at-end tail loss is fatal", func(t *testing.T) {
		s := probe(t, moovAtEnd)
		cls := ClassifyAgainst(s, []ByteRange{{Start: int64(len(moovAtEnd)) - 50, End: int64(len(moovAtEnd)) - 1}})
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s, want fatal", cls.Verdict)
		}
	})

	t.Run("live classify with missing moov segment is unknown", func(t *testing.T) {
		// moov-at-end file where the missing range covers moov: the live
		// probe cannot read the moov header → unknown, not degraded.
		s := probe(t, moovAtEnd)
		moovRange := findRange(t, s.Critical, "moov")
		r := &trackingReaderAt{data: moovAtEnd, missing: []ByteRange{moovRange}}
		cls := Classify(context.Background(), r, int64(len(moovAtEnd)), "movie.mp4", []ByteRange{moovRange})
		if cls.Verdict != VerdictUnknown {
			t.Fatalf("verdict = %s (%s), want unknown", cls.Verdict, cls.Reason)
		}
	})

	t.Run("too many missing ranges is fatal", func(t *testing.T) {
		s := probe(t, fastStart)
		mdatRange := findRange(t, s.Payload, "mdat")
		var missing []ByteRange
		for i := int64(0); i < maxMissingForDegraded+1; i++ {
			off := mdatRange.Start + 16 + i*100
			missing = append(missing, ByteRange{Start: off, End: off + 10})
		}
		cls := ClassifyAgainst(s, missing)
		if cls.Verdict != VerdictFatal {
			t.Fatalf("verdict = %s, want fatal", cls.Verdict)
		}
	})

	t.Run("non-video extension performs zero reads", func(t *testing.T) {
		r := &trackingReaderAt{data: fastStart}
		cls := Classify(context.Background(), r, int64(len(fastStart)), "archive.rar", []ByteRange{{Start: 0, End: 10}})
		if cls.Verdict != VerdictUnknown {
			t.Fatalf("verdict = %s, want unknown", cls.Verdict)
		}
		if len(r.reads) != 0 {
			t.Fatalf("expected zero reads for non-video file, got %d", len(r.reads))
		}
	})

	t.Run("obfuscated non-mp4 content is unknown", func(t *testing.T) {
		data := append([]byte("Rar!\x1a\x07\x01\x00"), make([]byte, 100)...)
		cls := Classify(context.Background(), &trackingReaderAt{data: data}, int64(len(data)), "movie.mp4", []ByteRange{{Start: 50, End: 60}})
		if cls.Verdict != VerdictUnknown {
			t.Fatalf("verdict = %s, want unknown", cls.Verdict)
		}
	})
}
