package iso

// Continuous-timeline remux core for Blu-ray main-feature virtual files.
//
// A merged BD main feature byte-concatenates N M2TS clips, each carrying its
// OWN independent PTS/DTS/PCR timeline (each starts near its own base). A
// player resyncs on the discontinuities so playback works, but ffprobe and
// seeking compute time from PTS deltas, which are meaningless across clip
// boundaries — hence "Duration: 00:26:21" for a 3h17m movie.
//
// This file holds the pure, stateless byte transform: given a TS packet and a
// 90 kHz delta, add the delta to every timestamp (PTS, DTS, PCR) found in the
// packet, in place. All timestamp fields are fixed-width, so the rewrite is
// byte-length preserving — the virtual file size and every byte offset are
// unchanged, so VFS byte-mapping and range requests keep working untouched.
//
// Nothing here does I/O or knows about clips; the caller supplies the delta
// per packet based on which clip the packet's byte offset falls in. That keeps
// this layer trivially testable (see tsremux_test.go) and is the feasibility
// gate for the whole continuous-timeline feature.

const (
	tsSync        = 0x47
	tsPacketLen   = 188
	bdavPacketLen = 192 // 4-byte TP_extra_header + 188-byte TS packet
	// ptsModulus is 2^33; PTS/DTS/PCR-base are 33-bit values that wrap here.
	// At 90 kHz that is ~26.5 h, far above any single feature's runtime, but
	// we still wrap defensively so a near-max base plus a delta stays legal.
	ptsModulus = int64(1) << 33
)

// detectTSPacketSize inspects a buffer that begins at a packet boundary and
// returns 192 (BDAV, sync byte at offset 4), 188 (plain TS, sync at offset 0),
// or 0 when neither layout is recognised. Blu-ray .m2ts on disc is BDAV
// (192-byte source packets); plain 188 is handled for completeness/tests.
func detectTSPacketSize(buf []byte) int {
	if len(buf) >= bdavPacketLen && buf[4] == tsSync {
		// Confirm with a second packet when available to avoid a chance 0x47.
		if len(buf) >= 2*bdavPacketLen {
			if buf[4+bdavPacketLen] == tsSync {
				return bdavPacketLen
			}
		} else {
			return bdavPacketLen
		}
	}
	if len(buf) >= tsPacketLen && buf[0] == tsSync {
		if len(buf) >= 2*tsPacketLen {
			if buf[tsPacketLen] == tsSync {
				return tsPacketLen
			}
		} else {
			return tsPacketLen
		}
	}
	// Fall back to BDAV if only its sync matched on a short buffer.
	if len(buf) >= bdavPacketLen && buf[4] == tsSync {
		return bdavPacketLen
	}
	if len(buf) >= tsPacketLen && buf[0] == tsSync {
		return tsPacketLen
	}
	return 0
}

// addMod33 returns (v + delta) wrapped into the 33-bit timestamp space.
// delta may be negative (when a clip's pts_base exceeds its timeline_start).
func addMod33(v, delta int64) int64 {
	r := (v + delta) % ptsModulus
	if r < 0 {
		r += ptsModulus
	}
	return r
}

