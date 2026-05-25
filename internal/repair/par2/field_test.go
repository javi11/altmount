package par2

import "testing"

func TestFieldInverseAndDivision(t *testing.T) {
	for _, a := range []uint16{1, 2, 3, 255, 256, 12345, 65535} {
		if got := gfMul(a, gfInv(a)); got != 1 {
			t.Errorf("a * inv(a) = %d, want 1 (a=%d)", got, a)
		}
		for _, b := range []uint16{1, 7, 99, 40000, 65535} {
			prod := gfMul(a, b)
			if got := gfDiv(prod, b); got != a {
				t.Errorf("(a*b)/b = %d, want %d (a=%d b=%d)", got, a, a, b)
			}
		}
	}
}

func TestFieldMulIdentityAndZero(t *testing.T) {
	for _, a := range []uint16{0, 1, 2, 500, 65535} {
		if got := gfMul(a, 1); got != a {
			t.Errorf("a*1 = %d, want %d", got, a)
		}
		if got := gfMul(a, 0); got != 0 {
			t.Errorf("a*0 = %d, want 0", got)
		}
	}
}

func TestFieldPow(t *testing.T) {
	if got := gfPow(12345, 0); got != 1 {
		t.Errorf("x^0 = %d, want 1", got)
	}
	// base^exp via repeated multiplication must match gfPow.
	for _, base := range []uint16{2, 3, 257} {
		want := uint16(1)
		for e := 0; e < 20; e++ {
			if got := gfPow(base, e); got != want {
				t.Fatalf("gfPow(%d,%d)=%d, want %d", base, e, got, want)
			}
			want = gfMul(want, base)
		}
	}
	// Generator powers must equal the exp table.
	for e := 0; e < 100; e++ {
		if got := gfPow(2, e); got != gfExp[e%gfMax] {
			t.Fatalf("gfPow(2,%d)=%d, want %d", e, got, gfExp[e%gfMax])
		}
	}
}

func TestFieldDistributive(t *testing.T) {
	// a*(b+c) == a*b + a*c  (+ is XOR)
	a, b, c := uint16(4321), uint16(9999), uint16(123)
	lhs := gfMul(a, gfAdd(b, c))
	rhs := gfAdd(gfMul(a, b), gfMul(a, c))
	if lhs != rhs {
		t.Errorf("distributive law failed: %d != %d", lhs, rhs)
	}
}

func TestMulSliceInto(t *testing.T) {
	// dst ^= c*src on a couple of words, cross-checked against gfMul.
	src := []byte{0x34, 0x12, 0xff, 0x00, 0x01, 0x80}
	dst := make([]byte, len(src))
	const c = uint16(7)
	mulSliceInto(dst, src, c)
	w := wordsLE(src)
	out := wordsLE(dst)
	for i := range w {
		if want := gfMul(w[i], c); out[i] != want {
			t.Errorf("word %d: got %d want %d", i, out[i], want)
		}
	}
}
