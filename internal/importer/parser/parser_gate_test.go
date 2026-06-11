package parser

import (
	"testing"

	"github.com/javi11/nzbparser"
)

// seg builds an NzbSegment with the given encoded byte count.
func seg(bytes, number int) nzbparser.NzbSegment {
	return nzbparser.NzbSegment{Bytes: bytes, Number: number, ID: "id"}
}

// uniformFile builds a file with n segments: n-1 full parts of `part` bytes and a
// smaller last part, with the given filename.
func uniformFile(filename string, n, part, last int) *nzbparser.NzbFile {
	segs := make(nzbparser.NzbSegments, 0, n)
	for i := 0; i < n-1; i++ {
		segs = append(segs, seg(part, i+1))
	}
	segs = append(segs, seg(last, n))
	return &nzbparser.NzbFile{Filename: filename, Segments: segs}
}

func TestShouldSkipFirstSegmentFetch(t *testing.T) {
	tests := []struct {
		name string
		file *nzbparser.NzbFile
		want bool
	}{
		{
			name: "clean multipart mkv is skipped",
			file: uniformFile("Great.Movie.2020.1080p.mkv", 5, 700000, 120000),
			want: true,
		},
		{
			name: "obfuscated md5 name is fetched",
			file: uniformFile("b082fa0beaa644d3aa01045d5b8d0b36.mkv", 5, 700000, 120000),
			want: false,
		},
		{
			name: "missing extension is fetched",
			file: uniformFile("Great Movie 2020", 5, 700000, 120000),
			want: false,
		},
		{
			name: "rar part is fetched (archive analysis / magic bytes)",
			file: uniformFile("Great.Movie.part01.rar", 5, 700000, 120000),
			want: false,
		},
		{
			name: "par2 file is fetched (descriptor content)",
			file: uniformFile("Great.Movie.vol01+02.par2", 5, 700000, 120000),
			want: false,
		},
		{
			name: "single segment file is fetched (no representative)",
			file: uniformFile("Great.Movie.mkv", 1, 700000, 700000),
			want: false,
		},
		{
			name: "two segment file is fetched (needs >=3 segments)",
			file: uniformFile("Great.Movie.mkv", 2, 700000, 120000),
			want: false,
		},
		{
			name: "non-uniform first segment is fetched",
			file: func() *nzbparser.NzbFile {
				f := uniformFile("Great.Movie.2020.mkv", 5, 700000, 120000)
				f.Segments[0].Bytes = 300000 // first part much smaller than middles
				return f
			}(),
			want: false,
		},
		{
			name: "ambiguous numeric split volume is fetched (needs magic bytes)",
			file: uniformFile("Movie.2020.001", 5, 700000, 120000),
			want: false,
		},
		{
			name: "multipart mkv split volume is fetched (needs magic bytes)",
			file: uniformFile("Movie.2020.mkv.001", 5, 700000, 120000),
			want: false,
		},
		{
			name: "ambiguous .bin extension is fetched",
			file: uniformFile("Movie.2020.bin", 5, 700000, 120000),
			want: false,
		},
		{
			name: "iso is fetched (disc image needs inspection)",
			file: uniformFile("Movie.2020.iso", 5, 700000, 120000),
			want: false,
		},
		{
			name: "nil file is not skipped",
			file: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipFirstSegmentFetch(tt.file); got != tt.want {
				t.Errorf("shouldSkipFirstSegmentFetch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstSegmentEncodedSizeUniform(t *testing.T) {
	tests := []struct {
		name     string
		segments nzbparser.NzbSegments
		want     bool
	}{
		{
			name:     "uniform parts within tolerance",
			segments: uniformFile("x.mkv", 5, 700000, 120000).Segments,
			want:     true,
		},
		{
			name:     "first segment differs beyond tolerance",
			segments: nzbparser.NzbSegments{seg(300000, 1), seg(700000, 2), seg(700000, 3), seg(120000, 4)},
			want:     false,
		},
		{
			name:     "fewer than 3 segments",
			segments: nzbparser.NzbSegments{seg(700000, 1), seg(120000, 2)},
			want:     false,
		},
		{
			name:     "first within 1 percent is uniform",
			segments: nzbparser.NzbSegments{seg(704000, 1), seg(700000, 2), seg(700000, 3), seg(120000, 4)},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstSegmentEncodedSizeUniform(tt.segments); got != tt.want {
				t.Errorf("firstSegmentEncodedSizeUniform() = %v, want %v", got, tt.want)
			}
		})
	}
}
