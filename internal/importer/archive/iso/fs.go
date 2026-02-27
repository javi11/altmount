package iso

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

const iso9660SectorSize = 2048

// isoFileEntry is one non-directory file returned by ListISOFiles.
type isoFileEntry struct {
	path string // full path within ISO (e.g. "BDMV/STREAM/00001.M2TS")
	lba  uint32
	size uint32
}

// ─────────────────────────────────────────────────────────────────────────────
// ISO 9660
// ─────────────────────────────────────────────────────────────────────────────

// iso9660DirEntry is one raw directory record from an ISO 9660 directory.
type iso9660DirEntry struct {
	name  string
	isDir bool
	lba   uint32
	size  uint32
}

// iso9660ListDir returns all non-dot entries in an ISO 9660 directory sector range.
func iso9660ListDir(rs io.ReadSeeker, dirLBA, dirSize uint32) ([]iso9660DirEntry, error) {
	data := make([]byte, dirSize)
	if _, err := rs.Seek(int64(dirLBA)*iso9660SectorSize, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rs, data); err != nil {
		return nil, err
	}
	var entries []iso9660DirEntry
	offset := 0
	for offset < int(dirSize) {
		recLen := int(data[offset])
		if recLen == 0 {
			next := ((offset / iso9660SectorSize) + 1) * iso9660SectorSize
			if next >= int(dirSize) {
				break
			}
			offset = next
			continue
		}
		if offset+recLen > int(dirSize) {
			break
		}
		nameLen := int(data[offset+32])
		if nameLen == 0 || offset+33+nameLen > int(dirSize) {
			offset += recLen
			continue
		}
		identifier := string(data[offset+33 : offset+33+nameLen])
		if identifier == "\x00" || identifier == "\x01" {
			offset += recLen
			continue
		}
		if idx := strings.Index(identifier, ";"); idx >= 0 {
			identifier = identifier[:idx]
		}
		fileFlags := data[offset+25]
		entryLBA := binary.LittleEndian.Uint32(data[offset+2 : offset+6])
		entrySize := binary.LittleEndian.Uint32(data[offset+10 : offset+14])
		entries = append(entries, iso9660DirEntry{
			name:  identifier,
			isDir: fileFlags&0x02 != 0,
			lba:   entryLBA,
			size:  entrySize,
		})
		offset += recLen
	}
	return entries, nil
}

