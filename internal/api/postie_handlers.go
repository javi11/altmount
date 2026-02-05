package api

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
)

// handlePostieRetry handles POST /api/queue/:id/postie-retry
// Resets a failed Postie upload back to "pending" status for retry
func (s *Server) handlePostieRetry(c *fiber.Ctx) error {
	id := c.Params("id")
	itemID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "")
	}

	// Get the queue item
	item, err := s.queueRepo.GetQueueItem(c.Context(), itemID)
	if err != nil {
		return RespondInternalError(c, "Failed to get queue item", err.Error())
	}
	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}

	// Check if item is in a state that allows Postie retry
	if item.PostieUploadStatus == nil || *item.PostieUploadStatus != "postie_failed" {
		return RespondBadRequest(c, "Item is not in a failed Postie state", "Current status: "+getStatusString(item.PostieUploadStatus))
	}

	// Reset to "pending" status
	pendingStatus := "pending"
	if err := s.queueRepo.UpdatePostieTracking(
		c.Context(),
		item.ID,
		item.PostieUploadID,
		&pendingStatus,
		nil, // Don't change uploaded_at
		item.OriginalReleaseName,
	); err != nil {
		return RespondInternalError(c, "Failed to reset item", err.Error())
	}

	return RespondSuccess(c, item)
}

// getStatusString returns a string representation of the Postie status
func getStatusString(status *string) string {
	if status == nil {
		return "not set"
	}
	return *status
}

// handleGetPendingPostieItems handles GET /api/queue/postie/pending
// Returns all queue items that are waiting for Postie upload
func (s *Server) handleGetPendingPostieItems(c *fiber.Ctx) error {
	items, err := s.queueRepo.GetItemsByPostieStatus(c.Context(), "pending")
	if err != nil {
		return RespondInternalError(c, "Failed to get pending Postie items", err.Error())
	}

	return RespondSuccess(c, items)
}

// handleGetFailedPostieItems handles GET /api/queue/postie/failed
// Returns all queue items where Postie upload has failed
func (s *Server) handleGetFailedPostieItems(c *fiber.Ctx) error {
	items, err := s.queueRepo.GetItemsByPostieStatus(c.Context(), "postie_failed")
	if err != nil {
		return RespondInternalError(c, "Failed to get failed Postie items", err.Error())
	}

	return RespondSuccess(c, items)
}

// handlePostieCheckTimeouts handles POST /api/queue/postie/check-timeouts
// Manually triggers the timeout check for Postie uploads
func (s *Server) handlePostieCheckTimeouts(c *fiber.Ctx) error {
	// This would require access to the Postie matcher
	// For now, return a not implemented response
	return RespondMessage(c, "Timeout check is run automatically by the background worker")
}
