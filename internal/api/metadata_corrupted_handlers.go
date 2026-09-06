package api

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
)

// CorruptedMetadataStatsResponse reports what the corrupted_metadata safety folder
// currently holds.
type CorruptedMetadataStatsResponse struct {
	FileCount  int   `json:"file_count"`
	TotalBytes int64 `json:"total_bytes"`
}

// handleGetCorruptedMetadata handles GET /api/metadata/corrupted
//
//	@Summary		Get corrupted metadata stats
//	@Description	Returns how many corrupted metadata safety copies are retained and how much disk they occupy.
//	@Tags			Metadata
//	@Produce		json
//	@Success		200	{object}	APIResponse{data=CorruptedMetadataStatsResponse}
//	@Failure		500	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/metadata/corrupted [get]
func (s *Server) handleGetCorruptedMetadata(c *fiber.Ctx) error {
	if s.metadataService == nil {
		return RespondServiceUnavailable(c, "Metadata service not available", "")
	}

	count, totalBytes, err := s.metadataService.CorruptedStats(c.Context())
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to read corrupted metadata stats", "error", err)
		return RespondInternalError(c, "Failed to read corrupted metadata stats", err.Error())
	}

	return RespondSuccess(c, CorruptedMetadataStatsResponse{FileCount: count, TotalBytes: totalBytes})
}

// handlePurgeCorruptedMetadata handles DELETE /api/metadata/corrupted
//
//	@Summary		Purge corrupted metadata
//	@Description	Removes every corrupted metadata safety copy and releases the segment stores they hold.
//	@Tags			Metadata
//	@Produce		json
//	@Success		204	"No Content"
//	@Failure		500	{object}	APIResponse
//	@Failure		503	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/metadata/corrupted [delete]
func (s *Server) handlePurgeCorruptedMetadata(c *fiber.Ctx) error {
	if s.metadataService == nil {
		return RespondServiceUnavailable(c, "Metadata service not available", "")
	}

	removed, err := s.metadataService.PruneCorrupted(c.Context(), 0)
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to purge corrupted metadata", "error", err, "removed", removed)
		return RespondInternalError(c, "Failed to purge corrupted metadata", err.Error())
	}

	return RespondNoContent(c)
}
