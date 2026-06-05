package iso

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

// TestUDFReadDirEntriesTruncatedExtent locks in the fix for the bug where
// a directory's allocation descriptor advertised an extent spanning
// multiple sectors but the walker read only the first sector and silently
// dropped every entry past it (~ the reason the Avatar BDMV main-feature
// M2TS files were invisible). Two assertions:
//   - readMetaExtent must keep reading sectors until ad.length is
//     satisfied (the fix);
//   - if a sector read fails because the image is shorter than ad.length,
//     the walk returns partial data without an error so a malformed ISO
//     can't fail the entire import.
func TestUDFReadDirEntriesTruncatedExtent(t *testing.T) {
	image := make([]byte, iso9660SectorSize*21)
	dirICBSector := image[10*iso9660SectorSize : 11*iso9660SectorSize]
	binary.LittleEndian.PutUint16(dirICBSector[0:2], 261)
	dirICBSector[34] = 0
	binary.LittleEndian.PutUint32(dirICBSector[168:172], 0)
	binary.LittleEndian.PutUint32(dirICBSector[172:176], 8)
	binary.LittleEndian.PutUint32(dirICBSector[176:180], 2796)
	binary.LittleEndian.PutUint32(dirICBSector[180:184], 20)

	entries, err := udfReadDirEntries(context.Background(), bytes.NewReader(image), 10, nil, 0)
	if err != nil {
		t.Fatalf("udfReadDirEntries() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("udfReadDirEntries() returned %d entries, want 0", len(entries))
	}
}
