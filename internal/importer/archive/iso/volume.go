package iso

import (
	"io"
	"strings"
)

// ReadVolumeLabel returns the ISO 9660 Volume Identifier from the Primary
// Volume Descriptor at sector 16. Hybrid Blu-ray discs always carry a
// 9660 PVD even when the active filesystem is UDF, so this works for both
// plain ISOs and BD images.
//
// Returns an empty string if the descriptor is missing or invalid — callers
// fall back to the ISO filename for disc-group keying.
func ReadVolumeLabel(rs io.ReadSeeker) string {
	pvd := make([]byte, iso9660SectorSize)
	if _, err := rs.Seek(16*iso9660SectorSize, io.SeekStart); err != nil {
		return ""
	}
	if _, err := io.ReadFull(rs, pvd); err != nil {
		return ""
	}
	// Type 1 = Primary Volume Descriptor; identifier "CD001" at +1.
	if pvd[0] != 1 || string(pvd[1:6]) != "CD001" {
		return ""
	}
	// Volume identifier: 32 bytes of a-characters at offset 40, space-padded.
	label := strings.TrimRight(string(pvd[40:72]), " \x00")
	return label
}
