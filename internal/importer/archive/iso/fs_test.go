package iso

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestUDFReadDirEntriesShortADClampsExtentLength(t *testing.T) {
	image := make([]byte, iso9660SectorSize*21)
	dirICBSector := image[10*iso9660SectorSize : 11*iso9660SectorSize]
	binary.LittleEndian.PutUint16(dirICBSector[0:2], 261)
	dirICBSector[34] = 0
	binary.LittleEndian.PutUint32(dirICBSector[168:172], 0)
	binary.LittleEndian.PutUint32(dirICBSector[172:176], 8)
	binary.LittleEndian.PutUint32(dirICBSector[176:180], 2796)
	binary.LittleEndian.PutUint32(dirICBSector[180:184], 20)

	entries, err := udfReadDirEntries(bytes.NewReader(image), 10, nil, 0)
	if err != nil {
		t.Fatalf("udfReadDirEntries() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("udfReadDirEntries() returned %d entries, want 0", len(entries))
	}
}
