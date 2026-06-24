package parser

import (
	"testing"

	"github.com/javi11/nzbparser"
)

// par2Magic is the PAR2 packet signature ("PAR2\0PKT").
var par2Magic = []byte{'P', 'A', 'R', '2', 0, 'P', 'K', 'T'}

func fsd(filename string, raw []byte) *FirstSegmentData {
	return &FirstSegmentData{
		File:     &nzbparser.NzbFile{Filename: filename},
		RawBytes: raw,
	}
}

func TestNeedsPar2Matching(t *testing.T) {
	tests := []struct {
		name string
		data *FirstSegmentData
		want bool
	}{
		{
			name: "obfuscated name needs matching",
			data: fsd("b082fa0beaa644d3aa01045d5b8d0b36.mkv", []byte("data")),
			want: true,
		},
		{
			name: "empty filename needs matching",
			data: fsd("", []byte("data")),
			want: true,
		},
		{
			name: "missing extension needs matching",
			data: fsd("Great Movie 2020", []byte("data")),
			want: true,
		},
		{
			name: "clean name with valid extension does not need matching",
			data: fsd("Great.Movie.2020.1080p.mkv", []byte("data")),
			want: false,
		},
		{
			name: "clean rar volume does not need matching",
			data: fsd("Great.Movie.2020.part01.rar", []byte("data")),
			want: false,
		},
		{
			name: "par2 file by magic bytes never needs matching",
			data: fsd("b082fa0beaa644d3aa01045d5b8d0b36.bin", par2Magic),
			want: false,
		},
		{
			name: "par2 file by name never needs matching",
			data: fsd("movie.vol01+02.par2", []byte("data")),
			want: false,
		},
		{
			name: "sidecar nfo does not need matching",
			data: fsd("release.nfo", []byte("data")),
			want: false,
		},
		{
			name: "skipped file cannot be matched",
			data: func() *FirstSegmentData {
				d := fsd("b082fa0beaa644d3aa01045d5b8d0b36.mkv", nil)
				d.SkippedFirstSegment = true
				return d
			}(),
			want: false,
		},
		{
			name: "missing first segment cannot be matched",
			data: func() *FirstSegmentData {
				d := fsd("b082fa0beaa644d3aa01045d5b8d0b36.mkv", nil)
				d.MissingFirstSegment = true
				return d
			}(),
			want: false,
		},
		{
			name: "nil data",
			data: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsPar2Matching(tt.data); got != tt.want {
				t.Errorf("needsPar2Matching() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasObfuscatedVolumeSet(t *testing.T) {
	cache := func(names ...string) []*FirstSegmentData {
		c := make([]*FirstSegmentData, len(names))
		for i, n := range names {
			c[i] = fsd(n, []byte("data"))
		}
		return c
	}

	tests := []struct {
		name  string
		cache []*FirstSegmentData
		want  bool
	}{
		{
			// The real-world failure: every volume of one .partNN.rar set has a distinct
			// random base. needsPar2Matching can't see this (the names read as clean), but
			// the all-distinct-bases divergence is unambiguous.
			name: "obfuscated divergent-base part set",
			cache: cache(
				"US8yidqpbbD0tHBa-Y5l_Phs8V5qb.part01.rar",
				"BtEPCuoFuvkpQLMHo1rs_Qp4fOtj6.part02.rar",
				"bY2YttkbyosUtsy.part03.rar",
				"DasjtQyqvxamxMrKTsW-Nw6t9.part04.rar",
			),
			want: true,
		},
		{
			name: "clean single multi-volume set shares one base",
			cache: cache(
				"Movie.Name.2023.1080p.part01.rar",
				"Movie.Name.2023.1080p.part02.rar",
				"Movie.Name.2023.1080p.part03.rar",
			),
			want: false,
		},
		{
			name: "two clean part sets: few bases across many volumes",
			cache: cache(
				"MovieA.part01.rar", "MovieA.part02.rar", "MovieA.part03.rar",
				"MovieB.part01.rar", "MovieB.part02.rar", "MovieB.part03.rar",
			),
			want: false,
		},
		{
			name:  "single part volume is not a set",
			cache: cache("US8yidqpbbD0tHBa.part01.rar"),
			want:  false,
		},
		{
			name:  "non-volume files ignored",
			cache: cache("b082fa0beaa644d3aa01045d5b8d0b36.mkv", "release.nfo"),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasObfuscatedVolumeSet(tt.cache); got != tt.want {
				t.Errorf("hasObfuscatedVolumeSet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnyFileNeedsPar2Matching(t *testing.T) {
	clean := fsd("Great.Movie.2020.mkv", []byte("data"))
	obfuscated := fsd("b082fa0beaa644d3aa01045d5b8d0b36.mkv", []byte("data"))

	if anyFileNeedsPar2Matching([]*FirstSegmentData{clean}) {
		t.Error("all-clean NZB must not need PAR2 matching")
	}
	if !anyFileNeedsPar2Matching([]*FirstSegmentData{clean, obfuscated}) {
		t.Error("NZB with an obfuscated file must need PAR2 matching")
	}
	if anyFileNeedsPar2Matching(nil) {
		t.Error("empty cache must not need PAR2 matching")
	}
}
