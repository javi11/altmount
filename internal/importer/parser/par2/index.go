package par2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// SliceCheck is one input slice's checksums from an IFSC packet. Checksums
// are computed over the slice zero-padded to the set's slice size.
type SliceCheck struct {
	MD5   [16]byte
	CRC32 uint32
}

// RecoverySliceRef locates one RecvSlic payload without loading it: the
// payload lives at BodyOffset within the FileIndex-th input stream and is
// exactly SliceSize bytes long.
type RecoverySliceRef struct {
	Exponent   uint32
	FileIndex  int
	BodyOffset int64
}

// Index is the parsed, recovery-relevant view of a PAR2 set: everything
// needed to plan and run a repair except the recovery payloads themselves.
type Index struct {
	SliceSize   uint64
	RecoveryIDs [][16]byte // recovery-set FileIDs, FileID-ascending
	Files       map[[16]byte]FileDescriptor
	SliceChecks map[[16]byte][]SliceCheck
	Recovery    []RecoverySliceRef
}

// ParseIndex scans PAR2 packets across the given streams (typically the
// .par2 index file followed by every .volXX+YY.par2 file, in a stable order —
// RecoverySliceRef.FileIndex refers to positions in this slice). Recovery
// slice payloads are skipped, not loaded; only their locations are recorded.
func ParseIndex(streams []io.Reader) (*Index, error) {
	idx := &Index{
		Files:       make(map[[16]byte]FileDescriptor),
		SliceChecks: make(map[[16]byte][]SliceCheck),
	}
	var mainSeen bool

	for fi, stream := range streams {
		cr := &countingReader{r: stream}
		pr := NewPacketReader(cr)
		for {
			header, err := pr.ReadHeader()
			if err == io.EOF {
				break
			}
			if err != nil {
				if errIsUnexpectedEOF(err) {
					break
				}
				return nil, fmt.Errorf("par2 index: stream %d: %w", fi, err)
			}
			bodyLen := int64(header.Length) - PacketHeaderSize

			switch header.Type {
			case PacketTypePARMain:
				if err := parseMainBody(pr.r, bodyLen, idx); err != nil {
					return nil, fmt.Errorf("par2 index: stream %d: %w", fi, err)
				}
				mainSeen = true

			case PacketTypeFileDesc:
				desc, err := pr.ReadFileDescriptor(header)
				if err != nil {
					return nil, fmt.Errorf("par2 index: stream %d: %w", fi, err)
				}
				idx.Files[desc.FileID] = *desc

			case PacketTypeIFSC:
				if err := parseIFSCBody(pr.r, bodyLen, idx); err != nil {
					return nil, fmt.Errorf("par2 index: stream %d: %w", fi, err)
				}

			case PacketTypeRecoverySlice:
				if bodyLen < 4 {
					return nil, fmt.Errorf("par2 index: stream %d: RecvSlic body too small: %d", fi, bodyLen)
				}
				var exponent uint32
				if err := binary.Read(pr.r, binary.LittleEndian, &exponent); err != nil {
					return nil, fmt.Errorf("par2 index: stream %d: read RecvSlic exponent: %w", fi, err)
				}
				idx.Recovery = append(idx.Recovery, RecoverySliceRef{
					Exponent:   exponent,
					FileIndex:  fi,
					BodyOffset: cr.n,
				})
				if err := cr.skip(bodyLen - 4); err != nil {
					return nil, fmt.Errorf("par2 index: stream %d: skip RecvSlic payload: %w", fi, err)
				}

			default:
				if err := pr.SkipPacketBody(header); err != nil {
					return nil, fmt.Errorf("par2 index: stream %d: %w", fi, err)
				}
			}
		}
	}

	if !mainSeen {
		return nil, fmt.Errorf("par2 index: no Main packet found")
	}
	if err := validateIndex(idx); err != nil {
		return nil, err
	}
	return idx, nil
}

