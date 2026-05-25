// Package par2 implements parsing of PAR2 (Parchive 2.0) recovery files and
// Reed-Solomon reconstruction of missing data, for on-the-fly repair of
// corrupt Usenet content during streaming.
//
// The arithmetic here follows the PAR2 specification exactly: a Galois field
// GF(2^16) with the reducing polynomial 0x1100B (x^16 + x^12 + x^3 + x + 1) and
// generator 2. Field elements are 16-bit words; within PAR2 slices they are
// stored little-endian. Addition is XOR.
package par2

const (
	gfSize = 1 << 16    // 65536
	gfMax  = gfSize - 1 // 65535, the multiplicative order
	// gfPoly is the PAR2 reducing polynomial: x^16 + x^12 + x^3 + x + 1.
	gfPoly = 0x1100B
)

var (
	// gfExp[i] = 2^i in GF(2^16) for i in [0, gfMax). gfExp[gfMax] aliases
	// gfExp[0] so callers can index with a raw (already reduced) exponent up to
	// and including gfMax without an extra modulo.
	gfExp [gfSize]uint16
	// gfLog[x] = discrete log of x base 2. gfLog[0] is meaningless (0 has no log).
	gfLog [gfSize]uint16
)

func init() {
	x := 1
	for i := 0; i < gfMax; i++ {
		gfExp[i] = uint16(x)
		gfLog[x] = uint16(i)
		x <<= 1
		if x&gfSize != 0 { // overflowed bit 16 → reduce by the polynomial
			x ^= gfPoly
		}
	}
	gfExp[gfMax] = gfExp[0] // 2^65535 == 2^0 == 1
}

// gfAdd returns a + b in GF(2^16), which is XOR.
func gfAdd(a, b uint16) uint16 { return a ^ b }

// gfMul returns a * b in GF(2^16).
func gfMul(a, b uint16) uint16 {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])+int(gfLog[b]))%gfMax]
}

// gfDiv returns a / b in GF(2^16). b must be non-zero.
func gfDiv(a, b uint16) uint16 {
	if a == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])-int(gfLog[b])+gfMax)%gfMax]
}

// gfPow returns base^exp in GF(2^16) for exp >= 0. base must be non-zero
// (PAR2 base constants are always non-zero).
func gfPow(base uint16, exp int) uint16 {
	if exp == 0 {
		return 1
	}
	if base == 0 {
		return 0
	}
	e := (int(gfLog[base]) * exp) % gfMax
	return gfExp[e]
}

// gfInv returns the multiplicative inverse of a. a must be non-zero.
func gfInv(a uint16) uint16 {
	return gfExp[(gfMax-int(gfLog[a]))%gfMax]
}

// wordsLE reinterprets a byte slice as little-endian uint16 words. The length
// must be even; PAR2 slice sizes are always a multiple of 4, so this holds for
// every slice.
func wordsLE(b []byte) []uint16 {
	w := make([]uint16, len(b)/2)
	for i := range w {
		w[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return w
}

// putWordsLE writes uint16 words back into a byte slice as little-endian.
func putWordsLE(dst []byte, w []uint16) {
	for i, v := range w {
		dst[2*i] = byte(v)
		dst[2*i+1] = byte(v >> 8)
	}
}

// xorInto computes dst ^= src over the overlapping prefix.
func xorInto(dst, src []byte) {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		dst[i] ^= src[i]
	}
}

// mulSliceInto computes dst ^= c * src, treating both slices as little-endian
// GF(2^16) words. dst and src must be the same (even) length. This is the inner
// loop of Reed-Solomon encode/recover.
func mulSliceInto(dst, src []byte, c uint16) {
	switch c {
	case 0:
		return
	case 1:
		xorInto(dst, src)
		return
	}
	lc := int(gfLog[c])
	for i := 0; i+1 < len(src) && i+1 < len(dst); i += 2 {
		v := uint16(src[i]) | uint16(src[i+1])<<8
		if v != 0 {
			p := gfExp[(int(gfLog[v])+lc)%gfMax]
			dst[i] ^= byte(p)
			dst[i+1] ^= byte(p >> 8)
		}
	}
}
