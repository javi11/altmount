package duplicatelookup

import (
	"bytes"
	"strings"
	"testing"
)

func TestFingerprintVectors(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{{0, "05a8b6e0a73e1eea7da7d93b345eb2e99c9e71b6c34e0ae42fde07302a5371d9"}, {3, "91faf454819e22599248399163fc7ae5a917c5e573b1fe2de6f7fe05872a6b9b"}, {196608, "20f87644845ab1ca1f301d85f83a1ca9e9ed9d5c6f86e589d02a945a39f8a740"}, {196609, "d2c3b849b00a87e9bb79ad28d2b820c74777911e4eb682d2c5b7b20ac1febbb9"}} {
		data := make([]byte, tc.n)
		for i := range data {
			data[i] = byte(i % 251)
		}
		got, err := Fingerprint(bytes.NewReader(data), int64(tc.n))
		if err != nil || got != tc.want {
			t.Fatalf("n=%d got=%s err=%v", tc.n, got, err)
		}
	}
}
func TestFingerprintRejectsShortRead(t *testing.T) {
	if _, err := Fingerprint(bytes.NewReader([]byte{1}), 2); err == nil {
		t.Fatal("accepted short file")
	}
}
func TestValidate(t *testing.T) {
	r := Request{Algorithm: Algorithm, Name: "Movie.mkv", Size: 1, Hash: strings.Repeat("a", 64)}
	if err := Validate(r); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "a\n"} {
		v := r
		v.Name = name
		if Validate(v) == nil {
			t.Fatalf("accepted name %q", name)
		}
	}
	v := r
	v.Size = -1
	if Validate(v) == nil {
		t.Fatal("negative size")
	}
	v = r
	v.Hash = strings.Repeat("A", 64)
	if Validate(v) == nil {
		t.Fatal("uppercase hash")
	}
	v = r
	v.Algorithm = "unknown"
	if Validate(v) == nil {
		t.Fatal("algorithm")
	}
}
