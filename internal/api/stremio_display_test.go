package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/prowlarr"
)

func TestRenderStreamTemplate(t *testing.T) {
	fields := map[string]string{"title": "Knight and Day", "year": "2010", "quality": "FHD", "flag": ""}
	tests := []struct {
		name, tmpl, want string
	}{
		{"tokens", "{title} {year}", "Knight and Day 2010"},
		{"group kept when complete", "{title}< ({year})>", "Knight and Day (2010)"},
		{"group dropped when a token is empty", "{title}< {flag}>< [{quality}]>", "Knight and Day [FHD]"},
		{"escaped and real newlines", "{title}\\n{quality}\n{year}", "Knight and Day\nFHD\n2010"},
		{"blank lines and repeated spaces removed", "{title}\\n<{flag}>\\n  {quality}   {year}", "Knight and Day\nFHD 2010"},
		{"written blank line kept as spacing", "{title}\n\n{quality}", "Knight and Day\n\nFHD"},
		{"escaped blank line kept, runs collapse to one", "{title}\n\\n\n{quality}", "Knight and Day\n\nFHD"},
		{"blank line next to a dropped line", "{title}\n<{flag}>\n\n{quality}", "Knight and Day\n\nFHD"},
		{"leading and trailing blank lines trimmed", "\n{title}\n\n", "Knight and Day"},
		{"unknown token left as written", "{title} {tilte}", "Knight and Day {tilte}"},
		{"known token without a value is empty", "{title} {codec}", "Knight and Day"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderStreamTemplate(tt.tmpl, fields))
		})
	}
}

// The default preset is the label format AltMount has always used.
func TestFormatStremioStreamDefaultPreset(t *testing.T) {
	r := prowlarr.NZBResult{Title: "Knight.and.Day.2010.GERMAN.1080p.BluRay.x264-GRP", Size: 6_680_000_000, Indexer: "Idx"}
	d := resolveStreamDisplay(config.StremioConfig{})

	name, description := formatStremioStream(d, r, true)
	assert.Equal(t, "⚡ Cached · AltMount 🇩🇪 FHD - Knight and Day (2010) [1080p][Deu]", name)
	assert.Equal(t, "Knight.and.Day.2010.GERMAN.1080p.BluRay.x264-GRP\n💾 6.68 GB 🌐 Idx", description)

	off := false
	d = resolveStreamDisplay(config.StremioConfig{ShowCachedIndicator: &off})
	name, _ = formatStremioStream(d, r, true)
	assert.Equal(t, "AltMount 🇩🇪 FHD - Knight and Day (2010) [1080p][Deu]", name)
}

func TestResolveStreamDisplay(t *testing.T) {
	minimal := resolveStreamDisplay(config.StremioConfig{Display: config.StremioDisplayConfig{Preset: "minimal"}})
	assert.Equal(t, streamDisplayPresets[2].NameTemplate, minimal.NameTemplate)

	custom := resolveStreamDisplay(config.StremioConfig{Display: config.StremioDisplayConfig{
		Preset: "custom", NameTemplate: "{quality}",
	}})
	assert.Equal(t, "{quality}", custom.NameTemplate)
	assert.Equal(t, streamDisplayPresets[0].DescriptionTemplate, custom.DescriptionTemplate, "an empty custom field falls back to the default")

	unknown := resolveStreamDisplay(config.StremioConfig{Display: config.StremioDisplayConfig{Preset: "nope"}})
	assert.Equal(t, streamDisplayPresets[0].NameTemplate, unknown.NameTemplate)
}

func TestStremioDisplayPreview(t *testing.T) {
	app := fiber.New()
	server := &Server{}
	app.Post("/api/stremio/display/preview", server.handleStremioDisplayPreview)

	raw, _ := json.Marshal(StreamDisplayPreviewRequest{Display: config.StremioDisplayConfig{
		Preset: "custom", NameTemplate: "{quality}< {flag}>", DescriptionTemplate: "{title}",
	}})
	req := httptest.NewRequest("POST", "/api/stremio/display/preview", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var out struct {
		Data []StreamDisplayPreview `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Data, len(streamDisplaySamples)+1)
	assert.True(t, out.Data[0].Library, "the first preview is a library file")
	assert.Equal(t, "FHD", out.Data[0].Name, "custom formats apply to library files too")
	assert.Equal(t, "FHD", out.Data[1].Name)
	assert.Equal(t, "Knight and Day", out.Data[1].Description)
	assert.Equal(t, "HD 🇩🇪", out.Data[3].Name)
}

// Files already in the library follow the chosen format too. The default
// format keeps their own "Altmount Library" label.
func TestFormatLibraryStreamFollowsDisplay(t *testing.T) {
	lib := "/media/Movies/Knight and Day (2010)/Knight and Day (2010) - [WEBDL-1080p]-GRP.mkv"
	h := &database.FileHealth{
		FilePath:    "complete/Movies/Knight.and.Day.2010.1080p.WEB-DL.H.264-GRP/Knight.and.Day.2010.1080p.WEB-DL.H.264-GRP.mkv",
		LibraryPath: &lib,
	}

	name, description := formatLibraryStream(resolveStreamDisplay(config.StremioConfig{}), h, 6_680_000_000)
	assert.Equal(t, formatLibraryStreamName(h), name)
	assert.Equal(t, formatLibraryStreamTitle(h), description)

	custom := resolveStreamDisplay(config.StremioConfig{Display: config.StremioDisplayConfig{
		Preset:              "custom",
		NameTemplate:        "<{cached} >{quality}",
		DescriptionTemplate: "{title}< ({year})>\\n<💾 {size} >🌐 {indexer}",
	}})
	name, description = formatLibraryStream(custom, h, 6_680_000_000)
	assert.Equal(t, "⚡ Library FHD", name)
	assert.Equal(t, "Knight and Day (2010)\n💾 6.68 GB 🌐 Local Library", description)

	minimal := resolveStreamDisplay(config.StremioConfig{Display: config.StremioDisplayConfig{Preset: "minimal"}})
	name, _ = formatLibraryStream(minimal, h, 0)
	assert.Equal(t, "AltMount 1080p", name)
}

func TestFormatStreamSize(t *testing.T) {
	assert.Equal(t, "6.68 GB", formatStreamSize(6_680_000_000, 2))
	assert.Equal(t, "6.7 GB", formatStreamSize(6_680_000_000, 1))
	assert.Equal(t, "7 GB", formatStreamSize(6_680_000_000, 0))
	assert.Equal(t, "0.4 GB", formatStreamSize(400_000_000, 0), "under 1 GB keeps a decimal")
}

// {size}, {size1} and {size0} show a size with 2, 1 and 0 decimals.
func TestSizeTokens(t *testing.T) {
	r := prowlarr.NZBResult{Title: "Knight.and.Day.2010.1080p.WEB-DL.H.264-GRP", Size: 6_680_000_000, Indexer: "Idx"}
	d := resolveStreamDisplay(config.StremioConfig{Display: config.StremioDisplayConfig{
		Preset: "custom", NameTemplate: "{size} | {size1} | {size0}",
	}})
	name, _ := formatStremioStream(d, r, false)
	assert.Equal(t, "6.68 GB | 6.7 GB | 7 GB", name)
}
