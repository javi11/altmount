package api

import (
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/stremio"
)

// TestValidateInspectScoring bounds the regex work a single inspect request can
// trigger. Every pattern in a draft config is compiled during evaluation, so a
// caller must not be able to dictate unbounded compilation, and an invalid
// pattern must be reported rather than silently never matching.
func TestValidateInspectScoring(t *testing.T) {
	manyFormats := func(n int) []stremio.TrashCustomFormat {
		out := make([]stremio.TrashCustomFormat, n)
		for i := range out {
			out[i] = stremio.TrashCustomFormat{Pattern: "x", PatternType: "token", Enabled: true}
		}
		return out
	}

	tests := []struct {
		name    string
		scoring *stremio.StreamScoringConfig
		wantErr string
	}{
		{"nil config is allowed", nil, ""},
		{"empty config is allowed", &stremio.StreamScoringConfig{}, ""},
		{
			"reasonable config is allowed",
			&stremio.StreamScoringConfig{
				CustomFormats:   manyFormats(40),
				ExcludeKeywords: []string{"CAM", "TS"},
				ExcludeRegex:    `\b(cam|ts)\b`,
			},
			"",
		},
		{
			"too many custom formats",
			&stremio.StreamScoringConfig{CustomFormats: manyFormats(maxInspectCustomFormats + 1)},
			"custom_formats has",
		},
		{
			"too many exclude keywords",
			&stremio.StreamScoringConfig{
				ExcludeKeywords: make([]string, maxInspectCustomFormats+1),
			},
			"exclude_keywords has",
		},
		{
			"oversized pattern",
			&stremio.StreamScoringConfig{
				CustomFormats: []stremio.TrashCustomFormat{
					{Pattern: strings.Repeat("a", maxInspectPatternLen+1), Enabled: true},
				},
			},
			"exceeds the",
		},
		{
			"invalid exclude regex is reported",
			&stremio.StreamScoringConfig{ExcludeRegex: "[unclosed"},
			"exclude_regex is invalid",
		},
		{
			"invalid custom format regex is reported",
			&stremio.StreamScoringConfig{
				CustomFormats: []stremio.TrashCustomFormat{
					{Pattern: "(unclosed", PatternType: "regex", Enabled: true},
				},
			},
			"custom_formats[0] pattern is invalid",
		},
		{
			"invalid pattern on a disabled format is ignored",
			&stremio.StreamScoringConfig{
				CustomFormats: []stremio.TrashCustomFormat{
					{Pattern: "(unclosed", PatternType: "regex", Enabled: false},
				},
			},
			"",
		},
		{
			"token patterns are not compiled",
			&stremio.StreamScoringConfig{
				CustomFormats: []stremio.TrashCustomFormat{
					{Pattern: "[SubsPlease]", PatternType: "token", Enabled: true},
				},
			},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInspectScoring(tt.scoring)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateInspectScoring() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateInspectScoring() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateInspectScoring() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
