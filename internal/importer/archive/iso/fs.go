package iso

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf16"
)

const iso9660SectorSize = 2048

// isoFileEntry is one non-directory file returned by ListISOFiles. The
// file's data on disc may be split across multiple contiguous extents
// — Blu-ray main-feature M2TS files routinely use hundreds of extents
// chained via Allocation Extent Descriptors. extents is in disc order;
// concatenating their bytes yields the complete file.
type isoFileEntry struct {
	path    string
	size    uint64
	extents []isoExtent
}

// firstLBA returns the start LBA of the file's first extent. Callers
// that only need a starting sector (e.g. reading a small MPLS file
// known to be single-extent) can use this.
func (e isoFileEntry) firstLBA() uint32 {
	if len(e.extents) == 0 {
		return 0
	}
	return e.extents[0].lba
}

// isoExtent is one contiguous run of sectors on disc that contributes
// length bytes to the logical file.
type isoExtent struct {
	lba    uint32
	length uint64
}

// ─────────────────────────────────────────────────────────────────────────────
// ISO 9660
// ─────────────────────────────────────────────────────────────────────────────

// iso9660DirEntry is one raw directory record from an ISO 9660 directory.
type iso9660DirEntry struct {
	name  string
	isDir bool
	lba   uint32
	size  uint64
}

// iso9660ListDir returns all non-dot entries in an ISO 9660 directory sector range.
func iso9660ListDir(rs io.ReadSeeker, dirLBA uint32, dirSize uint64) ([]iso9660DirEntry, error) {
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
			size:  uint64(entrySize),
		})
		offset += recLen
	}
	return entries, nil
}