// parseMainBody reads slice size + recovery-set FileIDs. Non-recovery-set
// FileIDs at the tail of the body are skipped.
func parseMainBody(r io.Reader, bodyLen int64, idx *Index) error {
	if bodyLen < 12 {
		return fmt.Errorf("main packet body too small: %d", bodyLen)
	}
	var head struct {
		SliceSize uint64
		NumFiles  uint32
	}
	if err := binary.Read(r, binary.LittleEndian, &head); err != nil {
		return fmt.Errorf("read main packet: %w", err)
	}
	if head.SliceSize == 0 || head.SliceSize%4 != 0 {
		return fmt.Errorf("invalid slice size %d", head.SliceSize)
	}
	rest := bodyLen - 12
	if int64(head.NumFiles)*16 > rest {
		return fmt.Errorf("main packet declares %d recovery files but body has %d bytes", head.NumFiles, rest)
	}
	idx.SliceSize = head.SliceSize
	idx.RecoveryIDs = make([][16]byte, head.NumFiles)
	for i := range idx.RecoveryIDs {
		if _, err := io.ReadFull(r, idx.RecoveryIDs[i][:]); err != nil {
			return fmt.Errorf("read recovery-set FileID %d: %w", i, err)
		}
	}
	sort.Slice(idx.RecoveryIDs, func(i, j int) bool {
		return bytes.Compare(idx.RecoveryIDs[i][:], idx.RecoveryIDs[j][:]) < 0
	})
	// Skip trailing non-recovery-set FileIDs.
	if skip := rest - int64(head.NumFiles)*16; skip > 0 {
		if _, err := io.CopyN(io.Discard, r, skip); err != nil {
			return fmt.Errorf("skip non-recovery FileIDs: %w", err)
		}
	}
	return nil
}

// parseIFSCBody reads a file's per-slice checksums.
func parseIFSCBody(r io.Reader, bodyLen int64, idx *Index) error {
	if bodyLen < 16 || (bodyLen-16)%20 != 0 {
		return fmt.Errorf("ifsc packet body has invalid size %d", bodyLen)
	}
	var fileID [16]byte
	if _, err := io.ReadFull(r, fileID[:]); err != nil {
		return fmt.Errorf("read IFSC FileID: %w", err)
	}
	n := (bodyLen - 16) / 20
	checks := make([]SliceCheck, n)
	buf := make([]byte, 20)
	for i := range checks {
		if _, err := io.ReadFull(r, buf); err != nil {
			return fmt.Errorf("read IFSC entry %d: %w", i, err)
		}
		copy(checks[i].MD5[:], buf[:16])
		checks[i].CRC32 = binary.LittleEndian.Uint32(buf[16:])
	}
	idx.SliceChecks[fileID] = checks
	return nil
}

// validateIndex checks that every recovery-set member has a FileDesc and a
// slice-count-consistent IFSC.
func validateIndex(idx *Index) error {
	for _, id := range idx.RecoveryIDs {
		fd, ok := idx.Files[id]
		if !ok {
			return fmt.Errorf("par2 index: recovery-set member %x has no FileDesc packet", id)
		}
		checks, ok := idx.SliceChecks[id]
		if !ok {
			return fmt.Errorf("par2 index: file %q has no IFSC packet", fd.Name)
		}
		want := int((fd.Length + idx.SliceSize - 1) / idx.SliceSize)
		if len(checks) != want {
			return fmt.Errorf("par2 index: file %q has %d slice checksums, want %d", fd.Name, len(checks), want)
		}
	}
	return nil
}

// countingReader tracks the absolute byte position within a stream so
// RecvSlic payload offsets can be recorded. When the underlying stream can
// Seek, skips avoid reading the skipped bytes — this is what keeps ParseIndex
// from downloading every recovery payload when streams are lazy article
// readers over NNTP.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// skip advances past n bytes, seeking when possible.
func (c *countingReader) skip(n int64) error {
	if n <= 0 {
		return nil
	}
	if s, ok := c.r.(io.Seeker); ok {
		if _, err := s.Seek(n, io.SeekCurrent); err == nil {
			c.n += n
			return nil
		}
	}
	_, err := io.CopyN(io.Discard, c.r, n)
	c.n += n
	return err
}

// errIsUnexpectedEOF reports whether err wraps an EOF that occurred at a
// packet boundary read (a cleanly-truncated trailing stream is tolerated;
// mid-packet truncation surfaces as a hard error from the body parsers).
func errIsUnexpectedEOF(err error) bool {
	return err != nil && (err == io.EOF ||
		bytes.Contains([]byte(err.Error()), []byte("EOF")))
}
