package api

import (
	_ "embed"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

//go:embed assets/no-streams.mp4
var noStreamsVideo []byte

// noStreamsVideoTitle is the label shown in Stremio for the placeholder stream.
const noStreamsVideoTitle = "No streams available"

// handleStremioNoStreamsVideo handles GET /stremio/:key/no-streams.mp4
// Serves the placeholder video returned when no streams are available.
//
//	@Summary		Stremio no-streams placeholder video
//	@Description	Serves a short placeholder video shown by Stremio clients when no streams are available.
//	@Tags			Stremio
//	@Produce		video/mp4
//	@Param			key	path		string	true	"Download key (SHA256 of API key)"
//	@Success		200	{string}	string	"Video content"
//	@Failure		401	{object}	APIResponse
//	@Router			/stremio/{key}/no-streams.mp4 [get]
func (s *Server) handleStremioNoStreamsVideo(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration not available", "")
	}
	cfg := s.configManager.GetConfig()
	if !isStremioEnabled(cfg) {
		return RespondNotFound(c, "Stremio endpoint", "Stremio integration is disabled")
	}

	key := c.Params("key")
	if !s.validateDownloadKey(ctx, key) {
		return RespondUnauthorized(c, "Invalid key", "")
	}

	return serveNoStreamsVideo(c)
}

// serveNoStreamsVideo writes the embedded placeholder video, honouring single-part
// Range requests so players can stream the asset properly.
func serveNoStreamsVideo(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "video/mp4")
	c.Set(fiber.HeaderAcceptRanges, "bytes")
	c.Set(fiber.HeaderCacheControl, "public, max-age=86400")

	if start, end, ok := parseByteRange(string(c.Request().Header.Peek(fiber.HeaderRange)), len(noStreamsVideo)); ok {
		c.Set(fiber.HeaderContentRange, "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(noStreamsVideo)))
		return c.Status(fiber.StatusPartialContent).Send(noStreamsVideo[start : end+1])
	}

	return c.Send(noStreamsVideo)
}

// parseByteRange parses a single HTTP Range header ("bytes=start-end", "bytes=start-"
// or "bytes=-suffix"). Returns ok=false when absent, malformed, multi-part or
// unsatisfiable; callers then fall back to a full 200 response.
func parseByteRange(header string, size int) (start, end int, ok bool) {
	if size <= 0 || !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return 0, 0, false
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])

	if startStr == "" {
		n, err := strconv.Atoi(endStr)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}

	start, err := strconv.Atoi(startStr)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end = size - 1
	if endStr != "" {
		parsed, err := strconv.Atoi(endStr)
		if err != nil || parsed < start {
			return 0, 0, false
		}
		if parsed < end {
			end = parsed
		}
	}
	return start, end, true
}
