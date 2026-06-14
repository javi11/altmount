package filesystem

import "testing"

func TestVolumeIndexResolveWidthMismatch(t *testing.T) {
	// A real set numbered "part0" + unpadded number: part01..part09 (2 digits) then
	// part010..part012 (3 digits). rardecode, having locked the width to 2 from the
	// first volume, requests "…part10.rar"/"…part11.rar" etc. — which must resolve
	// to the actual 3-digit files.
	base := "Movie.2024.BluRay.REMUX"
	names := []string{
		base + ".part01.rar", base + ".part02.rar", base + ".part03.rar",
		base + ".part04.rar", base + ".part05.rar", base + ".part06.rar",
		base + ".part07.rar", base + ".part08.rar", base + ".part09.rar",
		base + ".part010.rar", base + ".part011.rar", base + ".part012.rar",
	}
	vi := newVolumeIndex(names)

	tests := []struct {
		name      string
		requested string
		want      string
		wantOK    bool
	}{
		{"requested 2-digit resolves to 3-digit", base + ".part10.rar", base + ".part010.rar", true},
		{"requested 2-digit 11 resolves", base + ".part11.rar", base + ".part011.rar", true},
		{"requested 2-digit 12 resolves", base + ".part12.rar", base + ".part012.rar", true},
		{"exact-width low volume still resolves", base + ".part05.rar", base + ".part05.rar", true},
		{"absent volume does not resolve", base + ".part20.rar", "", false},
		{"non-volume name does not resolve", base + ".nfo", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := vi.resolve(tt.requested)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("resolve(%q) = (%q, %t); want (%q, %t)", tt.requested, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestVolumeIndexNoCrossSetCollision(t *testing.T) {
	// Two distinct sets in the same NZB sharing scheme+number must not collide.
	a := "MovieA.part010.rar"
	b := "MovieB.part10.rar"
	vi := newVolumeIndex([]string{a, b})

	// Requesting MovieA at width 2 resolves to MovieA's file, not MovieB's.
	if got, ok := vi.resolve("MovieA.part10.rar"); !ok || got != a {
		t.Errorf("resolve(MovieA.part10.rar) = (%q, %t); want (%q, true)", got, ok, a)
	}
	if got, ok := vi.resolve("MovieB.part010.rar"); !ok || got != b {
		t.Errorf("resolve(MovieB.part010.rar) = (%q, %t); want (%q, true)", got, ok, b)
	}
}

func TestVolumeIndexOldStyleSchemes(t *testing.T) {
	// .rNN roll scheme: .rar=vol0, .r00=vol1. Width is fixed there, but verify the
	// index keys these correctly and an exact request resolves.
	names := []string{"x.rar", "x.r00", "x.r01"}
	vi := newVolumeIndex(names)
	if got, ok := vi.resolve("x.r01"); !ok || got != "x.r01" {
		t.Errorf("resolve(x.r01) = (%q, %t); want (x.r01, true)", got, ok)
	}
	// A roll-scheme name and a part-scheme name with the same ordinal must not collide.
	if _, ok := vi.resolve("x.part1.rar"); ok {
		t.Errorf("part-scheme request unexpectedly resolved against roll-scheme set")
	}
}
