package api

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/prowlarr"
)

// streamDisplayPreset is a named pair of templates for a stream's name (the
// left-hand label in Stremio) and its description.
type streamDisplayPreset struct {
	ID                  string `json:"id"`
	Label               string `json:"label"`
	NameTemplate        string `json:"name_template"`
	DescriptionTemplate string `json:"description_template"`
}

// streamDisplayPresets are the built-in label formats. "default" is the
// format AltMount has always used.
var streamDisplayPresets = []streamDisplayPreset{
	{
		ID:                  "default",
		Label:               "Default",
		NameTemplate:        "<{cached} · >{addon}< {flag}>< {quality}> - {title}< ({year})>< [{resolution}]><[{lang}]>",
		DescriptionTemplate: "{release}\n<💾 {size} >🌐 {indexer}",
	},
	{
		ID:                  "full",
		Label:               "Full Detail",
		NameTemplate:        "{addon}< {resolution}><\n{cached}>",
		DescriptionTemplate: "{title}< ({year})>\n<{source} ><{codec} ><{audio}>\n<{flag} {language}\n>{release}\n<💾 {size} >🌐 {indexer}",
	},
	{
		ID:                  "minimal",
		Label:               "Minimal Detail",
		NameTemplate:        "{addon}< {resolution}>",
		DescriptionTemplate: "<{cached} · >{title}< ({year})>\n<💾 {size}>",
	},
}

// streamDisplayTokens lists the template tokens, for the config page.
var streamDisplayTokens = []string{
	"addon", "cached", "title", "year", "release", "quality", "resolution",
	"source", "codec", "audio", "flag", "lang", "language", "size", "size1",
	"size0", "indexer",
}

// streamDisplay is the resolved display configuration for one request.
type streamDisplay struct {
	// Preset is the resolved preset id ("default" when unset or unknown).
	Preset              string
	NameTemplate        string
	DescriptionTemplate string
	ShowCached          bool
}

// resolveStreamDisplay picks the templates for the configured preset. A
// custom preset falls back to the default template for a field left empty.
func resolveStreamDisplay(cfg config.StremioConfig) streamDisplay {
	d := streamDisplay{ShowCached: cfg.ShowCachedIndicator == nil || *cfg.ShowCachedIndicator}
	preset := streamDisplayPresets[0]
	for _, p := range streamDisplayPresets {
		if p.ID == cfg.Display.Preset {
			preset = p
		}
	}
	d.Preset = preset.ID
	d.NameTemplate, d.DescriptionTemplate = preset.NameTemplate, preset.DescriptionTemplate
	if cfg.Display.Preset == "custom" {
		d.Preset = "custom"
		if strings.TrimSpace(cfg.Display.NameTemplate) != "" {
			d.NameTemplate = cfg.Display.NameTemplate
		}
		if strings.TrimSpace(cfg.Display.DescriptionTemplate) != "" {
			d.DescriptionTemplate = cfg.Display.DescriptionTemplate
		}
	}
	return d
}

// streamLabelFields are the token values for one release.
func streamLabelFields(r prowlarr.NZBResult, cached bool, d streamDisplay) map[string]string {
	meta := prowlarr.InferReleaseMeta(r.Title)
	fields := map[string]string{
		"addon":      "AltMount",
		"title":      meta.ParsedTitle,
		"release":    r.Title,
		"quality":    meta.QualityLabel,
		"resolution": meta.Resolution,
		"source":     meta.Quality,
		"codec":      meta.Codec,
		"audio":      meta.Audio,
		"flag":       meta.FlagEmoji,
		"lang":       meta.LangCode,
		"language":   meta.Language,
		"indexer":    r.Indexer,
	}
	if fields["title"] == "" {
		fields["title"] = r.Title
	}
	if meta.Year > 0 {
		fields["year"] = fmt.Sprintf("%d", meta.Year)
	}
	if r.Size > 0 {
		fields["size"] = formatStreamSize(r.Size, 2)
		fields["size1"] = formatStreamSize(r.Size, 1)
		fields["size0"] = formatStreamSize(r.Size, 0)
	}
	if fields["indexer"] == "" {
		fields["indexer"] = "Unknown"
	}
	if cached && d.ShowCached {
		fields["cached"] = "⚡ Cached"
	}
	return fields
}