// iso9660WalkAll recursively lists all non-directory files starting at dirLBA/dirSize.
// prefix is prepended to each returned path (empty string for the root call).
func iso9660WalkAll(rs io.ReadSeeker, dirLBA uint32, dirSize uint64, prefix string) ([]isoFileEntry, error) {
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
			// ISO 9660 stores file data in a single contiguous extent.
			// (Interleave mode exists but is essentially never used.)
			result = append(result, isoFileEntry{
				path:    entryPath,
				size:    e.size,
				extents: []isoExtent{{lba: e.lba, length: e.size}},
			})
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

// udfMaxIndirectDepth caps how many Indirect Entry (tag 248) hops
// udfFollowIndirect will traverse before declaring a malformed chain.
// 16 matches Linux kernel UDF (fs/udf/inode.c) and libisofs convention.
const udfMaxIndirectDepth = 16

// udfFollowIndirect resolves a chain of Indirect Entries (tag 248)
// starting at physSect and returns the physical sector of the real
// File Entry plus its tag and raw buffer. Per UDF §14.7 an Indirect
// Entry is a 16-byte descriptor tag + 20-byte ICBTag + 16-byte
// long_ad at offset 36. Depth-capped to bound runaway on a malformed
// disc that points an Indirect Entry chain back at itself.
func udfFollowIndirect(ctx context.Context, rs io.ReadSeeker, physSect uint32, metaMap []udfMetaSpan, partStart uint32) (uint32, udfTag, []byte, error) {
	for depth := range udfMaxIndirectDepth {
		tag, buf, err := udfReadTag(rs, physSect)
		if err != nil {
			return 0, udfTag{}, nil, fmt.Errorf("udf: reading indirect entry at sector %d: %w", physSect, err)
		}
		if tag.id != 248 {
			return physSect, tag, buf, nil
		}
		if len(buf) < 36+16 {
			return 0, udfTag{}, nil, fmt.Errorf("udf: indirect entry at sector %d too short", physSect)
		}
		next := udfParseLongAD(buf, 36)
		resolved, err := udfResolveICB(next.loc, metaMap, partStart)
		if err != nil {
			return 0, udfTag{}, nil, fmt.Errorf("udf: resolving indirect ICB: %w", err)
		}
		slog.DebugContext(ctx, "UDF: followed Indirect Entry", "from", physSect, "to", resolved, "depth", depth)
		physSect = resolved
	}
	return 0, udfTag{}, nil, fmt.Errorf("udf: indirect entry chain exceeds depth cap (%d)", udfMaxIndirectDepth)
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
			end := min(mapOff+int(mapTableLen), len(buf))
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

// readMetaExtent reads a contiguous extent of `length` bytes starting at
// logical metadata block `startBlock`, walking sector by sector through
// the metaMap so multi-sector extents (e.g. a 26 KiB directory) are
// returned in full. Without this, callers that read only the first
// 2048-byte sector silently lose every entry past the first sector — the
// root cause of the "main-feature M2TS files missing from listing" bug.
func readMetaExtent(rs io.ReadSeeker, startBlock uint32, length int, metaMap []udfMetaSpan, partStart uint32) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	out := make([]byte, 0, length)
	remaining := length
	for b := uint32(0); remaining > 0; b++ {
		ps, err := udfResolveMetaBlock(startBlock+b, metaMap, partStart)
		if err != nil {
			return nil, err
		}
		_, sector, err := udfReadTag(rs, ps)
		if err != nil {
			// Malformed image (e.g. extent claims more sectors than exist):
			// return what we successfully read rather than failing the
			// entire walk. Callers parse partial directory data correctly.
			return out, nil
		}
		take := min(remaining, len(sector))
		out = append(out, sector[:take]...)
		remaining -= take
	}
	return out, nil
}

// readICBExtent is the long_ad analogue of readMetaExtent: walks blocks
// by incrementing the logical-block field inside the ICB long_ad.
func readICBExtent(rs io.ReadSeeker, loc udfLBA, length int, metaMap []udfMetaSpan, partStart uint32) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	out := make([]byte, 0, length)
	remaining := length
	cur := loc
	for remaining > 0 {
		ps, err := udfResolveICB(cur, metaMap, partStart)
		if err != nil {
			return nil, err
		}
		_, sector, err := udfReadTag(rs, ps)
		if err != nil {
			// Malformed image (e.g. extent claims more sectors than exist):
			// return what we successfully read rather than failing the
			// entire walk. Callers parse partial directory data correctly.
			return out, nil
		}
		take := min(remaining, len(sector))
		out = append(out, sector[:take]...)
		remaining -= take
		cur.block++
	}
	return out, nil
}

