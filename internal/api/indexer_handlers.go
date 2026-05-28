package api

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"log/slog"
)

// handleGetIndexerStats handles GET /api/system/indexer-stats
func (s *Server) handleGetIndexerStats(c *fiber.Ctx) error {
	stats, err := s.queueRepo.GetIndexerHealthStats(c.Context())
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to fetch indexer health stats", "error", err)
		return RespondInternalError(c, "Failed to fetch indexer health stats", err.Error())
	}

	return RespondSuccess(c, stats)
}

// handleCleanupIndexerStats handles DELETE /api/system/indexer-stats/cleanup
func (s *Server) handleCleanupIndexerStats(c *fiber.Ctx) error {
	indexer := c.Query("indexer")
	if indexer != "" {
		affectedRows, err := s.queueRepo.DeleteIndexerStats(c.Context(), indexer)
		if err != nil {
			slog.ErrorContext(c.Context(), "Failed to delete indexer stats", "indexer", indexer, "error", err)
			return RespondInternalError(c, "Failed to delete indexer stats", err.Error())
		}
		slog.InfoContext(c.Context(), "Deleted indexer stats successfully", "indexer", indexer, "pruned_rows", affectedRows)
		return RespondSuccess(c, fiber.Map{"pruned_rows": affectedRows})
	}

	hoursStr := c.Query("hours")
	daysStr := c.Query("days")

	var hours int
	var err error

	if daysStr != "" {
		days, err := strconv.Atoi(daysStr)
		if err != nil || days < 0 {
			return RespondBadRequest(c, "Invalid days parameter", "")
		}
		hours = days * 24
	} else if hoursStr != "" {
		hours, err = strconv.Atoi(hoursStr)
		if err != nil || hours < 0 {
			return RespondBadRequest(c, "Invalid hours parameter", "")
		}
	} else {
		return RespondBadRequest(c, "Either indexer, hours, or days query parameter must be provided", "")
	}

	affectedRows, err := s.queueRepo.PruneIndexerStats(c.Context(), hours)
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to prune indexer stats", "hours", hours, "error", err)
		return RespondInternalError(c, "Failed to prune indexer stats", err.Error())
	}

	slog.InfoContext(c.Context(), "Pruned indexer stats successfully", "hours", hours, "pruned_rows", affectedRows)
	return RespondSuccess(c, fiber.Map{"pruned_rows": affectedRows, "hours": hours})
}
