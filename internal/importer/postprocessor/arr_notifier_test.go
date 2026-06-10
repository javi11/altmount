package postprocessor

import "testing"

func TestInferARRTypeFromCategory(t *testing.T) {
	cases := []struct {
		name     string
		category string
		want     string
	}{
		// Radarr — exact, prefixed, and "movie" contains.
		{"radarr exact", "radarr", "radarr"},
		{"radarr-sqp1", "radarr-sqp1", "radarr"},
		{"radarr-sqp3", "radarr-sqp3", "radarr"},
		{"radarr-anime", "radarr-anime", "radarr"},
		{"radarr-anime-v3", "radarr-anime-v3", "radarr"},
		{"radarr uppercase", "RADARR-SQP1", "radarr"},
		{"movies", "movies", "radarr"},
		{"movie4k", "movie4k", "radarr"},

		// Sonarr — exact, prefixed, and "tv"/"show" contains.
		{"sonarr exact", "sonarr", "sonarr"},
		{"sonarr-4k", "sonarr-4k", "sonarr"},
		{"sonarr-anime", "sonarr-anime", "sonarr"},
		{"tv exact", "tv", "sonarr"},
		{"tv-shows", "tv-shows", "sonarr"},
		{"shows", "shows", "sonarr"},

		// Lidarr.
		{"lidarr exact", "lidarr", "lidarr"},
		{"lidarr-flac", "lidarr-flac", "lidarr"},
		{"music", "music", "lidarr"},

		// Readarr.
		{"readarr exact", "readarr", "readarr"},
		{"readarr-audiobooks", "readarr-audiobooks", "readarr"},
		{"books", "books", "readarr"},

		// Whisparr.
		{"whisparr exact", "whisparr", "whisparr"},
		{"whisparr-extra", "whisparr-extra", "whisparr"},
		{"adult", "adult", "whisparr"},

		// Unknown.
		{"empty", "", ""},
		{"junk", "asdf-foo", ""},
		{"default", "default", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferARRTypeFromCategory(tc.category)
			if got != tc.want {
				t.Errorf("inferARRTypeFromCategory(%q) = %q, want %q", tc.category, got, tc.want)
			}
		})
	}
}
