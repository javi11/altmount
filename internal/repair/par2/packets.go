package par2

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
)

// PAR2 packet framing constants.
var par2Magic = []byte("PAR2\x00PKT")

const par2HeaderLen = 64 // magic(8)+len(8)+md5(16)+setid(16)+type(16)

// 16-byte packet type identifiers.
var (
	typeMain     = [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'M', 'a', 'i', 'n', 0, 0, 0, 0}
	typeFileDesc = [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'F', 'i', 'l', 'e', 'D', 'e', 's', 'c'}
	typeIFSC     = [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'I', 'F', 'S', 'C', 0, 0, 0, 0}
	typeRecvSlic = [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'R', 'e', 'c', 'v', 'S', 'l', 'i', 'c'}
)

// Errors returned by the parser.
var (
	ErrNoMainPacket = errors.New("par2: no Main packet found")
	ErrNoSliceSize  = errors.New("par2: invalid or zero slice size")
)

// FileDesc describes one input file in the recovery set.
type FileDesc struct {
	ID     [16]byte // PAR2 File ID
	MD5    [16]byte // MD5 of the entire file
	MD5_16 [16]byte // MD5 of the first 16 KiB
	Length uint64   // file length in bytes
	Name   string   // file name
}

// SliceChecksum is the verification data for a single input slice.
type SliceChecksum struct {
	MD5   [16]byte
	CRC32 uint32
}

// RecoverySlice is one Reed-Solomon recovery block: parity data tagged with the
// exponent used to generate it.
type RecoverySlice struct {
	Exponent uint32
	Data     []byte
}

// RecoverySet is the parsed, de-duplicated contents of one PAR2 recovery set.
type RecoverySet struct {
	SetID           [16]byte
	SliceSize       uint64
	RecoveryFileIDs [][16]byte // recovery-set file IDs, in Main-packet order
	Files           map[[16]byte]*FileDesc
	SliceCRCs       map[[16]byte][]SliceChecksum // per File ID, slice-ordered
	Recovery        []RecoverySlice
}

// Parse reads a concatenation of PAR2 packet bytes (one or more .par2 files
// joined together) and returns the recovery set described by the Main packet.
// Duplicate packets — which PAR2 deliberately repeats across volume files — are
// de-duplicated by their packet MD5.
func Parse(data []byte) (*RecoverySet, error) {
	rs := &RecoverySet{
		Files:     make(map[[16]byte]*FileDesc),
		SliceCRCs: make(map[[16]byte][]SliceChecksum),
	}
	seen := make(map[[16]byte]struct{})
	haveMain := false

	// Non-Main packets are buffered and parsed only after the loop, once the
	// Main packet's SetID is known: a concatenation of .par2 files from different
	// sets must not mix foreign FileDesc/IFSC/recovery packets into this set.
	// Main may not be the first packet, so we can't filter inline.
	type pendingPkt struct {
		setid [16]byte
		ptype [16]byte
		body  []byte
	}
	var pending []pendingPkt

	i := 0
	for i+par2HeaderLen <= len(data) {
		if !bytes.Equal(data[i:i+8], par2Magic) {
			i++ // resync: scan forward a byte at a time until the next magic
			continue
		}
		plen := binary.LittleEndian.Uint64(data[i+8 : i+16])
		if plen < par2HeaderLen || plen%4 != 0 || i+int(plen) > len(data) {
			i += 8 // malformed length; skip past this magic and resync
			continue
		}
		pkt := data[i : i+int(plen)]
		i += int(plen)

		// Verify the packet MD5 (covers everything after the MD5 field).
		var sum [16]byte
		copy(sum[:], pkt[16:32])
		if md5.Sum(pkt[32:]) != sum {
			continue // corrupt packet; ignore it
		}
		if _, dup := seen[sum]; dup {
			continue
		}
		seen[sum] = struct{}{}

		var ptype [16]byte
		copy(ptype[:], pkt[48:64])
		body := pkt[64:]

		switch ptype {
		case typeMain:
			if err := rs.parseMain(pkt, body); err != nil {
				return nil, err
			}
			haveMain = true
		case typeFileDesc, typeIFSC, typeRecvSlic:
			var setid [16]byte
			copy(setid[:], pkt[32:48])
			pending = append(pending, pendingPkt{setid: setid, ptype: ptype, body: body})
		}
	}

	if !haveMain {
		return nil, ErrNoMainPacket
	}

	// Parse the buffered data packets that belong to this recovery set.
	for _, p := range pending {
		if p.setid != rs.SetID {
			continue // foreign packet from a different set; ignore
		}
		switch p.ptype {
		case typeFileDesc:
			rs.parseFileDesc(p.body)
		case typeIFSC:
			rs.parseIFSC(p.body)
		case typeRecvSlic:
			rs.parseRecovery(p.body)
		}
	}
	if rs.SliceSize == 0 || rs.SliceSize%4 != 0 {
		return nil, ErrNoSliceSize
	}
	// Every recovery slice must be exactly SliceSize bytes. A truncated or
	// malformed recovery packet (that still passed its own MD5) would otherwise
	// surface much later as a singular-matrix error or, worse, silently skew
	// reconstruction. Fail fast with a diagnosable error instead.
	for _, r := range rs.Recovery {
		if uint64(len(r.Data)) != rs.SliceSize {
			return nil, fmt.Errorf("par2: recovery slice (exponent %d) length %d != slice size %d",
				r.Exponent, len(r.Data), rs.SliceSize)
		}
	}
	return rs, nil
}

