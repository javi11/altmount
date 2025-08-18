package rarindex

// NOTE: This is an initial skeleton. RAR formats are proprietary; this code focuses on
// identifying header sizes and file data offsets for simple (stored) files, non encrypted.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Version enumerations
const (
	VersionUnknown = "UNKNOWN"
	VersionRar3    = "RAR3"
	VersionRar5    = "RAR5"
)

// Archive signatures
var (
	rarrSigV3 = []byte("Rar!\x1A\x07\x00") // 7 bytes then 0x00 (SFX may prepend data)
	rarrSigV5 = []byte("Rar!\x1A\x07\x01\x00")
)

// VolumeIndex holds header size accounting for a volume file.
type VolumeIndex struct {
	Path             string
	Version          string
	TotalHeaderBytes int64 // bytes from start of file up to first file payload (for a stored file)
	FileBlocks       []FileBlock
}

// FileBlock represents a file header encountered (RAR3 or RAR5 simplified)
type FileBlock struct {
	Name         string
	HeaderPos    int64 // offset where header starts
	HeaderSize   int64 // full header size
	DataPos      int64 // where the file's data would start within this volume
	PackedSize   int64 // size stored (for stored == original)
	Continued    bool  // continues in next volume
	UnpackedSize int64 // original size (if available)
	Stored       bool  // true if file data is stored (no compression)
}

func (v *VolumeIndex) DataOffset() int64 { return v.TotalHeaderBytes }

// DiscoverVolumes attempts to find all parts given the first volume path.
// Supports patterns like name.part01.rar / .part1.rar / .r00 style.
func DiscoverVolumes(first string) ([]string, error) {
	return DiscoverVolumesFS(defaultFS, first)
}

// FileSystem abstracts minimal operations needed to discover volumes.
type FileSystem interface {
	Stat(path string) (fs.FileInfo, error)
	Open(path string) (fs.File, error)
}

type osFS struct{}

func (osFS) Stat(p string) (fs.FileInfo, error) { return os.Stat(p) }
func (osFS) Open(p string) (fs.File, error)     { return os.Open(p) }

var defaultFS osFS

// DiscoverVolumesFS works like DiscoverVolumes but uses provided FileSystem (useful for virtual / in-memory tests).
func DiscoverVolumesFS(fs FileSystem, first string) ([]string, error) {
	base := filepath.Base(first)
	// Patterns we attempt to generalize: partXX.rar, partX.rar, .r00
	partRe := regexp.MustCompile(`(?i)(?P<prefix>.*?)(?P<sep>[_.-]?)(?:part)(?P<num>\d+)(?P<suffix>\.rar)`)
	if m := partRe.FindStringSubmatch(base); m != nil {
		prefix := m[1]
		sep := m[2]
		num := m[3]
		suffix := m[4]
		width := len(num)
		dir := filepath.Dir(first)
		// collect sequential numbers until gap
		var vols []string
		for i := 1; i < 10000; i++ { // arbitrary upper bound
			name := fmt.Sprintf("%s%spart%0*d%s", prefix, sep, width, i, suffix)

			p := filepath.Join(dir, name)
			if _, err := fs.Stat(p); err != nil {
				if i == 1 {
					return nil, fmt.Errorf("first volume not found: %s", p)
				}
				break
			}
			vols = append(vols, p)
		}
		return vols, nil
	}
	// .r00 style starting from .rar
	if strings.HasSuffix(strings.ToLower(base), ".rar") {
		prefix := strings.TrimSuffix(first, filepath.Ext(first))
		dir := filepath.Dir(first)
		var vols []string
		// first main .rar
		if _, err := fs.Stat(first); err == nil {
			vols = append(vols, first)
		} else {
			return nil, err
		}
		for i := 0; i < 1000; i++ {
			name := fmt.Sprintf("%s.r%02d", prefix, i)
			p := filepath.Join(dir, filepath.Base(name))
			if _, err := fs.Stat(p); err != nil {
				break
			}
			vols = append(vols, p)
		}
		return vols, nil
	}
	return []string{first}, nil
}

