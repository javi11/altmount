package archive

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadRenames(t *testing.T) {
	const release = "Some.Movie.2024.1080p.WEB-DL-GRP"

	tests := []struct {
		name     string
		release  string // defaults to release
		contents []Content
		want     map[string]string
	}{
		{
			name: "single obfuscated payload renamed to release",
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 5_000_000},
			},
			want: map[string]string{"a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv": release + ".mkv"},
		},
		{
			name: "sidecars sharing the payload stem follow it",
			contents: []Content{
				{InternalPath: "x9y8z7w6v5u4t3s2r1q0p9o8n7m6l5k4.mkv", Size: 5_000_000},
				{InternalPath: "x9y8z7w6v5u4t3s2r1q0p9o8n7m6l5k4.eng.srt", Size: 40_000},
				{InternalPath: "x9y8z7w6v5u4t3s2r1q0p9o8n7m6l5k4-sample.mkv", Size: 200_000},
				{InternalPath: "readme.nfo", Size: 1_000},
			},
			want: map[string]string{
				"x9y8z7w6v5u4t3s2r1q0p9o8n7m6l5k4.mkv":        release + ".mkv",
				"x9y8z7w6v5u4t3s2r1q0p9o8n7m6l5k4.eng.srt":    release + ".eng.srt",
				"x9y8z7w6v5u4t3s2r1q0p9o8n7m6l5k4-sample.mkv": release + "-sample.mkv",
			},
		},
		{
			name: "payload in a subdirectory keeps only the leaf renamed",
			contents: []Content{
				{InternalPath: "Inner/a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 5_000_000},
			},
			want: map[string]string{"Inner/a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv": release + ".mkv"},
		},
		{
			name: "several files of a size are a set, nothing renamed",
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 1_000_000},
				{InternalPath: "b3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 1_100_000},
				{InternalPath: "c3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 900_000},
			},
			want: nil,
		},
		{
			name: "clean payload name is left alone",
			contents: []Content{
				{InternalPath: "Some.Movie.2024.PROPER.1080p.mkv", Size: 5_000_000},
			},
			want: nil,
		},
		{
			name: "disc structure is never renamed",
			contents: []Content{
				{InternalPath: "BDMV/STREAM/00001.m2ts", Size: 5_000_000},
				{InternalPath: "BDMV/index.bdmv", Size: 100},
			},
			want: nil,
		},
		{
			name: "split container extension is never renamed",
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.vob", Size: 5_000_000},
			},
			want: nil,
		},
		{
			name:    "release already carrying the extension is not doubled",
			release: release + ".mkv",
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 5_000_000},
			},
			want: map[string]string{"a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv": release + ".mkv"},
		},
		{
			name: "rename that would collide with an existing entry is abandoned",
			contents: []Content{
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 5_000_000},
				{InternalPath: release + ".mkv", Size: 10},
			},
			want: nil,
		},
		{
			name: "directories and empty entries are ignored",
			contents: []Content{
				{InternalPath: "Extras", IsDirectory: true},
				{InternalPath: "a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv", Size: 5_000_000},
				{InternalPath: "empty.txt", Size: 0},
			},
			want: map[string]string{"a3f9c1d2e4b5a6f7c8d9e0f1a2b3c4d5.mkv": release + ".mkv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := tt.release
			if rel == "" {
				rel = release
			}
			got := PayloadRenames(tt.contents, rel)
			require.Equal(t, tt.want, got)
		})
	}
}
