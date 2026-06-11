// Package par2gen builds minimal valid PAR2 index files in memory for use in
// tests that exercise PAR2-based filename deobfuscation.
//
// Only FileDesc packets are emitted — enough for the par2.GetFileDescriptors
// path that reconstructs real filenames from obfuscated Usenet releases.
// No recovery blocks, IFSC, or Main packets are generated; the par2 reader
// used by altmount ignores those packet types.
package par2gen

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
)

// FileEntry describes one file that should appear in the generated PAR2 index.
type FileEntry struct {
	// Name is the real (unobfuscated) filename.
	Name string
	// Content is the file payload. The first 16 KB (zero-padded to exactly
	// 16384 bytes) determines Hash16k, which is how the importer matches
	// obfuscated files to their real names.
	Content []byte
}

// Build returns the binary content of a minimal PAR2 index containing one
// FileDesc packet per entry. The result can be served directly from fakepool
// as the payload for a PAR2 index segment.
func Build(entries ...FileEntry) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		writeFileDesc(&buf, e)
	}
	return buf.Bytes()
}

// writeFileDesc emits a single PAR2 FileDesc packet to w.
func writeFileDesc(w *bytes.Buffer, e FileEntry) {
	// --- compute fields ---

	// Hash16k: MD5 of first 16384 bytes, zero-padded.
	padded := make([]byte, 16384)
	copy(padded, e.Content)
	hash16k := md5.Sum(padded)

	// FileMD5: MD5 of full content.
	fileMD5 := md5.Sum(e.Content)

	// Name bytes with 4-byte alignment (null-padded).
	nameBytes := []byte(e.Name)
	alignedNameLen := (len(nameBytes) + 3) &^ 3
	paddedName := make([]byte, alignedNameLen)
	copy(paddedName, nameBytes)

	// FileID: MD5 of (Hash16k[16] || Length_le64[8] || name_bytes).
	var fileIDSrc bytes.Buffer
	fileIDSrc.Write(hash16k[:])
	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(e.Content)))
	fileIDSrc.Write(lenBuf[:])
	fileIDSrc.Write(nameBytes)
	fileID := md5.Sum(fileIDSrc.Bytes())

	// --- packet layout ---
	//
	// Header (64 bytes):
	//   Magic[8]      = "PAR2\0PKT"
	//   Length[8]     = total packet length (header + body)
	//   MD5Hash[16]   = MD5(packet[32:])
	//   RecoveryID[16] = arbitrary
	//   Type[16]      = "PAR 2.0\0FileDesc"
	//
	// Body (56 + alignedNameLen bytes):
	//   FileID[16] + FileMD5[16] + Hash16k[16] + Length[8] + name(aligned)

	const headerSize = 64
	bodySize := 16 + 16 + 16 + 8 + alignedNameLen
	totalLen := uint64(headerSize + bodySize)

	// Build the body first so we can compute the packet MD5Hash.
	var body bytes.Buffer
	body.Write(fileID[:])
	body.Write(fileMD5[:])
	body.Write(hash16k[:])
	binary.Write(&body, binary.LittleEndian, uint64(len(e.Content))) //nolint:errcheck
	body.Write(paddedName)

	// Build the tail of the header (bytes 32-63) + body for MD5 computation.
	magic := [8]byte{'P', 'A', 'R', '2', 0, 'P', 'K', 'T'}
	recoveryID := [16]byte{} // zero — tests don't need a specific set ID
	packetType := [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'F', 'i', 'l', 'e', 'D', 'e', 's', 'c'}

	var md5Input bytes.Buffer
	md5Input.Write(recoveryID[:])
	md5Input.Write(packetType[:])
	md5Input.Write(body.Bytes())
	packetMD5 := md5.Sum(md5Input.Bytes())

	// Write the full packet.
	w.Write(magic[:])
	binary.Write(w, binary.LittleEndian, totalLen) //nolint:errcheck
	w.Write(packetMD5[:])
	w.Write(recoveryID[:])
	w.Write(packetType[:])
	w.Write(body.Bytes())
}
