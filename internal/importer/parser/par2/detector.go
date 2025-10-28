package par2

// HasMagicBytes checks if the provided data contains a valid PAR2 magic signature
// The PAR2 format uses "PAR2\0PKT" as its magic bytes at the start of each packet
func HasMagicBytes(data []byte) bool {
	if len(data) < 8 {
		return false
	}

	// Compare the first 8 bytes with the expected PAR2 magic signature
	for i := range 8 {
		if data[i] != MagicBytes[i] {
			return false
		}
	}

	return true
}
