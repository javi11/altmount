package par2

import (
	"encoding/binary"
	"fmt"
	"io"
)

// PacketReader provides streaming access to PAR2 packets
// Reference: https://github.com/akalin/gopar/blob/main/par2/packet.go
type PacketReader struct {
	r io.Reader
}

// NewPacketReader creates a new PAR2 packet reader
func NewPacketReader(r io.Reader) *PacketReader {
	return &PacketReader{r: r}
}

// ReadHeader reads and validates a PAR2 packet header from the stream
func (pr *PacketReader) ReadHeader() (*PacketHeader, error) {
	header := &PacketHeader{}

	// Read the header (64 bytes total)
	if err := binary.Read(pr.r, binary.LittleEndian, header); err != nil {
		return nil, fmt.Errorf("failed to read PAR2 header: %w", err)
	}

	// Validate magic signature
	if header.Magic != MagicBytes {
		return nil, fmt.Errorf("invalid PAR2 magic signature")
	}

	// Validate packet length (must be at least header size and multiple of 4)
	if header.Length < PacketHeaderSize {
		return nil, fmt.Errorf("invalid packet length: %d (minimum %d)", header.Length, PacketHeaderSize)
	}

	if header.Length%4 != 0 {
		return nil, fmt.Errorf("packet length %d is not a multiple of 4", header.Length)
	}

	const maxPacketSize = 1024 * 1024 * 1024
	if header.Length > maxPacketSize {
		return nil, fmt.Errorf("packet length too large: %d bytes (max %d)", header.Length, maxPacketSize)
	}

	return header, nil
}

// ReadFileDescriptor reads a file descriptor from a FileDesc packet body
// The header must have already been read and validated as a FileDesc packet
// Reference: https://github.com/akalin/gopar/blob/main/par2/file_description_packet.go
func (pr *PacketReader) ReadFileDescriptor(header *PacketHeader) (*FileDescriptor, error) {
	// Validate this is a FileDesc packet
	if header.Type != PacketTypeFileDesc {
		return nil, fmt.Errorf("not a FileDesc packet")
	}

	// Calculate remaining bytes after header
	bodyLength := header.Length - PacketHeaderSize
	if bodyLength < 56 { // Minimum: FileID (16) + FileMD5 (16) + Hash16k (16) + Length (8) = 56 bytes
		return nil, fmt.Errorf("file description packet too small: %d bytes", bodyLength)
	}

	desc := &FileDescriptor{}

	// Read all fixed fields (56 bytes total) in one call
	var body struct {
		FileID  [16]byte
		FileMD5 [16]byte
		Hash16k [16]byte
		Length  uint64
	}
	if err := binary.Read(pr.r, binary.LittleEndian, &body); err != nil {
		return nil, fmt.Errorf("failed to read file descriptor body: %w", err)
	}
	desc.FileID = body.FileID
	desc.FileMD5 = body.FileMD5
	desc.Hash16k = body.Hash16k
	desc.Length = body.Length

	// Read filename (remaining bytes, null-terminated, 4-byte aligned)
	filenameLength := bodyLength - 56
	if filenameLength > 0 {
		const maxFilenameLength = 64 * 1024
		if filenameLength > maxFilenameLength {
			return nil, fmt.Errorf("filename length too large: %d bytes (max %d)", filenameLength, maxFilenameLength)
		}

		filenameBytes := make([]byte, int(filenameLength))
		if _, err := io.ReadFull(pr.r, filenameBytes); err != nil {
			return nil, fmt.Errorf("failed to read filename: %w", err)
		}

		// Find the actual end of the filename (remove null bytes and padding)
		actualLength := int(filenameLength)
		for i := len(filenameBytes) - 1; i >= 0; i-- {
			if filenameBytes[i] == 0 || filenameBytes[i] < 32 {
				actualLength = i
			} else {
				break
			}
		}

		desc.Name = string(filenameBytes[:actualLength])
	}

	return desc, nil
}

// SkipPacketBody skips the body of a packet (everything after the header)
func (pr *PacketReader) SkipPacketBody(header *PacketHeader) error {
	remainingBytes := header.Length - PacketHeaderSize
	if remainingBytes == 0 {
		return nil
	}

	const maxPacketBodySize = 1024 * 1024 * 1024
	if remainingBytes > maxPacketBodySize {
		return fmt.Errorf("packet body too large: %d bytes (max %d)", remainingBytes, maxPacketBodySize)
	}

	_, err := io.CopyN(io.Discard, pr.r, int64(remainingBytes))
	if err != nil {
		return fmt.Errorf("failed to skip packet body: %w", err)
	}

	return nil
}
