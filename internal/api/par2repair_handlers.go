package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

// Par2RepairEnqueuer queues a file for background PAR2 repair (implemented by
// par2repair.Service).
type Par2RepairEnqueuer interface {
	Enqueue(ctx context.Context, filePath string, failingSegmentID string)
}

// Par2RepairRequest is the body of POST /api/par2repair.
type Par2RepairRequest struct {
	FilePath string `json:"file_path"`
}

// SetPar2RepairEnqueuer wires the PAR2 repair queue into the API server.
func (s *Server) SetPar2RepairEnqueuer(re Par2RepairEnqueuer) {
	s.par2Repair = re
}

// handlePar2Repair handles POST /api/par2repair: queue a background PAR2
// repair for one file (by its virtual path).
func (s *Server) handlePar2Repair(c *fiber.Ctx) error {
	if s.par2Repair == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}
	var req Par2RepairRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}
	if req.FilePath == "" {
		return RespondBadRequest(c, "file_path is required", "")
	}
	if s.metadataService != nil && !s.metadataService.FileExists(req.FilePath) {
		return RespondNotFound(c, "File", req.FilePath)
	}
	s.par2Repair.Enqueue(c.Context(), req.FilePath, "")
	return RespondMessage(c, "PAR2 repair queued")
}
