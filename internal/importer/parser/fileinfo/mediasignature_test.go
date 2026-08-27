package fileinfo

import "testing"

func TestIsRecognizedMediaContainer(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"matroska EBML", mkvFixture(), true},
		{"mp4 ftyp box", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}, make([]byte, 16)...), true},
		{"avi RIFF", append([]byte("RIFF\x00\x00\x00\x00AVI LIST"), make([]byte, 4)...), true},
		{"mp3 frame sync", []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}, true},
		{"flac", []byte("\x66\x4C\x61\x43\x00\x00\x00\x22"), true},
		{"ogg", []byte("OggS\x00\x02\x00\x00"), true},
		{"mpeg-ts sync bytes at 188 stride", mpegTSFixture(), true},
		{"mpeg-ps pack header", []byte{0x00, 0x00, 0x01, 0xBA, 0x44, 0, 0, 0}, true},
		{"plain text, not media", []byte("this is not a media file, just text padding here"), false},
		{"empty", []byte{}, false},
		{"too short for any signature", []byte{0x1A, 0x45}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRecognizedMediaContainer(tt.data); got != tt.want {
				t.Errorf("IsRecognizedMediaContainer(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// mkvFixture builds a minimal Matroska EBML header carrying the DocType
// "matroska" at the offset mimetype's detector expects: the EBML magic,
// then the DocType element ID (0x42 0x82), a 1-byte vint length, then the
// DocType string itself.
func mkvFixture() []byte {
	buf := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x82, 0x88}
	buf = append(buf, []byte("matroska")...)
	return append(buf, make([]byte, 512-len(buf))...)
}

// mpegTSFixture builds a buffer with the 0x47 sync byte recurring at the
// 188-byte MPEG-TS packet stride, padded to look like a real capture.
func mpegTSFixture() []byte {
	buf := make([]byte, 512)
	for i := 0; i < len(buf); i += 188 {
		buf[i] = 0x47
	}
	return buf
}

func TestHasMpegTSMagic(t *testing.T) {
	buf := mpegTSFixture()
	if !HasMpegTSMagic(buf) {
		t.Error("expected MPEG-TS sync-byte stride to be detected")
	}
	if HasMpegTSMagic([]byte{0x47, 0, 0}) {
		t.Error("a single sync byte with no stride must not be treated as MPEG-TS")
	}
}

func TestHasMpegPSMagic(t *testing.T) {
	if !HasMpegPSMagic([]byte{0x00, 0x00, 0x01, 0xBA, 0x44}) {
		t.Error("expected MPEG-PS pack header to be detected")
	}
	if HasMpegPSMagic([]byte{0x00, 0x00, 0x01, 0xB3}) {
		t.Error("MPEG sequence header (0xB3) must not be confused with pack header (0xBA)")
	}
}
