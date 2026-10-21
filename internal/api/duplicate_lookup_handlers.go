package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/kipsilabs/altmount/internal/duplicatelookup"
	"github.com/kipsilabs/altmount/internal/utils"
)

type duplicateLookupService interface {
	Lookup(context.Context, duplicatelookup.Request) ([]byte, error)
}

// handleDuplicateLookup implements the generic duplicate lookup protocol.
//
// @Summary Find a reusable NZB by original filename, size and sampled content hash
// @Description Uses the sample-sha256-v1 protocol. Returns raw NZB XML on a match, or 204 on a miss. Requires X-API-Key even when interactive login is disabled.
// @Tags Files
// @Accept json
// @Produce application/x-nzb
// @Param X-API-Key header string true "API key"
// @Param request body duplicatelookup.Request true "File fingerprint"
// @Success 200 {file} binary
// @Success 204
// @Failure 400 {object} APIResponse
// @Failure 401 {object} APIResponse
// @Failure 503 {object} APIResponse
// @Router /files/duplicate-lookup [post]
func (s *Server) handleDuplicateLookup(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	if !s.validateAPIKey(c, c.Get("X-API-Key")) {
		return RespondUnauthorized(c, "A valid X-API-Key is required", "")
	}
	mediaType, _, err := mime.ParseMediaType(c.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(c.Body()) > 8192 {
		return RespondBadRequest(c, "Expected a JSON fingerprint of at most 8192 bytes", "")
	}
	var req duplicatelookup.Request
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return RespondBadRequest(c, "Invalid fingerprint JSON", "")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return RespondBadRequest(c, "Expected one JSON object", "")
	}
	if err := duplicatelookup.Validate(req); err != nil {
		return RespondBadRequest(c, "Invalid file fingerprint", err.Error())
	}
	s.duplicateLookupOnce.Do(func() {
		if s.duplicateLookup == nil && s.metadataService != nil && s.healthRepo != nil && s.nzbFilesystem != nil {
			s.duplicateLookup = duplicatelookup.NewCatalog(s.metadataService, s.healthRepo, s.nzbFilesystem.Open)
		}
	})
	if s.duplicateLookup == nil {
		return RespondServiceUnavailable(c, "Duplicate lookup is unavailable", "")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)
	data, err := s.duplicateLookup.Lookup(ctx, req)
	if err != nil {
		slog.WarnContext(ctx, "Duplicate lookup failed", "error", err)
		return RespondServiceUnavailable(c, "Duplicate lookup could not be completed; retry later", "")
	}
	if len(data) == 0 {
		return c.SendStatus(fiber.StatusNoContent)
	}
	c.Set("Content-Type", "application/x-nzb")
	c.Set("Content-Disposition", `attachment; filename="duplicate.nzb"`)
	c.Set("X-Content-Type-Options", "nosniff")
	return c.Send(data)
}
