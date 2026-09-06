package fileinfo

import "github.com/gabriel-vasile/mimetype"

// mediaMimeWhitelist lists every video/audio MIME type mimetype.Detect can
// return (per github.com/gabriel-vasile/mimetype's supported_mimes.md).
// Membership, not mere recognition, is what makes a probe ContentValid —
// a recognized-but-non-media type (e.g. a text/HTML error page) must still
// be rejected.
var mediaMimeWhitelist = map[string]bool{
	"application/ogg":                  true,
	"audio/ogg":                        true,
	"audio/flac":                       true,
	"audio/midi":                       true,
	"audio/ape":                        true,
	"audio/musepack":                   true,
	"audio/amr":                        true,
	"audio/wav":                        true,
	"audio/aiff":                       true,
	"audio/basic":                      true,
	"audio/aac":                        true,
	"audio/x-unknown":                  true,
	"application/vnd.apple.mpegurl":    true,
	"application/vnd.rn-realmedia-vbr": true,
	"audio/mpeg":                       true,
	"audio/webm":                       true,
	"audio/qcelp":                      true,
	"video/ogg":                        true,
	"video/mpeg":                       true,
	"video/quicktime":                  true,
	"video/mp4":                        true,
	"video/3gpp":                       true,
	"video/3gpp2":                      true,
	"video/x-m4v":                      true,
	"video/mj2":                        true,
	"video/vnd.dvb.file":               true,
	"video/webm":                       true,
	"video/x-msvideo":                  true,
	"video/x-flv":                      true,
	"video/matroska":                   true,
	"video/x-ms-asf":                   true,
	"video/jpm":                        true,
}

// IsRecognizedMediaContainer reports whether buf starts with a signature for
// a known video/audio container. It checks mimetype.Detect's type chain
// against a whitelist (never accepting a recognized-but-non-media type),
// then falls back to hand-rolled MPEG-TS/PS checks that the mimetype
// library does not cover — both are common outputs of ISO/BDMV remuxes.
func IsRecognizedMediaContainer(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}

	mt := mimetype.Detect(buf)
	for m := mt; m != nil; m = m.Parent() {
		if mediaMimeWhitelist[m.String()] {
			return true
		}
	}

	return HasMpegTSMagic(buf) || HasMpegPSMagic(buf)
}

// mpegTSSyncByte is the MPEG-TS packet sync byte, recurring every
// mpegTSPacketSize (plain TS) or mpegTSBDAVPacketSize (Blu-ray BDAV) bytes.
const mpegTSSyncByte = 0x47

// mpegTSPacketSize is the fixed MPEG-TS packet length in bytes.
const mpegTSPacketSize = 188

// mpegTSBDAVPacketSize is the Blu-ray BDAV (.m2ts) packet length: a 4-byte
// TP_extra_header (arrival timestamp, arbitrary bytes) prepended to a
// standard 188-byte TS packet.
const mpegTSBDAVPacketSize = 192

// HasMpegTSMagic checks for the sync byte 0x47 recurring at the MPEG-TS
// packet stride, trying both the plain 188-byte stride and the Blu-ray BDAV
// 192-byte stride (where the sync byte is offset by a 4-byte timestamp
// header). It scans for the first 0x47 within the first BDAV packet, then
// requires that offset to hold as a consistent stride for at least two
// packets — a single stray 0x47 byte is common in non-TS data and must not
// false-positive.
func HasMpegTSMagic(data []byte) bool {
	if len(data) < 2*mpegTSPacketSize {
		return false
	}
	for start := 0; start < mpegTSBDAVPacketSize && start < len(data); start++ {
		if data[start] != mpegTSSyncByte {
			continue
		}
		if hasConsistentTSStride(data, start, mpegTSPacketSize) ||
			hasConsistentTSStride(data, start, mpegTSBDAVPacketSize) {
			return true
		}
	}
	return false
}

// hasConsistentTSStride reports whether data[start] and every subsequent
// byte at the given stride are the TS sync byte, requiring at least two
// packets' worth of data to be available and agree.
func hasConsistentTSStride(data []byte, start, stride int) bool {
	if start+stride >= len(data) {
		return false
	}
	for offset := start; offset < len(data); offset += stride {
		if data[offset] != mpegTSSyncByte {
			return false
		}
	}
	return true
}

// mpegPSPackHeader is the MPEG-PS/VOB pack_header start code.
var mpegPSPackHeader = []byte{0x00, 0x00, 0x01, 0xBA}

// HasMpegPSMagic checks for the MPEG-PS/VOB pack_header start code.
func HasMpegPSMagic(data []byte) bool {
	if len(data) < len(mpegPSPackHeader) {
		return false
	}
	for i, b := range mpegPSPackHeader {
		if data[i] != b {
			return false
		}
	}
	return true
}