// rewritePacket adds delta90k (a 90 kHz signed offset) to the PTS, DTS, and
// PCR timestamps inside one source packet. packetSize is 192 (BDAV) or 188.
// The packet slice must be exactly packetSize bytes. Returns true if any
// timestamp was rewritten. Packets without timestamps (continuation packets,
// PSI, null) are left untouched.
//
// BDAV's 4-byte TP_extra_header (which carries a 27 MHz arrival timestamp) is
// intentionally NOT rewritten: ATS feeds the player's input-buffer model, not
// presentation timing or ffprobe's duration estimate. Leaving it avoids a
// whole extra class of bugs; revisit only if a hardware player needs it.
func rewritePacket(pkt []byte, packetSize int, delta90k int64) bool {
	if delta90k == 0 || len(pkt) != packetSize {
		return false
	}
	// Locate the 188-byte TS packet within the source packet.
	off := 0
	if packetSize == bdavPacketLen {
		off = 4
	}
	ts := pkt[off : off+tsPacketLen]
	if ts[0] != tsSync {
		return false
	}

	pusi := ts[1]&0x40 != 0
	afc := (ts[3] >> 4) & 0x03 // adaptation_field_control

	changed := false

	// --- PCR (adaptation field) ---
	// AFC 0b10 = adaptation only, 0b11 = adaptation + payload.
	payloadStart := 4
	if afc == 0x02 || afc == 0x03 {
		afLen := int(ts[4])
		// adaptation_field_length counts bytes after itself.
		payloadStart = 5 + afLen
		if afLen >= 1 && 5+afLen <= tsPacketLen {
			afFlags := ts[5]
			if afFlags&0x10 != 0 { // PCR_flag
				// PCR occupies the 6 bytes at ts[6..12).
				if 6+6 <= tsPacketLen {
					if rewritePCR(ts[6:12], delta90k) {
						changed = true
					}
				}
			}
		}
	}

	// --- PTS / DTS (PES header) ---
	// Only the first TS packet of a PES (PUSI=1) carries the PES header with
	// the timestamps; continuation packets have none.
	if pusi && (afc == 0x01 || afc == 0x03) && payloadStart+9 <= tsPacketLen {
		p := ts[payloadStart:]
		// PES start code 0x000001.
		if len(p) >= 9 && p[0] == 0x00 && p[1] == 0x00 && p[2] == 0x01 {
			// Optional PES header present only when top 2 bits of p[6] == 10.
			if p[6]&0xC0 == 0x80 {
				ptsDtsFlags := (p[7] & 0xC0) >> 6
				// 0b10 = PTS only; 0b11 = PTS + DTS.
				if ptsDtsFlags == 0x02 || ptsDtsFlags == 0x03 {
					if payloadStart+9+5 <= tsPacketLen {
						if rewriteTS(p[9:14], delta90k) {
							changed = true
						}
					}
				}
				if ptsDtsFlags == 0x03 {
					if payloadStart+14+5 <= tsPacketLen {
						if rewriteTS(p[14:19], delta90k) {
							changed = true
						}
					}
				}
			}
		}
	}

	return changed
}

// readTS decodes a 33-bit PTS/DTS from a 5-byte field.
//
//	b[0]: prefix(7..4) PTS[32..30](3..1) marker(0)
//	b[1]: PTS[29..22]
//	b[2]: PTS[21..15](7..1) marker(0)
//	b[3]: PTS[14..7]
//	b[4]: PTS[6..0](7..1) marker(0)
func readTS(b []byte) int64 {
	return (int64(b[0]&0x0E) << 29) |
		(int64(b[1]) << 22) |
		(int64(b[2]&0xFE) << 14) |
		(int64(b[3]) << 7) |
		(int64(b[4]) >> 1)
}

// writeTS encodes v back into the 5-byte field, preserving the prefix nibble
// (bits 7..4 of b[0]) and all three marker bits (bit 0 of b[0], b[2], b[4]).
func writeTS(b []byte, v int64) {
	b[0] = (b[0] & 0xF1) | byte((v>>29)&0x0E)
	b[1] = byte(v >> 22)
	b[2] = (b[2] & 0x01) | byte((v>>14)&0xFE)
	b[3] = byte(v >> 7)
	b[4] = (b[4] & 0x01) | byte((v<<1)&0xFE)
}

// rewriteTS adds delta to a PTS/DTS field in place.
func rewriteTS(b []byte, delta int64) bool {
	writeTS(b, addMod33(readTS(b), delta))
	return true
}

// rewritePCR adds delta (90 kHz) to a 6-byte PCR field. The 27 MHz PCR value
// is base*300 + ext; adding delta90k*300 is equivalent to adding delta90k to
// base and leaving ext untouched.
//
//	b[0..3] + top bit of b[4] : program_clock_reference_base (33 bits)
//	b[4] bits 6..1           : reserved
//	b[4] bit 0 + b[5]        : program_clock_reference_extension (9 bits)
func rewritePCR(b []byte, delta90k int64) bool {
	base := (int64(b[0]) << 25) |
		(int64(b[1]) << 17) |
		(int64(b[2]) << 9) |
		(int64(b[3]) << 1) |
		(int64(b[4]) >> 7)
	base = addMod33(base, delta90k)
	b[0] = byte(base >> 25)
	b[1] = byte(base >> 17)
	b[2] = byte(base >> 9)
	b[3] = byte(base >> 1)
	// Preserve b[4] low 7 bits (reserved + ext high bit); set bit 7 = base LSB.
	b[4] = byte((base&0x01)<<7) | (b[4] & 0x7F)
	// b[5] (ext low byte) unchanged.
	return true
}
