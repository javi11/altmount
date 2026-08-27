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

// mpegTSSyncByte is the MPEG-TS packet sync byte, recurring every 188 bytes.
const mpegTSSyncByte = 0x47

// mpegTSPacketSize is the fixed MPEG-TS packet length in bytes.
const mpegTSPacketSize = 188

// HasMpegTSMagic checks for the sync byte 0x47 recurring at the 188-byte
// MPEG-TS packet stride. A single 0x47 byte is common in non-TS data, so at
// least two packets (376 bytes) must be available and agree before this
// returns true.
func HasMpegTSMagic(data []byte) bool {
	if len(data) < 2*mpegTSPacketSize {
		return false
	}
	if data[0] != mpegTSSyncByte {
		return false
	}
	for offset := mpegTSPacketSize; offset < len(data); offset += mpegTSPacketSize {
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
