package iso

import (
	"encoding/binary"
	"testing"
)

// buildMPLS constructs a synthetic .mpls byte stream containing the given
// PlayItems. Each PlayItem is laid out at its minimum legal size (20 bytes
// body + 2-byte length prefix). multiAngleTail, when non-nil, is appended
// inside the first PlayItem to exercise the length-prefixed skip logic.
func buildMPLS(t *testing.T, version string, items []MPLSPlayItem, multiAngleTail []byte) []byte {
	t.Helper()
	if len(version) != 4 {
		t.Fatalf("version must be 4 bytes, got %q", version)
	}

	// Build PlayItems body.
	var playItemsBuf []byte
	for i, it := range items {
		if len(it.ClipName) != 5 {
			t.Fatalf("item %d: ClipName must be 5 chars", i)
		}
		body := make([]byte, 20)
		copy(body[0:5], it.ClipName)
		copy(body[5:9], "M2TS")
		// flags (2) + ref_to_STC_id (1) left zero
		binary.BigEndian.PutUint32(body[12:16], it.InTime)
		binary.BigEndian.PutUint32(body[16:20], it.OutTime)
		// Inject the multi-angle tail into the first item only — the parser
		// must skip past it via the length field without misaligning the
		// next item.
		if i == 0 && multiAngleTail != nil {
			body = append(body, multiAngleTail...)
		}
		// PlayItem length excludes its own 2-byte length prefix.
		lenPrefix := make([]byte, 2)
		binary.BigEndian.PutUint16(lenPrefix, uint16(len(body)))
		playItemsBuf = append(playItemsBuf, lenPrefix...)
		playItemsBuf = append(playItemsBuf, body...)
	}

	// PlayList header: length(4)+reserved(2)+numPI(2)+numSub(2)+playItems
	plHeader := make([]byte, 10)
	// length excludes its own 4-byte field
	binary.BigEndian.PutUint32(plHeader[0:4], uint32(6+len(playItemsBuf)))
	binary.BigEndian.PutUint16(plHeader[6:8], uint16(len(items)))
	// numSubPaths left zero

	playList := append(plHeader, playItemsBuf...)

	// File header: 4 magic + 4 version + 4 PL offset + 4 PLMark + 4 ExtData
	hdr := make([]byte, mplsHeaderSize)
	copy(hdr[0:4], "MPLS")
	copy(hdr[4:8], version)
	binary.BigEndian.PutUint32(hdr[8:12], uint32(mplsHeaderSize))
	// PlayListMark & ExtensionData offsets unused; leave zero.

	return append(hdr, playList...)
}

func TestParseMPLS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      []byte
		wantErr   bool
		wantItems []MPLSPlayItem
		wantTicks int64
	}{
		{
			name: "single PlayItem",
			data: buildMPLS(t, "0200", []MPLSPlayItem{
				{ClipName: "00001", InTime: 1000, OutTime: 91000},
			}, nil),
			wantItems: []MPLSPlayItem{{ClipName: "00001", InTime: 1000, OutTime: 91000}},
			wantTicks: 90000, // 2s at 45kHz
		},
		{
			name: "five PlayItems (main feature shape)",
			data: buildMPLS(t, "0200", []MPLSPlayItem{
				{ClipName: "00001", InTime: 0, OutTime: 45000},
				{ClipName: "00002", InTime: 0, OutTime: 45000},
				{ClipName: "00003", InTime: 0, OutTime: 45000},
				{ClipName: "00004", InTime: 0, OutTime: 45000},
				{ClipName: "00005", InTime: 0, OutTime: 45000},
			}, nil),
			wantItems: []MPLSPlayItem{
				{ClipName: "00001", InTime: 0, OutTime: 45000},
				{ClipName: "00002", InTime: 0, OutTime: 45000},
				{ClipName: "00003", InTime: 0, OutTime: 45000},
				{ClipName: "00004", InTime: 0, OutTime: 45000},
				{ClipName: "00005", InTime: 0, OutTime: 45000},
			},
			wantTicks: 5 * 45000,
		},
		{
			name: "multi-angle PlayItem (tail must be skipped)",
			// The tail simulates angle-count + alt-angle records appended
			// after the fixed PlayItem prefix. The parser only consumes the
			// first 20 bytes and uses the length field to skip past the
			// rest, so item 2 must still parse cleanly.
			data: buildMPLS(t, "0200", []MPLSPlayItem{
				{ClipName: "00001", InTime: 0, OutTime: 45000},
				{ClipName: "00002", InTime: 0, OutTime: 90000},
			}, []byte{
				0x02,                                              // num_angles
				0x00,                                              // is_different_audios flags
				'0', '0', '0', '0', '7', 'M', '2', 'T', 'S', 0x00, // one alt angle entry (10 bytes)
			}),
			wantItems: []MPLSPlayItem{
				{ClipName: "00001", InTime: 0, OutTime: 45000},
				{ClipName: "00002", InTime: 0, OutTime: 90000},
			},
			wantTicks: 45000 + 90000,
		},
		{
			name:    "wrong magic",
			data:    []byte("NOTMPLS-padding-here-padding-here"),
			wantErr: true,
		},
		{
			name:    "truncated header",
			data:    []byte("MPLS"),
			wantErr: true,
		},
		{
			name: "PlayList offset out of range",
			data: func() []byte {
				b := make([]byte, mplsHeaderSize)
				copy(b[0:4], "MPLS")
				copy(b[4:8], "0200")
				binary.BigEndian.PutUint32(b[8:12], 9999)
				return b
			}(),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMPLS(tc.data)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.PlayItems) != len(tc.wantItems) {
				t.Fatalf("PlayItems len = %d, want %d", len(got.PlayItems), len(tc.wantItems))
			}
			for i, it := range got.PlayItems {
				if it != tc.wantItems[i] {
					t.Errorf("PlayItem[%d] = %+v, want %+v", i, it, tc.wantItems[i])
				}
			}
			if d := got.DurationTicks(); d != tc.wantTicks {
				t.Errorf("DurationTicks = %d, want %d", d, tc.wantTicks)
			}
		})
	}
}
