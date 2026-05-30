package segments

import (
	"bytes"
	"strings"
	"testing"
)

// TestMessageID_FormatStableAndSortable pins the canonical form so other
// test packages can construct IDs without importing this helper if they
// prefer raw strings — the format is part of the public contract.
func TestMessageID_FormatStableAndSortable(t *testing.T) {
	t.Parallel()
	a := MessageID(7)
	b := MessageID(42)
	wantA := "altmount-test-seg-000007@fake"
	wantB := "altmount-test-seg-000042@fake"
	if a != wantA {
		t.Errorf("MessageID(7) = %q, want %q", a, wantA)
	}
	if b != wantB {
		t.Errorf("MessageID(42) = %q, want %q", b, wantB)
	}
	if a >= b {
		t.Errorf("lexicographic order broken: %q >= %q", a, b)
	}
	if !strings.HasPrefix(a, MessageIDPrefix) {
		t.Errorf("MessageID missing prefix %q", MessageIDPrefix)
	}
}

// TestPayload_HeaderEncodesIndex makes the diagnostic property explicit:
// the first bytes of every payload must identify its segment so test
// failures can be inspected by hexdump alone.
func TestPayload_HeaderEncodesIndex(t *testing.T) {
	t.Parallel()
	p := Payload(123, 100)
	if len(p) != 100 {
		t.Fatalf("len = %d, want 100", len(p))
	}
	if !bytes.HasPrefix(p, []byte("seg-000123")) {
		t.Errorf("payload header = %q, want prefix %q", p[:10], "seg-000123")
	}
}

// TestPayload_DeterministicAcrossCalls pins reproducibility — the storming
// tests rely on this so that re-running them with a different seed isn't
// possible by accident.
func TestPayload_DeterministicAcrossCalls(t *testing.T) {
	t.Parallel()
	p1 := Payload(5, 200)
	p2 := Payload(5, 200)
	if !bytes.Equal(p1, p2) {
		t.Errorf("Payload(5, 200) is non-deterministic")
	}
}

// TestFileBytes_ConcatenatesInOrder catches the obvious off-by-one in the
// concatenator and serves as the reassembly oracle for end-to-end tests.
func TestFileBytes_ConcatenatesInOrder(t *testing.T) {
	t.Parallel()
	got := FileBytes(3, 50)
	want := append(append(Payload(0, 50), Payload(1, 50)...), Payload(2, 50)...)
	if !bytes.Equal(got, want) {
		t.Errorf("FileBytes(3, 50) mismatch")
	}
}
