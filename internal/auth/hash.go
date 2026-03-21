package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashAPIKey generates a SHA256 hash of the API key for secure comparison.
func HashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}
