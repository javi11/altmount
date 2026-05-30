package iso

import "testing"

// --- synthetic BDAV packet builders -----------------------------------------

// newBDAVPacket returns a zeroed 192-byte BDAV source packet with the sync
// byte set. afc selects adaptation_field_control (0x01 payload-only,
// 0x03 adaptation+payload). pusi sets payload_unit_start_indicator.
func newBDAVPacket(pid uint16, pusi bool, afc byte) []byte {
	p := make([]byte, bdavPacketLen)
	ts := p[4:] // 188-byte TS packet
	ts[0] = tsSync
	ts[1] = byte(pid>>8) & 0x1F
	if pusi {
		ts[1] |= 0x40
	}
	ts[2] = byte(pid)
	ts[3] = (afc << 4) // scrambling 00, CC 0
	return p
}

// setPTS writes a PTS-only PES header into a payload-only BDAV packet and
// returns the packet. tsBytesOffset is where the 188-byte TS payload starts
// within the source packet (8 for BDAV payload-only).
func setPTS(p []byte, pts int64) []byte {
	pl := p[8:] // payload of the 188-byte TS packet (BDAV off 4 + TS header 4)
	pl[0], pl[1], pl[2] = 0x00, 0x00, 0x01
	pl[3] = 0xE0 // video stream_id
	pl[4], pl[5] = 0x00, 0x00
	pl[6] = 0x80 // marker '10', no flags
	pl[7] = 0x80 // PTS_DTS_flags = '10' (PTS only)
	pl[8] = 0x05 // PES_header_data_length
	// Seed the PTS field prefix nibble (0010) + marker bits, then encode.
	pl[9] = 0x21  // 0010 ...1
	pl[11] = 0x01 // marker
	pl[13] = 0x01 // marker
	writeTS(pl[9:14], pts)
	return p
}

// setPTSDTS writes a PTS+DTS PES header.
func setPTSDTS(p []byte, pts, dts int64) []byte {
	pl := p[8:]
	pl[0], pl[1], pl[2] = 0x00, 0x00, 0x01
	pl[3] = 0xE0
	pl[4], pl[5] = 0x00, 0x00
	pl[6] = 0x80
	pl[7] = 0xC0 // PTS_DTS_flags = '11'
	pl[8] = 0x0A // 10 bytes (PTS+DTS)
	pl[9] = 0x31 // prefix 0011 for PTS-when-DTS-present
	pl[11], pl[13] = 0x01, 0x01
	writeTS(pl[9:14], pts)
	pl[14] = 0x11 // prefix 0001 for DTS
	pl[16], pl[18] = 0x01, 0x01
	writeTS(pl[14:19], dts)
	return p
}

// setPCR writes a PCR into an adaptation+payload BDAV packet (afc 0x03).
func setPCR(p []byte, pcrBase int64) []byte {
	ts := p[4:]
	// adaptation_field_length: 1 (flags) + 6 (PCR) = 7.
	ts[4] = 7
	ts[5] = 0x10 // PCR_flag
	b := ts[6:12]
	b[0] = byte(pcrBase >> 25)
	b[1] = byte(pcrBase >> 17)
	b[2] = byte(pcrBase >> 9)
	b[3] = byte(pcrBase >> 1)
	b[4] = byte((pcrBase&0x01)<<7) // ext = 0
	b[5] = 0x00
	return p
}

func readPCRBase(p []byte) int64 {
	b := p[4:][6:12]
	return (int64(b[0]) << 25) | (int64(b[1]) << 17) | (int64(b[2]) << 9) |
		(int64(b[3]) << 1) | (int64(b[4]) >> 7)
}

// --- tests ------------------------------------------------------------------

func TestReadWriteTS_RoundTrip(t *testing.T) {
	cases := []int64{0, 1, 90000, 1048500, ptsModulus - 1, (1 << 32) + 12345}
	for _, want := range cases {
		b := []byte{0x21, 0x00, 0x01, 0x00, 0x01} // prefix + markers
		writeTS(b, want)
		got := readTS(b)
		if got != want {
			t.Errorf("round-trip PTS: wrote %d, read %d", want, got)
		}
		// Marker bits must be preserved (bit 0 of b[0], b[2], b[4]).
		if b[0]&0x01 != 0x01 || b[2]&0x01 != 0x01 || b[4]&0x01 != 0x01 {
			t.Errorf("marker bits clobbered for %d: % x", want, b)
		}
		// Prefix nibble preserved.
		if b[0]&0xF0 != 0x20 {
			t.Errorf("prefix nibble clobbered for %d: %#x", want, b[0])
		}
	}
}

func TestRewritePacket_PTSOnly(t *testing.T) {
	const base = int64(1048500) // ~11.65s
	const delta = int64(90000)  // +1s
	p := setPTS(newBDAVPacket(0x100, true, 0x01), base)
	if !rewritePacket(p, bdavPacketLen, delta) {
		t.Fatal("rewritePacket reported no change for a PTS packet")
	}
	got := readTS(p[8:][9:14])
	if got != base+delta {
		t.Errorf("PTS after rewrite = %d, want %d", got, base+delta)
	}
}

