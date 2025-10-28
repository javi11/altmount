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

	// Read fixed fields (56 bytes total)
	if err := binary.Read(pr.r, binary.LittleEndian, &desc.FileID); err != nil {
		return nil, fmt.Errorf("failed to read FileID: %w", err)
	}
	if err := binary.Read(pr.r, binary.LittleEndian, &desc.FileMD5); err != nil {
		return nil, fmt.Errorf("failed to read FileMD5: %w", err)
	}
	if err := binary.Read(pr.r, binary.LittleEndian, &desc.Hash16k); err != nil {
		return nil, fmt.Errorf("failed to read Hash16k: %w", err)
	}
	if err := binary.Read(pr.r, binary.LittleEndian, &desc.Length); err != nil {
		return nil, fmt.Errorf("failed to read Length: %w", err)
	}

	// Read filename (remaining bytes, null-terminated, 4-byte aligned)
	filenameLength := bodyLength - 56
	if filenameLength > 0 {
		filenameBytes := make([]byte, filenameLength)
		if _, err := io.ReadFull(pr.r, filenameBytes); err != nil {
			return nil, fmt.Errorf("failed to read filename: %w", err)
		}

		// Find the actual end of the filename (remove null bytes and padding)
		actualLength := filenameLength
		for i := len(filenameBytes) - 1; i >= 0; i-- {
			if filenameBytes[i] == 0 || filenameBytes[i] < 32 {
				actualLength = uint64(i)
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

	_, err := io.CopyN(io.Discard, pr.r, int64(remainingBytes))
	if err != nil {
		return fmt.Errorf("failed to skip packet body: %w", err)
	}

	return nil
}
