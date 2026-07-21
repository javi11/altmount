package api

import "testing"

func TestParseBackboneEntries(t *testing.T) {
	// Shape mirrors the rexum /api/providers payload.
	raw := []byte(`{
		"providers": [
			{
				"name": "Newshosting",
				"server": [
					{"nntp": "news.newshosting.com", "backbone": "Omicron"},
					{"nntp": "NEWS.NEWSHOSTING.COM", "backbone": "Omicron"}
				]
			},
			{
				"name": "Eweka",
				"server": [
					{"nntp": "news.eweka.nl", "backbone": "Eweka Internet Services"}
				]
			},
			{
				"name": "MissingBackbone",
				"server": [
					{"nntp": "reader.nobackbone.com", "backbone": ""}
				]
			},
			{
				"name": "MissingHost",
				"server": [
					{"nntp": "", "backbone": "Ghost"}
				]
			}
		]
	}`)

	entries, err := parseBackboneEntries(raw)
	if err != nil {
		t.Fatalf("parseBackboneEntries() error = %v", err)
	}

	// Duplicate host (case-insensitive) collapses; entries missing host or
	// backbone are skipped. Newshosting + Eweka => 2 entries.
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}

	byHost := map[string]BackboneEntry{}
	for _, e := range entries {
		byHost[e.Host] = e
	}

	nh, ok := byHost["news.newshosting.com"]
	if !ok {
		t.Fatalf("missing newshosting entry: %+v", entries)
	}
	if nh.Backbone != "Omicron" {
		t.Errorf("newshosting backbone = %q, want %q", nh.Backbone, "Omicron")
	}
	if nh.Provider != "Newshosting" {
		t.Errorf("newshosting provider = %q, want %q", nh.Provider, "Newshosting")
	}

	if _, ok := byHost["news.eweka.nl"]; !ok {
		t.Errorf("missing eweka entry: %+v", entries)
	}
}

func TestParseBackboneEntries_InvalidJSON(t *testing.T) {
	if _, err := parseBackboneEntries([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