// iso9660WalkAll recursively lists all non-directory files starting at dirLBA/dirSize.
// prefix is prepended to each returned path (empty string for the root call).
func iso9660WalkAll(rs io.ReadSeeker, dirLBA, dirSize uint32, prefix string) ([]isoFileEntry, error) {
	entries, err := iso9660ListDir(rs, dirLBA, dirSize)
	if err != nil {
		return nil, err
	}
	var result []isoFileEntry
	for _, e := range entries {
		entryPath := e.name
		if prefix != "" {
			entryPath = prefix + "/" + e.name
		}
		if e.isDir {
			sub, subErr := iso9660WalkAll(rs, e.lba, e.size, entryPath)
			if subErr != nil {
				continue // skip unreadable sub-directories
			}
			result = append(result, sub...)
		} else {
			result = append(result, isoFileEntry{path: entryPath, lba: e.lba, size: e.size})
		}
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// UDF 2.50
// ─────────────────────────────────────────────────────────────────────────────

// udfTag is the 16-byte ECMA-167 descriptor tag.
type udfTag struct {
	id       uint16
	version  uint16
	checksum uint8
	serial   uint8
	crc      uint16
	crcLen   uint16
	location uint32
}

// udfExtent is an extent_ad (8 bytes): length + absolute sector.
type udfExtent struct{ length, sector uint32 }

// udfLBA is lb_addr (6 bytes): logical block + partition ref.
type udfLBA struct {
	block uint32
	part  uint16
}

// udfLongAD is long_ad: length(4) + lb_addr(6) + implUse(2).
type udfLongAD struct {
	length uint32
	loc    udfLBA
}

// udfShortAD is short_ad (8 bytes): length + logical block.
type udfShortAD struct {
	length uint32
	block  uint32
}

// udfMetaSpan maps a range of metadata logical blocks to physical sectors.
type udfMetaSpan struct {
	metaBlock uint32 // first metadata logical block of this span
	physSect  uint32 // corresponding physical sector
	count     uint32 // number of blocks in this span
}

// udfDirEntry holds one parsed File Identifier Descriptor.
type udfDirEntry struct {
	name  string
	isDir bool
	icb   udfLongAD
}

// udfReadTag reads one 2048-byte sector at sectorNum and parses the 16-byte
// ECMA-167 descriptor tag from its start.
func udfReadTag(rs io.ReadSeeker, sectorNum uint32) (udfTag, []byte, error) {
	buf := make([]byte, iso9660SectorSize)
	if _, err := rs.Seek(int64(sectorNum)*iso9660SectorSize, io.SeekStart); err != nil {
		return udfTag{}, nil, fmt.Errorf("udf seek sector %d: %w", sectorNum, err)
	}
	if _, err := io.ReadFull(rs, buf); err != nil {
		return udfTag{}, nil, fmt.Errorf("udf read sector %d: %w", sectorNum, err)
	}
	t := udfTag{
		id:       binary.LittleEndian.Uint16(buf[0:2]),
		version:  binary.LittleEndian.Uint16(buf[2:4]),
		checksum: buf[4],
		serial:   buf[5],
		crc:      binary.LittleEndian.Uint16(buf[6:8]),
		crcLen:   binary.LittleEndian.Uint16(buf[8:10]),
		location: binary.LittleEndian.Uint32(buf[12:16]),
	}
	return t, buf, nil
}

// udfParseLongAD parses a long_ad from buf[off:].
func udfParseLongAD(buf []byte, off int) udfLongAD {
	length := binary.LittleEndian.Uint32(buf[off:])
	block := binary.LittleEndian.Uint32(buf[off+4:])
	part := binary.LittleEndian.Uint16(buf[off+8:])
	return udfLongAD{length: length & 0x3FFFFFFF, loc: udfLBA{block: block, part: part}}
}

// udfParseShortAD parses a short_ad from buf[off:].
func udfParseShortAD(buf []byte, off int) udfShortAD {
	return udfShortAD{
		length: binary.LittleEndian.Uint32(buf[off:]) & 0x3FFFFFFF,
		block:  binary.LittleEndian.Uint32(buf[off+4:]),
	}
}

// udfCS0ToString converts a CS0 File Identifier to a Go string.
func udfCS0ToString(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	comp := b[0]
	data := b[1:]
	if comp == 8 {
		return string(data)
	}
	if comp == 16 {
		u := make([]uint16, len(data)/2)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(data[i*2:])
		}
		return string(utf16.Decode(u))
	}
	return string(data)
}

// udfParseVDS parses the Volume Descriptor Sequence and returns
// (partitionStart, fsdLongAD, metadataFileLoc).
func udfParseVDS(rs io.ReadSeeker, vdsExtent udfExtent) (partStart uint32, fsdAD udfLongAD, metaFileLoc uint32, err error) {
	sectors := vdsExtent.length / iso9660SectorSize
	if sectors == 0 {
		sectors = 16
	}
	for i := uint32(0); i < sectors; i++ {
		tag, buf, rerr := udfReadTag(rs, vdsExtent.sector+i)
		if rerr != nil {
			return 0, udfLongAD{}, 0, rerr
		}
		switch tag.id {
		case 8: // Terminating Descriptor
			return partStart, fsdAD, metaFileLoc, nil
		case 5: // Partition Descriptor
			partStart = binary.LittleEndian.Uint32(buf[188:192])
		case 6: // Logical Volume Descriptor
			fsdAD = udfParseLongAD(buf, 248)
			mapTableLen := binary.LittleEndian.Uint32(buf[264:268])
			mapOff := 440
			end := mapOff + int(mapTableLen)
			if end > len(buf) {
				end = len(buf)
			}
			for mapOff < end {
				pmType := buf[mapOff]
				pmLen := int(buf[mapOff+1])
				if pmLen == 0 || mapOff+pmLen > end {
					break
				}
				if pmType == 2 && pmLen >= 64 {
					rawIdent := strings.TrimRight(string(buf[mapOff+5:mapOff+28]), "\x00")
					if strings.Contains(rawIdent, "UDF Metadata Partition") {
						metaFileLoc = binary.LittleEndian.Uint32(buf[mapOff+40 : mapOff+44])
					}
				}
				mapOff += pmLen
			}
		}
	}
	return partStart, fsdAD, metaFileLoc, nil
}

// udfBuildMetaMap reads the Metadata File's Extended File Entry and builds
// a list of (metaBlock, physSect, count) spans.
func udfBuildMetaMap(rs io.ReadSeeker, partStart, metaFileLoc uint32) ([]udfMetaSpan, error) {
	physSect := partStart + metaFileLoc
	tag, buf, err := udfReadTag(rs, physSect)
	if err != nil {
		return nil, fmt.Errorf("reading metadata file ICB at %d: %w", physSect, err)
	}
	if tag.id != 261 && tag.id != 266 {
		return nil, fmt.Errorf("expected File Entry (261/266) at sector %d, got tag %d", physSect, tag.id)
	}

	allocType := buf[34] & 0x07

	var allocDescOff, allocDescLen int
	if tag.id == 266 { // Extended File Entry
		eaLen := int(binary.LittleEndian.Uint32(buf[208:212]))
		allocDescLen = int(binary.LittleEndian.Uint32(buf[212:216]))
		allocDescOff = 216 + eaLen
	} else { // Plain File Entry (261)
		eaLen := int(binary.LittleEndian.Uint32(buf[168:172]))
		allocDescLen = int(binary.LittleEndian.Uint32(buf[172:176]))
		allocDescOff = 176 + eaLen
	}

	if allocDescOff+allocDescLen > len(buf) {
		allocDescLen = len(buf) - allocDescOff
	}
	var spans []udfMetaSpan
	var metaBlock uint32
	switch allocType {
	case 0: // short_ad
		for off := 0; off+8 <= allocDescLen; off += 8 {
			ad := udfParseShortAD(buf[allocDescOff:], off)
			if ad.length == 0 {
				break
			}
			nBlocks := (ad.length + iso9660SectorSize - 1) / iso9660SectorSize
			spans = append(spans, udfMetaSpan{metaBlock: metaBlock, physSect: partStart + ad.block, count: nBlocks})
			metaBlock += nBlocks
		}
	case 1: // long_ad
		for off := 0; off+16 <= allocDescLen; off += 16 {
			ad := udfParseLongAD(buf[allocDescOff:], off)
			if ad.length == 0 {
				break
			}
			nBlocks := (ad.length + iso9660SectorSize - 1) / iso9660SectorSize
			spans = append(spans, udfMetaSpan{metaBlock: metaBlock, physSect: partStart + ad.loc.block, count: nBlocks})
			metaBlock += nBlocks
		}
	}
	return spans, nil
}

// udfResolveMetaBlock translates a metadata logical block number to a physical sector.
func udfResolveMetaBlock(block uint32, metaMap []udfMetaSpan, partStart uint32) (uint32, error) {
	for _, span := range metaMap {
		if block >= span.metaBlock && block < span.metaBlock+span.count {
			return span.physSect + (block - span.metaBlock), nil
		}
	}
	return partStart + block, nil
}

// udfResolveICB converts a long_ad ICB location to a physical sector number.
func udfResolveICB(loc udfLBA, metaMap []udfMetaSpan, partStart uint32) (uint32, error) {
	if loc.part == 0 {
		return partStart + loc.block, nil
	}
	return udfResolveMetaBlock(loc.block, metaMap, partStart)
}

// udfReadDirEntries reads all File Identifier Descriptor records from a
// File Entry at physSect.
func udfReadDirEntries(rs io.ReadSeeker, physSect uint32, metaMap []udfMetaSpan, partStart uint32) ([]udfDirEntry, error) {
	tag, buf, err := udfReadTag(rs, physSect)
	if err != nil {
		return nil, fmt.Errorf("reading dir ICB at %d: %w", physSect, err)
	}
	if tag.id != 261 && tag.id != 266 {
		return nil, fmt.Errorf("expected File Entry at sector %d, got tag %d", physSect, tag.id)
	}

	allocType := buf[34] & 0x07

	var allocDescOff, allocDescLen int
	if tag.id == 266 {
		eaLen := int(binary.LittleEndian.Uint32(buf[208:212]))
		allocDescLen = int(binary.LittleEndian.Uint32(buf[212:216]))
		allocDescOff = 216 + eaLen
	} else {
		eaLen := int(binary.LittleEndian.Uint32(buf[168:172]))
		allocDescLen = int(binary.LittleEndian.Uint32(buf[172:176]))
		allocDescOff = 176 + eaLen
	}
	if allocDescOff+allocDescLen > len(buf) {
		allocDescLen = len(buf) - allocDescOff
	}

	var dirData []byte
	switch allocType {
	case 3: // inline
		dirData = buf[allocDescOff : allocDescOff+allocDescLen]
	case 0: // short_ad
		for off := 0; off+8 <= allocDescLen; off += 8 {
			ad := udfParseShortAD(buf[allocDescOff:], off)
			if ad.length == 0 {
				break
			}
			ps, rerr := udfResolveMetaBlock(ad.block, metaMap, partStart)
			if rerr != nil {
				return nil, rerr
			}
			_, sector, rerr := udfReadTag(rs, ps)
			if rerr != nil {
				return nil, rerr
			}
			dirData = append(dirData, sector[:ad.length]...)
		}
	case 1: // long_ad
		for off := 0; off+16 <= allocDescLen; off += 16 {
			ad := udfParseLongAD(buf[allocDescOff:], off)
			if ad.length == 0 {
				break
			}
			ps, rerr := udfResolveICB(ad.loc, metaMap, partStart)
			if rerr != nil {
				return nil, rerr
			}
			_, sector, rerr := udfReadTag(rs, ps)
			if rerr != nil {
				return nil, rerr
			}
			take := int(ad.length)
			if take > len(sector) {
				take = len(sector)
			}
			dirData = append(dirData, sector[:take]...)
		}
	}

	var entries []udfDirEntry
	off := 0
	for off < len(dirData) {
		if off+2 > len(dirData) {
			break
		}
		fidTagID := binary.LittleEndian.Uint16(dirData[off:])
		if fidTagID != 257 { // File Identifier Descriptor
			break
		}
		if off+38 > len(dirData) {
			break
		}
		fileChar := dirData[off+18]
		fileNameLen := int(dirData[off+19])
		icb := udfParseLongAD(dirData, off+20)
		implUseLen := int(binary.LittleEndian.Uint16(dirData[off+36:]))
		headerLen := 38 + implUseLen
		nameStart := off + headerLen
		if nameStart+fileNameLen > len(dirData) {
			break
		}

		recLen := headerLen + fileNameLen
		if recLen%4 != 0 {
			recLen += 4 - (recLen % 4)
		}

		// Skip parent (0x08) or deleted (0x04) entries
		if fileChar&0x0C == 0 {
			name := udfCS0ToString(dirData[nameStart : nameStart+fileNameLen])
			entries = append(entries, udfDirEntry{name: name, isDir: fileChar&0x02 != 0, icb: icb})
		}

		off += recLen
		if recLen == 0 {
			break
		}
	}
	return entries, nil
}

// udfScanForFSD scans sectors from partStart looking for the first File Set
// Descriptor (tag 256).
func udfScanForFSD(rs io.ReadSeeker, partStart uint32) uint32 {
	const scanLimit = 1024
	buf := make([]byte, 16)
	for i := uint32(0); i < scanLimit; i++ {
		sect := partStart + i
		if _, err := rs.Seek(int64(sect)*iso9660SectorSize, io.SeekStart); err != nil {
			return 0
		}
		if _, err := io.ReadFull(rs, buf); err != nil {
			return 0
		}
		if binary.LittleEndian.Uint16(buf[0:2]) == 256 {
			return sect
		}
	}
	return 0
}

// udfSetup reads the AVDP→VDS→FSD chain and returns the partition start,
// metadata map, and root directory ICB.
func udfSetup(rs io.ReadSeeker) (partStart uint32, metaMap []udfMetaSpan, rootICB udfLongAD, err error) {
	_, avdpBuf, err := udfReadTag(rs, 256)
	if err != nil {
		return 0, nil, udfLongAD{}, fmt.Errorf("udf: reading AVDP: %w", err)
	}
	vdsExtent := udfExtent{
		length: binary.LittleEndian.Uint32(avdpBuf[16:20]),
		sector: binary.LittleEndian.Uint32(avdpBuf[20:24]),
	}
	var fsdAD udfLongAD
	var metaFileLoc uint32
	partStart, fsdAD, metaFileLoc, err = udfParseVDS(rs, vdsExtent)
	if err != nil {
		return 0, nil, udfLongAD{}, fmt.Errorf("udf: parsing VDS: %w", err)
	}
	metaMap, err = udfBuildMetaMap(rs, partStart, metaFileLoc)
	if err != nil {
		return 0, nil, udfLongAD{}, fmt.Errorf("udf: building meta map: %w", err)
	}
	fsdPhys, err := udfResolveICB(fsdAD.loc, metaMap, partStart)
	if err != nil {
		return 0, nil, udfLongAD{}, fmt.Errorf("udf: resolving FSD ICB: %w", err)
	}
	fsdTag, fsdBuf, err := udfReadTag(rs, fsdPhys)
	if err != nil {
		return 0, nil, udfLongAD{}, fmt.Errorf("udf: reading FSD at %d: %w", fsdPhys, err)
	}
	if fsdTag.id != 256 {
		if found := udfScanForFSD(rs, partStart); found != 0 {
			_, fsdBuf, err = udfReadTag(rs, found)
			if err != nil {
				return 0, nil, udfLongAD{}, fmt.Errorf("udf: reading scanned FSD at %d: %w", found, err)
			}
			if len(metaMap) == 0 {
				metaMap = []udfMetaSpan{{metaBlock: 0, physSect: found, count: 65536}}
			}
		} else {
			return 0, nil, udfLongAD{}, fmt.Errorf("udf: FSD (tag 256) not found in first 1024 sectors of partition")
		}
	}
	rootICB = udfParseLongAD(fsdBuf, 400)
	return partStart, metaMap, rootICB, nil
}

// udfWalkAll recursively lists all non-directory files in a UDF filesystem.
func udfWalkAll(rs io.ReadSeeker, dirICB udfLongAD, metaMap []udfMetaSpan, partStart uint32, prefix string) ([]isoFileEntry, error) {
	physSect, err := udfResolveICB(dirICB.loc, metaMap, partStart)
	if err != nil {
		return nil, err
	}
	entries, err := udfReadDirEntries(rs, physSect, metaMap, partStart)
	if err != nil {
		return nil, err
	}
	var result []isoFileEntry
	for _, e := range entries {
		entryPath := e.name
		if prefix != "" {
			entryPath = prefix + "/" + e.name
		}
		if e.isDir {
			sub, _ := udfWalkAll(rs, e.icb, metaMap, partStart, entryPath)
			result = append(result, sub...)
			continue
		}
		fePhys, rerr := udfResolveICB(e.icb.loc, metaMap, partStart)
		if rerr != nil {
			continue
		}
		feTag, feBuf, rerr := udfReadTag(rs, fePhys)
		if rerr != nil || (feTag.id != 261 && feTag.id != 266) {
			continue
		}
		infoLen := binary.LittleEndian.Uint64(feBuf[56:64])
		allocType := feBuf[34] & 0x07

		var allocDescOff, allocDescLen int
		if feTag.id == 266 {
			eaLen := int(binary.LittleEndian.Uint32(feBuf[208:212]))
			allocDescLen = int(binary.LittleEndian.Uint32(feBuf[212:216]))
			allocDescOff = 216 + eaLen
		} else {
			eaLen := int(binary.LittleEndian.Uint32(feBuf[168:172]))
			allocDescLen = int(binary.LittleEndian.Uint32(feBuf[172:176]))
			allocDescOff = 176 + eaLen
		}
		if allocDescOff+allocDescLen > len(feBuf) {
			allocDescLen = len(feBuf) - allocDescOff
		}

		var fileLBA uint32
		switch allocType {
		case 0:
			if allocDescLen >= 8 {
				ad := udfParseShortAD(feBuf[allocDescOff:], 0)
				fileLBA = partStart + ad.block
			}
		case 1:
			if allocDescLen >= 16 {
				ad := udfParseLongAD(feBuf[allocDescOff:], 0)
				fileLBA, _ = udfResolveICB(ad.loc, metaMap, partStart)
			}
		}
		if fileLBA > 0 {
			result = append(result, isoFileEntry{path: entryPath, lba: fileLBA, size: uint32(infoLen)})
		}
	}
	return result, nil
}

// ListISOFiles walks the ISO 9660/UDF filesystem and returns all non-directory
// entries. It tries ISO 9660 first and falls back to UDF.
func ListISOFiles(rs io.ReadSeeker) ([]isoFileEntry, error) {
	// Try ISO 9660
	pvd := make([]byte, iso9660SectorSize)
	if _, err := rs.Seek(16*iso9660SectorSize, io.SeekStart); err == nil {
		if _, err := io.ReadFull(rs, pvd); err == nil {
			if pvd[0] == 1 && string(pvd[1:6]) == "CD001" {
				rootRec := pvd[156:]
				dirLBA := binary.LittleEndian.Uint32(rootRec[2:6])
				dirSize := binary.LittleEndian.Uint32(rootRec[10:14])
				return iso9660WalkAll(rs, dirLBA, dirSize, "")
			}
		}
	}
	// Fall back to UDF
	partStart, metaMap, rootICB, err := udfSetup(rs)
	if err != nil {
		return nil, fmt.Errorf("iso: not a valid ISO 9660 or UDF image: %w", err)
	}
	return udfWalkAll(rs, rootICB, metaMap, partStart, "")
}
