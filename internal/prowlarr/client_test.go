package prowlarr

import "testing"

func TestResolutionLabel(t *testing.T) {
	cases := map[string]string{
		"2160p": "4K",
		"1080p": "FHD",
		"720p":  "HD",
		"480p":  "SD",
		"576p":  "SD",
		"360p":  "",
		"":      "",
	}
	for res, want := range cases {
		if got := resolutionLabel(res); got != want {
			t.Errorf("resolutionLabel(%q) = %q, want %q", res, got, want)
		}
	}
}

func TestInferLanguage(t *testing.T) {
	cases := map[string]string{
		"Movie.2021.1080p.SPANISH.BluRay.x264": "Spanish",
		"Some.Show.S01E01.VOSTFR.WEB-DL":       "French",
		"Film.2020.German.DL.1080p":            "German",
		"Release.MULTi.2160p.UHD":              "Multi",
		"Plain.English.Movie.2019.eng.720p":    "English",
		"Movie.2021.1080p.BluRay.x264":         "", // no language marker
	}
	for title, want := range cases {
		if got := InferLanguage(title); got != want {
			t.Errorf("InferLanguage(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestInferReleaseMeta(t *testing.T) {
	// PTN parses resolution/year/codec; resolutionLabel maps to a quality label.
	m := InferReleaseMeta("The.Movie.2021.1080p.BluRay.x264-GROUP")
	if m.Resolution != "1080p" {
		t.Errorf("Resolution = %q, want 1080p", m.Resolution)
	}
	if m.QualityLabel != "FHD" {
		t.Errorf("QualityLabel = %q, want FHD (from 1080p)", m.QualityLabel)
	}
	if m.Year != 2021 {
		t.Errorf("Year = %d, want 2021", m.Year)
	}

	// Spanish marker → language fields populated.
	s := InferReleaseMeta("La.Pelicula.2020.SPANISH.1080p.WEB-DL")
	if s.Language != "Spanish" {
		t.Fatalf("Language = %q, want Spanish", s.Language)
	}
	if s.FlagEmoji != "🇪🇸" || s.LangCode != "Esp" {
		t.Errorf("flag/code = %q/%q, want 🇪🇸/Esp", s.FlagEmoji, s.LangCode)
	}

	// No resolution → QualityLabel falls back to the parsed quality string.
	q := InferReleaseMeta("Show.S01E02.WEB-DL")
	if q.Resolution == "" && q.Quality != "" && q.QualityLabel != q.Quality {
		t.Errorf("QualityLabel = %q, want fallback to Quality %q", q.QualityLabel, q.Quality)
	}
}
