package aes

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
)

// aesDecryptReader wraps an io.ReadCloser with AES-CBC decryption
// Based on the implementation from rardecode example: github.com/javi11/rardecode/blob/main/examples/rarextract/main.go
type aesDecryptReader struct {
	ctx       context.Context
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error)
	source    io.ReadCloser
	key       []byte
	iv        []byte
	origIV    []byte // Original IV for recalculation during seeks
	decrypter cipher.BlockMode
	buffer    []byte // Buffer for partial block reads
	bufferPos int    // Current position in buffer
	bufferLen int    // Length of valid data in buffer
	offset    int64  // Current read position
	size      int64  // Total size of decrypted data
	closed    bool
}

// newAesDecryptReader creates a new AES-CBC decrypt reader
func newAesDecryptReader(
	ctx context.Context,
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
	key, iv []byte,
	size int64,
) (*aesDecryptReader, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid AES key size: %d (must be 16, 24, or 32 bytes)", len(key))
	}

	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid IV size: %d (must be %d bytes)", len(iv), aes.BlockSize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Make a copy of IV since CBC mode modifies it
	ivCopy := make([]byte, len(iv))
	copy(ivCopy, iv)

	return &aesDecryptReader{
		ctx:       ctx,
		getReader: getReader,
		source:    nil, // Will be initialized lazily on first read
		key:       key,
		iv:        ivCopy,
		origIV:    iv,
		decrypter: cipher.NewCBCDecrypter(block, ivCopy),
		buffer:    make([]byte, aes.BlockSize*64), // Buffer multiple blocks for efficiency
		size:      size,
	}, nil
}

// Read implements io.Reader
func (r *aesDecryptReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}

	// Lazy initialization of source reader
	if r.source == nil {
		var err error
		r.source, err = r.getReader(r.ctx, 0, r.size-1)
		if err != nil {
			return 0, fmt.Errorf("failed to initialize source reader: %w", err)
		}
	}

	totalRead := 0

	for totalRead < len(p) {
		// First, drain any buffered data
		if r.bufferPos < r.bufferLen {
			n := copy(p[totalRead:], r.buffer[r.bufferPos:r.bufferLen])
			r.bufferPos += n
			r.offset += int64(n)
			totalRead += n
			continue
		}

		// Need to read more data
		// Read in multiples of AES block size
		readSize := len(r.buffer)
		if r.offset+int64(readSize) > r.size {
			readSize = int(r.size - r.offset)
			// Round up to block size
			if readSize%aes.BlockSize != 0 {
				readSize += aes.BlockSize - (readSize % aes.BlockSize)
			}
		}

		if readSize == 0 {
			if totalRead > 0 {
				return totalRead, nil
			}
			return 0, io.EOF
		}

		// Read encrypted data
		encBuf := make([]byte, readSize)
		n, err := io.ReadFull(r.source, encBuf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return totalRead, err
		}

		if n == 0 {
			if totalRead > 0 {
				return totalRead, nil
			}
			return 0, io.EOF
		}

		// Ensure we have a complete block
		if n%aes.BlockSize != 0 {
			n = (n / aes.BlockSize) * aes.BlockSize
		}

		if n > 0 {
			// Decrypt the data in-place
			r.decrypter.CryptBlocks(encBuf[:n], encBuf[:n])

			// Calculate how much decrypted data is actually part of the file
			decryptedLen := n
			if r.offset+int64(n) > r.size {
				decryptedLen = int(r.size - r.offset)
			}

			// Copy to buffer
			copy(r.buffer, encBuf[:decryptedLen])
			r.bufferLen = decryptedLen
			r.bufferPos = 0
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			if r.bufferLen == 0 {
				if totalRead > 0 {
					return totalRead, nil
				}
				return 0, io.EOF
			}
		}
	}

	return totalRead, nil
}

// Seek implements io.Seeker
// For CBC mode, seeking requires recalculating the IV based on the previous ciphertext block
func (r *aesDecryptReader) Seek(offset int64, whence int) (int64, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}

	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.offset + offset
	case io.SeekEnd:
		abs = r.size + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if abs < 0 {
		return 0, fmt.Errorf("negative seek position: %d", abs)
	}

	if abs > r.size {
		return 0, fmt.Errorf("seek beyond end of file: %d > %d", abs, r.size)
	}

	// If seeking to current position, nothing to do
	if abs == r.offset {
		return abs, nil
	}

	// Close the current source if it exists
	if r.source != nil {
		r.source.Close()
		r.source = nil
	}

	// Seeking requires recreating the decrypter with the correct IV
	// For CBC mode: IV for block N = ciphertext of block N-1
	blockNum := abs / int64(aes.BlockSize)
	blockOffset := abs % int64(aes.BlockSize)

	// Recreate IV by reading the previous ciphertext block
	var newIV []byte
	if blockNum == 0 {
		// First block uses original IV
		newIV = make([]byte, len(r.origIV))
		copy(newIV, r.origIV)
	} else {
		// Need to read the previous block's ciphertext to use as IV
		prevBlockOffset := (blockNum - 1) * int64(aes.BlockSize)
		prevBlockEnd := prevBlockOffset + int64(aes.BlockSize) - 1

		// Get a reader for the previous block
		prevBlockReader, err := r.getReader(r.ctx, prevBlockOffset, prevBlockEnd)
		if err != nil {
			return 0, fmt.Errorf("failed to get reader for IV block: %w", err)
		}
		defer prevBlockReader.Close()

		// Read previous block as new IV
		newIV = make([]byte, aes.BlockSize)
		_, err = io.ReadFull(prevBlockReader, newIV)
		if err != nil {
			return 0, fmt.Errorf("failed to read IV block: %w", err)
		}
	}

	// Get a new reader starting at the target block
	sourceOffset := blockNum * int64(aes.BlockSize)
	newSource, err := r.getReader(r.ctx, sourceOffset, r.size-1)
	if err != nil {
		return 0, fmt.Errorf("failed to get reader for seek position: %w", err)
	}

	// Recreate decrypter with new IV
	block, err := aes.NewCipher(r.key)
	if err != nil {
		newSource.Close()
		return 0, fmt.Errorf("failed to recreate cipher: %w", err)
	}

	r.source = newSource
	r.iv = newIV
	r.decrypter = cipher.NewCBCDecrypter(block, newIV)
	r.offset = blockNum * int64(aes.BlockSize)

	// Reset buffer
	r.bufferPos = 0
	r.bufferLen = 0

	// If we need to skip bytes within the block
	if blockOffset > 0 {
		skipBuf := make([]byte, blockOffset)
		_, err := io.ReadFull(r, skipBuf)
		if err != nil {
			return 0, fmt.Errorf("failed to skip to offset in block: %w", err)
		}
	}

	return abs, nil
}

// Close implements io.Closer
func (r *aesDecryptReader) Close() error {
	if r.closed {
		return nil
	}

	r.closed = true
	if r.source != nil {
		return r.source.Close()
	}

	return nil
}