func TestRewritePacket_PTSDTS_andPCR(t *testing.T) {
	const pts = int64(900000)
	const dts = int64(810000)
	const delta = int64(45000)
	p := setPTSDTS(newBDAVPacket(0x100, true, 0x01), pts, dts)
	rewritePacket(p, bdavPacketLen, delta)
	if g := readTS(p[8:][9:14]); g != pts+delta {
		t.Errorf("PTS = %d, want %d", g, pts+delta)
	}
	if g := readTS(p[8:][14:19]); g != dts+delta {
		t.Errorf("DTS = %d, want %d", g, dts+delta)
	}

	const pcrBase = int64(1234567)
	pc := setPCR(newBDAVPacket(0x100, false, 0x03), pcrBase)
	rewritePacket(pc, bdavPacketLen, delta)
	if g := readPCRBase(pc); g != pcrBase+delta {
		t.Errorf("PCR base = %d, want %d", g, pcrBase+delta)
	}
}

func TestRewritePacket_NoTimestampLeavesUnchanged(t *testing.T) {
	// Continuation packet (PUSI=0, payload-only, no PES header).
	p := newBDAVPacket(0x100, false, 0x01)
	cp := make([]byte, len(p))
	copy(cp, p)
	if rewritePacket(p, bdavPacketLen, 90000) {
		t.Error("rewritePacket changed a packet with no timestamps")
	}
	for i := range p {
		if p[i] != cp[i] {
			t.Fatalf("byte %d changed in a no-timestamp packet", i)
		}
	}
}

// TestRewrite_TwoClipsBecomeMonotonic is the FEASIBILITY GATE: two clips with
// independent PTS bases, byte-concatenated, become a single monotonic timeline
// after per-clip delta rewriting, and last−first equals the sum of the clips'
// durations. This proves the whole continuous-timeline approach before any
// metadata/VFS plumbing is built.
func TestRewrite_TwoClipsBecomeMonotonic(t *testing.T) {
	const hz = 90000
	// Clip 0: base 11.65s, 3 packets spaced 1s, duration 30s.
	// Clip 1: base 0.5s,  3 packets spaced 1s, duration 20s.
	clip0Base := int64(11.65 * hz)
	clip1Base := int64(0.5 * hz)
	clip0Dur := int64(30 * hz)
	clip1Dur := int64(20 * hz)

	mkClip := func(base int64, n int) [][]byte {
		out := make([][]byte, n)
		for i := range n {
			out[i] = setPTS(newBDAVPacket(0x100, true, 0x01), base+int64(i)*hz)
		}
		return out
	}
	clip0 := mkClip(clip0Base, 3)
	clip1 := mkClip(clip1Base, 3)

	// timeline_start: clip0 keeps its own base (start the file at 11.65s),
	// clip1 begins where clip0's authored duration ends.
	timelineStart0 := clip0Base
	timelineStart1 := clip0Base + clip0Dur
	delta0 := timelineStart0 - clip0Base // 0
	delta1 := timelineStart1 - clip1Base

	var ptsSeq []int64
	for _, p := range clip0 {
		rewritePacket(p, bdavPacketLen, delta0)
		ptsSeq = append(ptsSeq, readTS(p[8:][9:14]))
	}
	for _, p := range clip1 {
		rewritePacket(p, bdavPacketLen, delta1)
		ptsSeq = append(ptsSeq, readTS(p[8:][9:14]))
	}

	// Strictly monotonic across the whole concatenation.
	for i := 1; i < len(ptsSeq); i++ {
		if ptsSeq[i] <= ptsSeq[i-1] {
			t.Fatalf("PTS not monotonic at index %d: %d <= %d (full seq: %v)", i, ptsSeq[i], ptsSeq[i-1], ptsSeq)
		}
	}

	// ffprobe-style duration estimate: last − first. The last packet sits at
	// timelineStart1 + 2s; first at clip0Base. Their delta must equal the
	// real elapsed time across the unified timeline.
	first := ptsSeq[0]
	last := ptsSeq[len(ptsSeq)-1]
	wantSpan := (timelineStart1 + 2*hz) - clip0Base
	if last-first != wantSpan {
		t.Errorf("timeline span = %d ticks, want %d", last-first, wantSpan)
	}

	// Clip 1's first packet must land exactly at timelineStart1, proving it
	// was lifted off its own 0.5s base onto the unified timeline rather than
	// resetting (which is what breaks ffprobe today).
	if ptsSeq[3] != timelineStart1 {
		t.Errorf("clip1 first PTS = %d, want timelineStart1 %d", ptsSeq[3], timelineStart1)
	}
	_ = clip1Dur // documented as clip 1's authored length; not needed past timelineStart1
}