func (rs *RecoverySet) parseMain(pkt, body []byte) error {
	if len(body) < 12 {
		return fmt.Errorf("par2: Main packet too short: %d bytes", len(body))
	}
	copy(rs.SetID[:], pkt[32:48])
	rs.SliceSize = binary.LittleEndian.Uint64(body[0:8])
	nRecovery := int(binary.LittleEndian.Uint32(body[8:12]))
	ids := body[12:]
	if len(ids)%16 != 0 || nRecovery*16 > len(ids) {
		return fmt.Errorf("par2: Main packet file-id table malformed")
	}
	rs.RecoveryFileIDs = make([][16]byte, nRecovery)
	for k := 0; k < nRecovery; k++ {
		copy(rs.RecoveryFileIDs[k][:], ids[k*16:k*16+16])
	}
	return nil
}

func (rs *RecoverySet) parseFileDesc(body []byte) {
	if len(body) < 56 {
		return
	}
	fd := &FileDesc{}
	copy(fd.ID[:], body[0:16])
	copy(fd.MD5[:], body[16:32])
	copy(fd.MD5_16[:], body[32:48])
	fd.Length = binary.LittleEndian.Uint64(body[48:56])
	name := body[56:]
	if z := bytes.IndexByte(name, 0); z >= 0 {
		name = name[:z]
	}
	fd.Name = string(name)
	rs.Files[fd.ID] = fd
}

func (rs *RecoverySet) parseIFSC(body []byte) {
	if len(body) < 16 {
		return
	}
	var id [16]byte
	copy(id[:], body[0:16])
	rest := body[16:]
	n := len(rest) / 20
	sums := make([]SliceChecksum, n)
	for k := 0; k < n; k++ {
		off := k * 20
		copy(sums[k].MD5[:], rest[off:off+16])
		sums[k].CRC32 = binary.LittleEndian.Uint32(rest[off+16 : off+20])
	}
	rs.SliceCRCs[id] = sums
}

func (rs *RecoverySet) parseRecovery(body []byte) {
	if len(body) < 4 {
		return
	}
	data := make([]byte, len(body)-4)
	copy(data, body[4:])
	rs.Recovery = append(rs.Recovery, RecoverySlice{
		Exponent: binary.LittleEndian.Uint32(body[0:4]),
		Data:     data,
	})
}
