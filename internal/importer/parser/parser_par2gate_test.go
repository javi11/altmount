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
