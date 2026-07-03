package metadata

import (
	"testing"

	"github.com/javi11/altmount/internal/mediaprobe"
)

// TestMediaStructureRoundTrip proves a persisted structure classifies
// identically to the live-probed one.
func TestMediaStructureRoundTrip(t *testing.T) {
	orig := &mediaprobe.Structure{
		Container:       "mp4",
		FileSize:        10_000,
		DurationSeconds: 3600.5,
		Critical: []mediaprobe.ByteRange{
			{Start: 0, End: 31, Label: "ftyp"},
			{Start: 32, End: 1023, Label: "moov"},
		},
		Payload:  []mediaprobe.ByteRange{{Start: 1024, End: 9999, Label: "mdat"}},
		SeekOnly: []mediaprobe.ByteRange{{Start: 500, End: 600, Label: "sidx"}},
	}

	back := MediaStructureFromProto(MediaStructureToProto(orig), orig.FileSize)
	if back == nil {
		t.Fatal("round trip returned nil")
	}

	cases := [][]mediaprobe.ByteRange{
		{{Start: 100, End: 200}},   // inside moov → fatal
		{{Start: 2000, End: 2100}}, // inside mdat → degraded
	}
	for _, missing := range cases {
		a := mediaprobe.ClassifyAgainst(orig, missing)
		b := mediaprobe.ClassifyAgainst(back, missing)
		if a.Verdict != b.Verdict || a.Reason != b.Reason {
			t.Errorf("classification diverged for %+v: live=%s(%s) stored=%s(%s)",
				missing, a.Verdict, a.Reason, b.Verdict, b.Reason)
		}
	}

	if back.DurationSeconds != orig.DurationSeconds {
		t.Errorf("duration = %f, want %f", back.DurationSeconds, orig.DurationSeconds)
	}
	if len(back.Critical) != 2 || back.Critical[1].Label != "moov" {
		t.Errorf("critical ranges lost in round trip: %+v", back.Critical)
	}
}

func TestMediaStructureNilSafety(t *testing.T) {
	if MediaStructureToProto(nil) != nil {
		t.Error("MediaStructureToProto(nil) should be nil")
	}
	if MediaStructureFromProto(nil, 100) != nil {
		t.Error("MediaStructureFromProto(nil) should be nil")
	}
}