// IndexVolumes parses each volume to compute header sizes. Stops at first error.
func IndexVolumes(fs FileSystem, volPaths []string) ([]*VolumeIndex, error) {
	var res []*VolumeIndex
	for _, p := range volPaths {
		v, err := indexSingle(fs, p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		res = append(res, v)
	}
	return res, nil
}

func indexSingle(fs FileSystem, path string) (*VolumeIndex, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, _ := f.Stat()
	fileSize := st.Size()
	br := bufio.NewReader(f)
	// detect signature (scan first 1KB to allow SFX stubs)
	version, sigOffset, err := detectSignature(br)
	if err != nil {
		return nil, err
	}
	if s, ok := f.(io.Seeker); ok {
		if _, err := s.Seek(sigOffset, io.SeekStart); err != nil {
			return nil, err
		}
	} else {
		// If we cannot seek, we need to reset the reader to the start
		if _, err := br.Discard(int(sigOffset)); err != nil {
			return nil, fmt.Errorf("failed to seek to signature offset %d in %s: %w", sigOffset, path, err)
		}
	}

	br.Reset(f)
	vi := &VolumeIndex{Path: path, Version: version}
	switch version {
	case VersionRar3:
		if err := parseRar3(br, vi, sigOffset); err != nil {
			return nil, err
		}
	case VersionRar5:
		// Attempt to provide seeker for optimized skipping
		var seeker io.ReadSeeker
		if rs, ok := f.(io.ReadSeeker); ok {
			seeker = rs
		}
		if err := parseRar5(br, seeker, vi, sigOffset, fileSize); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported/unknown version")
	}
	return vi, nil
}

func detectSignature(br *bufio.Reader) (string, int64, error) {
	buf, _ := br.Peek(1024)
	// search
	for i := 0; i+7 < len(buf); i++ {
		if i+len(rarrSigV5) <= len(buf) && string(buf[i:i+len(rarrSigV5)]) == string(rarrSigV5) {
			return VersionRar5, int64(i), nil
		}
		if i+len(rarrSigV3) <= len(buf) && string(buf[i:i+len(rarrSigV3)]) == string(rarrSigV3) {
			return VersionRar3, int64(i), nil
		}
	}
	return VersionUnknown, 0, errors.New("RAR signature not found in first 1KB")
}

// ---------------- RAR3 parsing (simplified) -----------------
// RAR3 block header layout:
//  2 bytes CRC
//  1 byte  type
//  2 bytes flags
//  2 bytes size (including header)
//  (optional) addsize (4 bytes) if bit 0x8000 in flags
// File header (type=0x74) has additional fixed fields.

const (
	rar3BlockTypeFile = 0x74
)

type rar3BlockHeader struct {
	CRC     uint16
	Type    byte
	Flags   uint16
	Size    uint16
	AddSize uint32 // only if flags & 0x8000
}

func parseRar3(br *bufio.Reader, vi *VolumeIndex, baseOffset int64) error {
	// first 7+1 bytes signature (7 + 1 null) already positioned
	pos := baseOffset
	// skip signature (7 + 1)
	if _, err := br.Discard(8); err != nil {
		return err
	}
	pos += 8
	for {
		hdrStart := pos
		h, err := readRar3BlockHeader(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		totalSize := int64(h.Size)
		if h.Flags&0x8000 != 0 {
			totalSize += int64(h.AddSize)
		}
		if h.Type == rar3BlockTypeFile {
			fb, err := parseRar3FileHeader(br, hdrStart, h, pos)
			if err != nil {
				return err
			}
			vi.FileBlocks = append(vi.FileBlocks, fb)
		} else {
			// skip rest of block body (already consumed header bytes?)
			toSkip := totalSize - 7 // header struct bytes read
			if toSkip > 0 {
				if _, err := br.Discard(int(toSkip)); err != nil {
					return err
				}
			}
		}
		pos += totalSize
		// Stop condition: we only need header region until first file data.
		if len(vi.FileBlocks) > 0 {
			vi.TotalHeaderBytes = vi.FileBlocks[0].DataPos
			break
		}
	}
	return nil
}

func readRar3BlockHeader(br *bufio.Reader) (*rar3BlockHeader, error) {
	var raw [7]byte
	if _, err := io.ReadFull(br, raw[:]); err != nil {
		return nil, err
	}
	h := &rar3BlockHeader{
		CRC:   binary.LittleEndian.Uint16(raw[0:2]),
		Type:  raw[2],
		Flags: binary.LittleEndian.Uint16(raw[3:5]),
		Size:  binary.LittleEndian.Uint16(raw[5:7]),
	}
	if h.Flags&0x8000 != 0 {
		var add [4]byte
		if _, err := io.ReadFull(br, add[:]); err != nil {
			return nil, err
		}
		h.AddSize = binary.LittleEndian.Uint32(add[:])
	}
	return h, nil
}

func parseRar3FileHeader(br *bufio.Reader, hdrStart int64, bh *rar3BlockHeader, pos int64) (FileBlock, error) {
	// We have already read 7 or 11 bytes of header. Need to read rest of file header fixed part.
	// RAR3 file header layout after initial block header fields:
	// PACK_SIZE (4), UNP_SIZE (4), HOST_OS(1), FILE_CRC(4), FTIME(4), UNP_VER(1), METHOD(1), NAME_SIZE(2), ATTR(4)
	var fixed [25]byte
	if _, err := io.ReadFull(br, fixed[:]); err != nil {
		return FileBlock{}, err
	}
	packSize := binary.LittleEndian.Uint32(fixed[0:4])
	unpSize := binary.LittleEndian.Uint32(fixed[4:8])
	method := fixed[18]
	nameSize := binary.LittleEndian.Uint16(fixed[19:21])
	// read filename
	nameBytes := make([]byte, nameSize)
	if _, err := io.ReadFull(br, nameBytes); err != nil {
		return FileBlock{}, err
	}
	name := string(nameBytes)
	headerSize := int64(7) // initial block header
	if bh.Flags&0x8000 != 0 {
		headerSize += 4
	}
	headerSize += 25 + int64(nameSize)
	// RAR3 may have salt if flags & 0x400
	if bh.Flags&0x0400 != 0 { // salt present
		if _, err := br.Discard(8); err != nil {
			return FileBlock{}, err
		}
		headerSize += 8
	}
	dataPos := hdrStart + headerSize
	stored := method == 0x30 // '0' stored
	return FileBlock{
		Name:         name,
		HeaderPos:    hdrStart,
		HeaderSize:   headerSize,
		DataPos:      dataPos,
		PackedSize:   int64(packSize),
		Continued:    unpSize > packSize, // simplistic heuristic
		UnpackedSize: int64(unpSize),
		Stored:       stored,
	}, nil
}

// ---------------- RAR5 parsing (simplified but spec‑aligned for header sizing) -----------------
// RAR5 block layout (simplified):
//  CRC32(4) | HEAD_SIZE(varint) | BLOCK_TYPE(varint) | FLAGS(varint) | [EXTRA_AREA_SIZE(varint)] | [DATA_SIZE(varint)] | EXTRA_AREA... | (file header fields...) | (file data...)
// HEAD_SIZE counts bytes starting from HEAD_SIZE field up to end of header (EXTRA_AREA + file header meta), NOT including the initial CRC.
// If FLAGS bit0 set => EXTRA_AREA_SIZE present and EXTRA_AREA follows inside header.
// If FLAGS bit1 set => DATA_SIZE present (size of following data section). File data begins right after full header (CRC + HEAD_SIZE bytes).
// We only need to locate first FILE (type=2) block and report its data offset.

// FileEntry summarizes a file within a volume.
type FileEntry struct {
	Name       string
	DataOffset int64
	PackedSize int64
}

// VolumeFiles lists all file entries for a volume.
type VolumeFiles struct {
	Path  string
	Files []FileEntry
}

// BuildVolumeFiles constructs per-volume file listings.
func BuildVolumeFiles(vs []*VolumeIndex) []VolumeFiles {
	out := make([]VolumeFiles, 0, len(vs))
	for _, v := range vs {
		vf := VolumeFiles{Path: v.Path}
		for _, fb := range v.FileBlocks {
			vf.Files = append(vf.Files, FileEntry{Name: fb.Name, DataOffset: fb.DataPos, PackedSize: fb.PackedSize})
		}
		out = append(out, vf)
	}
	return out
}

// parseRar5 implements spec-based parsing and collects all file headers.
func parseRar5(br *bufio.Reader, seeker io.ReadSeeker, vi *VolumeIndex, baseOffset int64, fileSize int64) error {
	if _, err := br.Discard(8); err != nil {
		return fmt.Errorf("discard signature: %w", err)
	}
	pos := baseOffset + 8
	debug := os.Getenv("RARINDEX_DEBUG") != ""
	logDebug := func(format string, a ...any) {
		if debug {
			fmt.Fprintf(os.Stderr, "[rar5] "+format+"\n", a...)
		}
	}
	for {
		if fileSize > 0 && pos >= fileSize {
			return nil
		}
		hdrStart := pos
		var crc [4]byte
		if _, err := io.ReadFull(br, crc[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read block crc at %d: %w", pos, err)
		}
		pos += 4
		headSize, headSizeLen, err := readVarint(br)
		if err != nil {
			return fmt.Errorf("read headSize at %d: %w", pos, err)
		}
		pos += headSizeLen
		if headSize == 0 { // tolerant: treat as end marker / padding
			logDebug("zero headSize encountered at %d -> stop", hdrStart)
			return nil
		}
		if headSize > 2*1024*1024 {
			return fmt.Errorf("suspicious headSize %d at %d", headSize, hdrStart)
		}
		if fileSize > 0 && pos+int64(headSize) > fileSize { // truncated / misaligned -> stop gracefully
			logDebug("headSize exceeds remaining file (%d) at %d -> stop", headSize, hdrStart)
			return nil
		}
		headData := make([]byte, headSize)
		if _, err := io.ReadFull(br, headData); err != nil {
			return fmt.Errorf("read headData size=%d at %d: %w", headSize, hdrStart, err)
		}
		pos += int64(headSize)
		cur := 0
		readVar := func() (uint64, int, error) {
			v, n, e := readVarintFromSlice(headData[cur:])
			if e != nil {
				return 0, 0, e
			}
			cur += int(n)
			return v, int(n), nil
		}
		blockType, _, err := readVar()
		if err != nil {
			return fmt.Errorf("blockType: %w", err)
		}
		flags, _, err := readVar()
		if err != nil {
			return fmt.Errorf("flags: %w", err)
		}
		var extraAreaSize, dataSize uint64
		if flags&0x0001 != 0 {
			v, _, e := readVar()
			if e != nil {
				return fmt.Errorf("extraAreaSize: %w", e)
			}
			extraAreaSize = v
		}
		if flags&0x0002 != 0 {
			v, _, e := readVar()
			if e != nil {
				return fmt.Errorf("dataSize: %w", e)
			}
			dataSize = v
		}
		// Extra area is at END of header. So block specific region excludes trailing extra area.
		blockSpecificEnd := int(headSize)
		if extraAreaSize > 0 {
			if extraAreaSize > uint64(blockSpecificEnd-cur) {
				return fmt.Errorf("extraAreaSize overflow %d > %d", extraAreaSize, blockSpecificEnd-cur)
			}
			blockSpecificEnd -= int(extraAreaSize)
		}
		if debug {
			logDebug("hdr @%d type=%d flags=%#x headSize=%d extra=%d data=%d cur=%d blockSpecificEnd=%d", hdrStart, blockType, flags, headSize, extraAreaSize, dataSize, cur, blockSpecificEnd)
		}
		if blockType == 2 { // File header
			if blockSpecificEnd < cur {
				return fmt.Errorf("blockSpecificEnd<cur")
			}
			bs := headData[cur:blockSpecificEnd]
			bcur := 0
			readFileVar := func() (uint64, int, error) {
				v, n, e := readVarintFromSlice(bs[bcur:])
				if e != nil {
					return 0, 0, e
				}
				bcur += int(n)
				return v, int(n), nil
			}
			fileFlags, _, err := readFileVar()
			if err != nil {
				return fmt.Errorf("fileFlags: %w", err)
			}
			unpSizeVal, _, err := readFileVar()
			if err != nil {
				return fmt.Errorf("unpackedSize: %w", err)
			}
			_, _, err = readFileVar()
			if err != nil {
				return fmt.Errorf("fileAttr: %w", err)
			} // Attributes
			if fileFlags&0x0002 != 0 { // mtime
				if len(bs)-bcur < 4 {
					return fmt.Errorf("mtime truncated")
				}
				bcur += 4
			}
			if fileFlags&0x0004 != 0 { // CRC32
				if len(bs)-bcur < 4 {
					return fmt.Errorf("crc32 truncated")
				}
				bcur += 4
			}
			compInfo, _, err := readFileVar()
			if err != nil {
				return fmt.Errorf("compInfo: %w", err)
			}
			_, _, err = readFileVar()
			if err != nil {
				return fmt.Errorf("hostOS: %w", err)
			}
			nameLen, _, err := readFileVar()
			if err != nil {
				return fmt.Errorf("nameLen: %w", err)
			}
			if nameLen == 0 || int(nameLen) > len(bs)-bcur {
				return fmt.Errorf("bad nameLen %d", nameLen)
			}
			nameBytes := bs[bcur : bcur+int(nameLen)]
			bcur += int(nameLen)
			stored := compInfo == 0
			fb := FileBlock{HeaderPos: hdrStart, HeaderSize: 4 + headSizeLen + int64(headSize), DataPos: hdrStart + 4 + headSizeLen + int64(headSize), PackedSize: int64(dataSize), Name: string(nameBytes), UnpackedSize: int64(unpSizeVal), Stored: stored}
			vi.FileBlocks = append(vi.FileBlocks, fb)
			if vi.TotalHeaderBytes == 0 {
				vi.TotalHeaderBytes = fb.DataPos
			}
			if debug {
				logDebug("file name=%s unpacked=%d packed=%d stored=%v", fb.Name, unpSizeVal, dataSize, stored)
			}
		}
		if blockType == 5 { // end of archive
			return nil
		}
		// Skip data
		if dataSize > 0 {
			toSkip := int64(dataSize)
			if seeker != nil {
				// Drain buffered bytes first; they are part of the data section already read ahead.
				if b := br.Buffered(); b > 0 {
					// Never drain more than we intend to skip (safety guard in case of inconsistency).
					if int64(b) > toSkip {
						b = int(toSkip)
					}
					if _, err := br.Discard(b); err != nil {
						return fmt.Errorf("drain buffer before seek: %w", err)
					}
					pos += int64(b)
					toSkip -= int64(b)
				}
				if toSkip > 0 { // only seek remaining
					if _, err := seeker.Seek(toSkip, io.SeekCurrent); err == nil {
						pos += toSkip
						br.Reset(seeker)
						continue
					}
					// If seek fails we fall through to CopyN for remaining bytes.
				}
			}
			if toSkip > 0 { // CopyN remaining (either no seeker or seek failed)
				if _, err := io.CopyN(io.Discard, br, toSkip); err != nil {
					return fmt.Errorf("discard data: %w", err)
				}
				pos += toSkip
			}
		}
	}
}

// readVarintFromSlice reads a RAR5 varint from a byte slice.
func readVarintFromSlice(b []byte) (uint64, int64, error) {
	var val uint64
	var n int64
	for i := 0; i < len(b) && i < 10; i++ {
		c := b[i]
		val |= uint64(c&0x7F) << (7 * i)
		n++
		if c&0x80 == 0 {
			return val, n, nil
		}
	}
	if n == 0 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	return 0, n, errors.New("varint too long or truncated")
}

// readVarint reads RAR5 variable-length integer directly from reader.
func readVarint(br *bufio.Reader) (value uint64, n int64, err error) {
	for i := 0; i < 10; i++ {
		b, e := br.ReadByte()
		if e != nil {
			err = e
			return
		}
		value |= uint64(b&0x7f) << (7 * i)
		n++
		if b&0x80 == 0 {
			return
		}
	}
	err = errors.New("varint too long")
	return
}

// VolumeData summarizes per volume offset and first file name (if parsed).
type VolumeData struct {
	Path       string
	DataOffset int64
	FileName   string
}

// Offsets builds a slice of VolumeData from VolumeIndex slice.
func Offsets(vs []*VolumeIndex) []VolumeData {
	out := make([]VolumeData, 0, len(vs))
	lastName := ""
	for _, v := range vs {
		if len(v.FileBlocks) > 0 && v.FileBlocks[0].Name != "" {
			lastName = v.FileBlocks[0].Name
		}
		out = append(out, VolumeData{Path: v.Path, DataOffset: v.DataOffset(), FileName: lastName})
	}
	return out
}

// AggregatedFilePart represents one part of a (possibly split) file residing in a volume.
type AggregatedFilePart struct {
	Path         string `json:"path"`
	DataOffset   int64  `json:"dataOffset"`
	PackedSize   int64  `json:"packedSize"`
	UnpackedSize int64  `json:"unpackedSize"`
	Stored       bool   `json:"stored"`
}

// AggregatedFile groups all parts (headers) for a given file name across volumes.
type AggregatedFile struct {
	Name              string               `json:"name"`
	TotalPackedSize   int64                `json:"totalPackedSize"`
	TotalUnpackedSize int64                `json:"totalUnpackedSize"`
	Parts             []AggregatedFilePart `json:"parts"`
}

// AggregateFiles builds aggregated file listing from volume indexes.
func AggregateFiles(vs []*VolumeIndex) []AggregatedFile {
	m := make(map[string]*AggregatedFile)
	order := []string{}
	for _, v := range vs {
		for _, fb := range v.FileBlocks {
			if fb.Name == "" {
				continue
			}
			ag, ok := m[fb.Name]
			if !ok {
				ag = &AggregatedFile{Name: fb.Name}
				m[fb.Name] = ag
				order = append(order, fb.Name)
			}
			ag.Parts = append(ag.Parts, AggregatedFilePart{Path: v.Path, DataOffset: fb.DataPos, PackedSize: fb.PackedSize, UnpackedSize: fb.UnpackedSize, Stored: fb.Stored})
			ag.TotalPackedSize += fb.PackedSize
			// Only take first reported unpacked size (do not sum across parts)
			if ag.TotalUnpackedSize == 0 && fb.UnpackedSize > 0 {
				ag.TotalUnpackedSize = fb.UnpackedSize
			}
		}
	}
	out := make([]AggregatedFile, 0, len(order))
	for _, name := range order {
		out = append(out, *m[name])
	}
	return out
}

// aggregateFromFirstFS is an internal helper that discovers volumes via the provided FileSystem
// starting from the given first volume path, indexes them, and returns the aggregated file list.
func AggregateFromFirstFS(fs FileSystem, first string) ([]AggregatedFile, error) {
	vols, err := DiscoverVolumesFS(fs, first)
	if err != nil {
		return nil, err
	}
	idx, err := IndexVolumes(fs, vols)
	if err != nil {
		return nil, err
	}
	return AggregateFiles(idx), nil
}

// AggregateFromFirst is a convenience function using the default filesystem.
// It discovers all volumes starting from the provided first volume path and returns
// the aggregated files (grouped across volumes) ready for streaming logic.
func AggregateFromFirst(first string) ([]AggregatedFile, error) {
	return AggregateFromFirstFS(defaultFS, first)
}
