// Package duplicatelookup identifies directly posted files by sampled content.
package duplicatelookup

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const Algorithm = "sample-sha256-v1"

type Request struct {
	Algorithm string `json:"algorithm"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Hash      string `json:"hash"`
}

func Validate(r Request) error {
	if r.Algorithm != Algorithm {
		return fmt.Errorf("unsupported fingerprint algorithm")
	}
	if r.Name == "" || r.Name == "." || r.Name == ".." || strings.ContainsAny(r.Name, "/\\") || strings.IndexFunc(r.Name, unicode.IsControl) >= 0 {
		return fmt.Errorf("name must be an exact basename")
	}
	if r.Size < 0 {
		return fmt.Errorf("size must be nonnegative")
	}
	if len(r.Hash) != 64 || strings.Trim(r.Hash, "0123456789abcdef") != "" {
		return fmt.Errorf("hash must be 64 lowercase hexadecimal characters")
	}
	return nil
}
func Fingerprint(r io.ReaderAt, size int64) (string, error) {
	if size < 0 {
		return "", fmt.Errorf("negative size")
	}
	h := sha256.New()
	h.Write([]byte(Algorithm + "\x00"))
	var word [8]byte
	binary.LittleEndian.PutUint64(word[:], uint64(size))
	h.Write(word[:])
	samples := [][2]int64{{0, size}}
	if size > 196608 {
		samples = [][2]int64{{0, 65536}, {(size - 65536) / 2, 65536}, {size - 65536, 65536}}
	}
	for _, s := range samples {
		for _, v := range s {
			binary.LittleEndian.PutUint64(word[:], uint64(v))
			h.Write(word[:])
		}
		if _, err := io.CopyN(h, io.NewSectionReader(r, s[0], s[1]), s[1]); err != nil {
			return "", fmt.Errorf("read fingerprint sample: %w", err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