// formatStreamSize shows a size in GB with the given decimals. Whole-GB
// rounding keeps one decimal below 1 GB, so a small file never reads "0 GB".
func formatStreamSize(bytes int64, decimals int) string {
	gb := float64(bytes) / 1e9
	if decimals == 0 && gb < 1 {
		decimals = 1
	}
	return fmt.Sprintf("%.*f GB", decimals, gb)
}

var (
	streamTemplateToken = regexp.MustCompile(`\{([a-z0-9]+)\}`)
	streamTemplateGroup = regexp.MustCompile(`<([^<>]*)>`)
	streamTemplateSpace = regexp.MustCompile(`[ \t]{2,}`)
)

// renderStreamTemplate fills a label template. {token} is replaced by its
// value; a <...> segment is dropped when any token inside it is empty; "\n"
// (or a real newline) starts a new line. Unknown tokens are left as written,
// so a typo shows up in Stremio. Lines left empty by missing values and
// repeated spaces are removed; a blank line written in the template is kept
// as spacing (one per run).
func renderStreamTemplate(tmpl string, fields map[string]string) string {
	expand := func(s string) (string, bool) {
		complete := true
		out := streamTemplateToken.ReplaceAllStringFunc(s, func(tok string) string {
			name := tok[1 : len(tok)-1]
			v, known := fields[name]
			if !known {
				if !isStreamDisplayToken(name) {
					return tok
				}
				v = ""
			}
			if v == "" {
				complete = false
			}
			return v
		})
		return out, complete
	}

	tmpl = strings.ReplaceAll(tmpl, `\n`, "\n")
	// Mark the blank lines the template itself contains: they are spacing the
	// user asked for, unlike lines left empty by missing values.
	written := strings.Split(tmpl, "\n")
	for i, line := range written {
		if strings.TrimSpace(line) == "" {
			written[i] = streamTemplateBlank
		}
	}
	tmpl = strings.Join(written, "\n")
	tmpl = streamTemplateGroup.ReplaceAllStringFunc(tmpl, func(group string) string {
		out, complete := expand(group[1 : len(group)-1])
		if !complete {
			return ""
		}
		return out
	})
	tmpl, _ = expand(tmpl)

	var kept []string
	for _, line := range strings.Split(tmpl, "\n") {
		if line == streamTemplateBlank {
			// One blank line for a run of them, and none before the first
			// line of text.
			if len(kept) > 0 && kept[len(kept)-1] != "" {
				kept = append(kept, "")
			}
			continue
		}
		line = strings.TrimSpace(streamTemplateSpace.ReplaceAllString(line, " "))
		if line != "" {
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n")
}

// streamTemplateBlank stands in for a blank line written in a template while
// it is rendered.
const streamTemplateBlank = "\x00"

func isStreamDisplayToken(name string) bool {
	for _, t := range streamDisplayTokens {
		if t == name {
			return true
		}
	}
	return false
}

// formatStremioStream renders a release's Stremio name and description.
func formatStremioStream(d streamDisplay, r prowlarr.NZBResult, cached bool) (name, description string) {
	fields := streamLabelFields(r, cached, d)
	return renderStreamTemplate(d.NameTemplate, fields), renderStreamTemplate(d.DescriptionTemplate, fields)
}

// formatLibraryStream renders the Stremio name and description of a file
// already in the library. The default preset keeps the library's own label;
// any other format is applied as for search results, with the values taken
// from the file's original release name, {indexer} "Local Library" and
// {cached} "⚡ Library".
func formatLibraryStream(d streamDisplay, h *database.FileHealth, size int64) (name, description string) {
	if d.Preset == "default" {
		return formatLibraryStreamName(h), formatLibraryStreamTitle(h)
	}
	release := filepath.Base(h.FilePath)
	release = strings.TrimSuffix(release, filepath.Ext(release))
	fields := streamLabelFields(prowlarr.NZBResult{Title: release, Size: size, Indexer: "Local Library"}, false, d)
	if d.ShowCached {
		fields["cached"] = "⚡ Library"
	}
	return renderStreamTemplate(d.NameTemplate, fields), renderStreamTemplate(d.DescriptionTemplate, fields)
}

// libraryStreamLabels renders a library file's labels, reading its size from
// metadata.
func (s *Server) libraryStreamLabels(d streamDisplay, h *database.FileHealth) (name, description string) {
	var size int64
	if d.Preset != "default" && s.metadataService != nil {
		if lite, err := s.metadataService.ReadFileMetadataLite(h.FilePath); err == nil && lite != nil {
			size = lite.FileSize
		}
	}
	return formatLibraryStream(d, h, size)
}

// streamDisplayLibrarySample is the library file the config page previews.
var streamDisplayLibrarySample = func() *database.FileHealth {
	lib := "/media/Movies/Knight and Day (2010)/Knight and Day (2010) - [WEBDL-1080p][EAC3 5.1][h264]-GRP.mkv"
	return &database.FileHealth{
		FilePath:    "complete/Movies/Knight.and.Day.2010.1080p.AMZN.WEB-DL.DDP5.1.H.264-GRP/Knight.and.Day.2010.1080p.AMZN.WEB-DL.DDP5.1.H.264-GRP.mkv",
		LibraryPath: &lib,
	}
}()

// streamDisplaySamples are the releases the config page previews labels with.
var streamDisplaySamples = []struct {
	result prowlarr.NZBResult
	cached bool
}{
	{prowlarr.NZBResult{Title: "Knight.and.Day.2010.1080p.DSNP.WEB-DL.DDP5.1.H.264-GRP", Size: 6_680_000_000, Indexer: "Indexer"}, true},
	{prowlarr.NZBResult{Title: "Dune.Part.Two.2024.2160p.UHD.BluRay.x265.10bit.HDR.TrueHD.7.1.Atmos-GRP", Size: 31_200_000_000, Indexer: "Indexer"}, false},
	{prowlarr.NZBResult{Title: "Das.Boot.1981.GERMAN.720p.BluRay.x264-GRP", Size: 4_370_000_000, Indexer: "Indexer"}, false},
}

// StreamDisplayPreviewRequest is the body of POST /api/stremio/display/preview.
type StreamDisplayPreviewRequest struct {
	Display             config.StremioDisplayConfig `json:"display"`
	ShowCachedIndicator *bool                       `json:"show_cached_indicator,omitempty"`
}

// StreamDisplayPreview is one sample release rendered with a display config.
type StreamDisplayPreview struct {
	Release     string `json:"release"`
	Cached      bool   `json:"cached"`
	Library     bool   `json:"library"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleGetStremioDisplayPresets handles GET /api/stremio/display/presets.
func (s *Server) handleGetStremioDisplayPresets(c *fiber.Ctx) error {
	return RespondSuccess(c, fiber.Map{"presets": streamDisplayPresets, "tokens": streamDisplayTokens})
}

// handleStremioDisplayPreview handles POST /api/stremio/display/preview:
// renders sample releases with a (possibly unsaved) display config, using the
// same code that labels streams for Stremio.
func (s *Server) handleStremioDisplayPreview(c *fiber.Ctx) error {
	var req StreamDisplayPreviewRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}
	d := resolveStreamDisplay(config.StremioConfig{Display: req.Display, ShowCachedIndicator: req.ShowCachedIndicator})
	previews := make([]StreamDisplayPreview, 0, len(streamDisplaySamples)+1)
	libName, libDescription := formatLibraryStream(d, streamDisplayLibrarySample, 9_190_000_000)
	previews = append(previews, StreamDisplayPreview{
		Release: filepath.Base(streamDisplayLibrarySample.FilePath), Library: true, Name: libName, Description: libDescription,
	})
	for _, sample := range streamDisplaySamples {
		name, description := formatStremioStream(d, sample.result, sample.cached)
		previews = append(previews, StreamDisplayPreview{
			Release: sample.result.Title, Cached: sample.cached, Name: name, Description: description,
		})
	}
	return RespondSuccess(c, previews)
}
