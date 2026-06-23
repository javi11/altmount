package sharenet_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

func TestInfohash_Deterministic(t *testing.T) {
	h1 := sharenet.Infohash("abc123")
	h2 := sharenet.Infohash("abc123")
	if h1 != h2 {
		t.Fatalf("same input must produce same infohash: %x vs %x", h1, h2)
	}
}

func TestInfohash_Distinct(t *testing.T) {
	h1 := sharenet.Infohash("abc123")
	h2 := sharenet.Infohash("def456")
	if h1 == h2 {
		t.Fatal("different inputs must produce different infohashes")
	}
}

func TestInfohash_NotZero(t *testing.T) {
	h := sharenet.Infohash("abc123")
	var zero [20]byte
	if h == zero {
		t.Fatal("infohash must not be zero")
	}
}

func TestComputeReleaseHash_Deterministic(t *testing.T) {
	data := []byte("<?xml version=\"1.0\"?><nzb/>")
	h1 := sharenet.ComputeReleaseHash(data)
	h2 := sharenet.ComputeReleaseHash(data)
	if h1 != h2 {
		t.Fatalf("same content must produce same hash: %s vs %s", h1, h2)
	}
}

func TestComputeReleaseHash_KnownValue(t *testing.T) {
	data := []byte("hello")
	got := sharenet.ComputeReleaseHash(data)
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
