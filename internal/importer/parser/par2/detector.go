package par2

import "bytes"

// HasMagicBytes checks if the provided data contains a valid PAR2 magic signature
// The PAR2 format uses "PAR2\0PKT" as its magic bytes at the start of each packet
func HasMagicBytes(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], MagicBytes[:])
}
