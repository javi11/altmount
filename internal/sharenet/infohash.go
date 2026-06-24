package sharenet

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
)

const namespace = "altmount-v1:"

// Infohash derives the private DHT infohash for a release.
// The namespace prefix makes it invisible to public BitTorrent queries.
// Same releaseHash always produces the same 20-byte key.
func Infohash(releaseHash string) [20]byte {
	return sha1.Sum([]byte(namespace + releaseHash))
}

// ComputeReleaseHash returns the hex-encoded SHA256 of the NZB XML content.
// Two nodes with the same NZB always derive the same release hash.
func ComputeReleaseHash(nzbXML []byte) string {
	sum := sha256.Sum256(nzbXML)
	return hex.EncodeToString(sum[:])
}