// udfReadDirEntries reads all File Identifier Descriptor records from a
// File Entry at physSect. ctx is threaded for upcoming Indirect Entry
// (tag 248) follow logic that will emit a debug log on each redirect,
// and as a hook for future warn-log additions in this function.
func udfReadDirEntries(ctx context.Context, rs io.ReadSeeker, physSect uint32, metaMap []udfMetaSpan, partStart uint32) ([]udfDirEntry, error) {
	// Transparently traverse any Indirect Entry (tag 248) chain on a
	// directory ICB. udfFollowIndirect emits a Debug log per redirect.
	physSect, tag, buf, err := udfFollowIndirect(ctx, rs, physSect, metaMap, partStart)
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
		// A single allocation descriptor describes an extent that can span
		// many 2048-byte sectors. The previous version of this code read
		// only the first sector and truncated the rest of the extent,
		// silently dropping every directory entry past ~30 FIDs — which is
		// why BDMV/STREAM/ on a real Blu-ray (~300 entries, ~26 KiB) lost
		// every main-feature M2TS clip. We now walk the full extent.
		for off := 0; off+8 <= allocDescLen; off += 8 {
			ad := udfParseShortAD(buf[allocDescOff:], off)
			if ad.length == 0 {
				break
			}
			data, rerr := readMetaExtent(rs, ad.block, int(ad.length), metaMap, partStart)
			if rerr != nil {
				return nil, rerr
			}
			dirData = append(dirData, data...)
		}
	case 1: // long_ad
		for off := 0; off+16 <= allocDescLen; off += 16 {
			ad := udfParseLongAD(buf[allocDescOff:], off)
			if ad.length == 0 {
				break
			}
			data, rerr := readICBExtent(rs, ad.loc, int(ad.length), metaMap, partStart)
			if rerr != nil {
				return nil, rerr
			}
			dirData = append(dirData, data...)
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
	for i := range uint32(scanLimit) {
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
func udfWalkAll(ctx context.Context, rs io.ReadSeeker, dirICB udfLongAD, metaMap []udfMetaSpan, partStart uint32, prefix string) ([]isoFileEntry, error) {
	physSect, err := udfResolveICB(dirICB.loc, metaMap, partStart)
	if err != nil {
		return nil, err
	}
	entries, err := udfReadDirEntries(ctx, rs, physSect, metaMap, partStart)
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
			sub, _ := udfWalkAll(ctx, rs, e.icb, metaMap, partStart, entryPath)
			result = append(result, sub...)
			continue
		}
		fePhys, rerr := udfResolveICB(e.icb.loc, metaMap, partStart)
		if rerr != nil {
			slog.WarnContext(ctx, "UDF: ICB resolve failed, dropping file from listing",
				"path", entryPath, "icb_block", e.icb.loc.block, "error", rerr)
			continue
		}
		// Transparently follow any Indirect Entry (tag 248) chain. fePhys
		// is reassigned to the resolved post-redirect sector so the
		// downstream collectFileExtents call uses the real FE for any
		// embedded-data ("embeddedFEPhys") accounting.
		fePhys, feTag, feBuf, rerr := udfFollowIndirect(ctx, rs, fePhys, metaMap, partStart)
		if rerr != nil {
			slog.WarnContext(ctx, "UDF: file ICB read failed, dropping file from listing",
				"path", entryPath, "phys_sector", fePhys, "error", rerr)
			continue
		}
		if feTag.id != 261 && feTag.id != 266 {
			slog.WarnContext(ctx, "UDF: file ICB has unexpected tag, dropping file from listing",
				"path", entryPath, "tag_id", feTag.id)
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

		extents := collectFileExtents(ctx, rs, feBuf[allocDescOff:allocDescOff+allocDescLen], allocType, metaMap, partStart, infoLen, fePhys)
		if len(extents) == 0 {
			slog.WarnContext(ctx, "UDF: collectFileExtents returned 0 extents, dropping file from listing",
				"path", entryPath, "info_length", infoLen, "alloc_type", allocType)
			continue
		}
		result = append(result, isoFileEntry{
			path:    entryPath,
			size:    infoLen,
			extents: extents,
		})
	}
	return result, nil
}

// collectFileExtents walks the allocation descriptors of a UDF File Entry
// (or Extended File Entry), following Allocation Extent Descriptor chains
// when the inline AD area is exhausted, and returns one isoExtent per
// recorded data extent in disc order.
//
// allocType is the lower 3 bits of the FE's ICBTag flags:
//
//	0 → short_ad (8 bytes each)
//	1 → long_ad  (16 bytes each)
//	2 → extended ad (20 bytes; rare, treated as short_ad-prefix here)
//	3 → file data embedded in the FE itself (small files)
//
// The high 2 bits of each AD's length field encode the AD "type":
//
//	0 → recorded & allocated extent (real data — emit)
//	1 → not recorded, allocated (sparse — skip, file should not see this on BD)
//	2 → not recorded, not allocated (hole — skip)
//	3 → next AD points at a continuation Allocation Extent Descriptor
//	    (tag 258) holding more ADs; chase the chain
//
// embeddedFEPhys is only meaningful for allocType 3 (it's the FE's own
// physical sector — the file data is inline at allocDescOff of that
// sector, so we materialise a single synthetic extent pointing at it).
func collectFileExtents(ctx context.Context, rs io.ReadSeeker, inlineADs []byte, allocType byte, metaMap []udfMetaSpan, partStart uint32, infoLen uint64, embeddedFEPhys uint32) []isoExtent {
	if allocType == 3 {
		// Embedded data — a single "extent" pointing at the FE sector
		// itself with the inline-AD area treated as the file data. We
		// can't emit a usable LBA for slicing because the data isn't
		// sector-aligned. Skip for now; BD streams never use embedded.
		return nil
	}
	var step int
	switch allocType {
	case 0:
		step = 8
	case 1:
		step = 16
	case 2:
		step = 20 // first 16 bytes are a long_ad; trailing 4 bytes are impl-use
	default:
		return nil
	}

	var extents []isoExtent
	chase := inlineADs
	safety := 0
	for {
		safety++
		if safety > 4096 {
			break // pathological — bail to avoid runaway IO
		}
		var chain *udfLongAD
		for off := 0; off+step <= len(chase); off += step {
			lenField := binary.LittleEndian.Uint32(chase[off:])
			adType := lenField >> 30
			adLen := lenField & 0x3FFFFFFF
			if adLen == 0 && adType != 3 {
				break
			}
			if adType == 3 {
				var loc udfLongAD
				switch step {
				case 8:
					// short_ad continuation: the 4 bytes after length
					// are the next AED's logical block; partition is
					// implicit (same as parent).
					loc = udfLongAD{length: adLen, loc: udfLBA{block: binary.LittleEndian.Uint32(chase[off+4:])}}
				default:
					loc = udfParseLongAD(chase, off)
				}
				chain = &loc
				break
			}
			if adType != 0 {
				// Type 1 (allocated but not recorded) and type 2 (hole)
				// don't carry real bytes. Skip — BD streams shouldn't
				// have these in practice.
				continue
			}
			var lba uint32
			switch step {
			case 8:
				ad := udfParseShortAD(chase, off)
				resolved, err := udfResolveMetaBlock(ad.block, metaMap, partStart)
				if err != nil {
					continue
				}
				lba = resolved
			default:
				ad := udfParseLongAD(chase, off)
				resolved, err := udfResolveICB(ad.loc, metaMap, partStart)
				if err != nil {
					continue
				}
				lba = resolved
			}
			extents = append(extents, isoExtent{lba: lba, length: uint64(adLen)})
		}
		if chain == nil {
			break
		}
		ps, err := udfResolveICB(chain.loc, metaMap, partStart)
		if err != nil {
			slog.WarnContext(ctx, "UDF: AED chain truncated",
				"reason", "icb resolve failed",
				"extents_so_far", len(extents),
				"error", err)
			break
		}
		_, aedBuf, err := udfReadTag(rs, ps)
		if err != nil {
			slog.WarnContext(ctx, "UDF: AED chain truncated",
				"reason", "tag read failed",
				"extents_so_far", len(extents),
				"error", err)
			break
		}
		// Allocation Extent Descriptor layout: 16-byte tag + 4-byte
		// previous-AED pointer + 4-byte length-of-allocation-descriptors,
		// then the ADs themselves.
		if len(aedBuf) < 24 {
			slog.WarnContext(ctx, "UDF: AED chain truncated",
				"reason", "aed buffer too short",
				"extents_so_far", len(extents),
				"buf_len", len(aedBuf))
			break
		}
		nextLen := int(binary.LittleEndian.Uint32(aedBuf[20:24]))
		if nextLen <= 0 || 24+nextLen > len(aedBuf) {
			slog.WarnContext(ctx, "UDF: AED chain truncated",
				"reason", "aed length out of range",
				"extents_so_far", len(extents),
				"next_len", nextLen,
				"buf_len", len(aedBuf))
			break
		}
		chase = aedBuf[24 : 24+nextLen]
	}

	// Defensive: cap the total extent bytes at the FE's info_length so a
	// malformed disc with mis-sized ADs can't return more bytes than the
	// file legitimately contains.
	var total uint64
	for i := range extents {
		if total+extents[i].length > infoLen {
			extents[i].length = infoLen - total
			extents = extents[:i+1]
			break
		}
		total += extents[i].length
	}

	// Coalesce physically contiguous extents — many BD3D SSIF files have
	// dozens of small ADs that sit right next to each other on disc. The
	// underlying bytes are one contiguous run; merging the ADs collapses
	// the NestedSources count proportionally (Avatar SSIF: 23 → 2) and
	// shrinks both the metadata proto and the validation surface.
	extents = coalesceExtents(extents)
	_ = embeddedFEPhys
	return extents
}

// coalesceExtents merges adjacent extents whose physical sectors are
// contiguous (next.lba == prev.lba + prev.length/sector). Returns the
// possibly-shorter slice in disc order. A file whose extents are
// physically scattered (typical for BD M2TS clips interleaved with SSIF
// dependent-view data) is returned unchanged.
func coalesceExtents(in []isoExtent) []isoExtent {
	if len(in) <= 1 {
		return in
	}
	out := make([]isoExtent, 0, len(in))
	cur := in[0]
	for i := 1; i < len(in); i++ {
		next := in[i]
		// length must be a whole number of sectors for the contiguity
		// arithmetic to apply; if it isn't (final partial sector of a
		// file), fall through and start a new run after.
		if cur.length%iso9660SectorSize == 0 &&
			next.lba == cur.lba+uint32(cur.length/iso9660SectorSize) {
			cur.length += next.length
			continue
		}
		out = append(out, cur)
		cur = next
	}
	out = append(out, cur)
	return out
}

// ListISOFiles walks the ISO 9660/UDF filesystem and returns all non-directory
// entries. It tries UDF first (correct 64-bit sizes, authoritative for Blu-ray)
// and falls back to ISO 9660 for plain discs without UDF. ctx is threaded
// through the UDF walk so silent-drop sites can emit slog.WarnContext logs
// for diagnosis without polluting the io.ReadSeeker signature.
func ListISOFiles(ctx context.Context, rs io.ReadSeeker) ([]isoFileEntry, error) {
	// Track the underlying reason both layers failed so the combined-failure
	// error message can point an operator at the actual cause (transient
	// network read, malformed structure, unrecognised version, ...).
	var udfErr, isoErr error

	// Try UDF first (handles Blu-ray and modern discs with correct 64-bit sizes).
	if partStart, metaMap, rootICB, err := udfSetup(rs); err != nil {
		udfErr = err
	} else {
		files, err := udfWalkAll(ctx, rs, rootICB, metaMap, partStart, "")
		switch {
		case err != nil:
			udfErr = fmt.Errorf("walk: %w", err)
		case len(files) == 0:
			udfErr = fmt.Errorf("walk returned no files")
		default:
			return files, nil
		}
	}

	// Fall back to ISO 9660.
	pvd := make([]byte, iso9660SectorSize)
	if _, err := rs.Seek(16*iso9660SectorSize, io.SeekStart); err != nil {
		isoErr = fmt.Errorf("seek PVD: %w", err)
	} else if _, err := io.ReadFull(rs, pvd); err != nil {
		isoErr = fmt.Errorf("read PVD: %w", err)
	} else if pvd[0] != 1 || string(pvd[1:6]) != "CD001" {
		isoErr = fmt.Errorf("invalid PVD header (type=%d magic=%q)", pvd[0], pvd[1:6])
	} else {
		rootRec := pvd[156:]
		dirLBA := binary.LittleEndian.Uint32(rootRec[2:6])
		dirSize := uint64(binary.LittleEndian.Uint32(rootRec[10:14]))
		return iso9660WalkAll(rs, dirLBA, dirSize, "")
	}

	return nil, fmt.Errorf("iso: not a valid ISO 9660 or UDF image (udf: %v; iso9660: %v)", udfErr, isoErr)
}
