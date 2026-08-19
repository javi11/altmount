package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/javi11/altmount/internal/database"
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

// SetPar2RepairRepo wires the PAR2 repair job repository so the API can list
// recent jobs.
func (s *Server) SetPar2RepairRepo(repo *database.Par2RepairRepository) {
	s.par2RepairRepo = repo
}

// Par2RepairJobResponse is the JSON shape of one repair job row.
type Par2RepairJobResponse struct {
	ID            int64      `json:"id"`
	FilePath      string     `json:"file_path"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	LastError     *string    `json:"last_error,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func toPar2RepairJobResponse(job *database.Par2RepairJob) Par2RepairJobResponse {
	resp := Par2RepairJobResponse{
		ID:        job.ID,
		FilePath:  job.FilePath,
		Status:    string(job.Status),
		Attempts:  job.Attempts,
		CreatedAt: job.CreatedAt,
		UpdatedAt: job.UpdatedAt,
	}
	if job.LastError.Valid {
		lastErr := job.LastError.String
		resp.LastError = &lastErr
	}
	if job.NextAttemptAt.Valid {
		next := job.NextAttemptAt.Time
		resp.NextAttemptAt = &next
	}
	return resp
}

// handleListPar2Repair handles GET /api/par2repair: list the most recently
// updated PAR2 repair jobs.
func (s *Server) handleListPar2Repair(c *fiber.Ctx) error {
	if s.par2RepairRepo == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}
	jobs, err := s.par2RepairRepo.List(c.Context(), c.QueryInt("limit", 0))
	if err != nil {
		return RespondInternalError(c, "Failed to list PAR2 repair jobs", err.Error())
	}
	resp := make([]Par2RepairJobResponse, 0, len(jobs))
	for _, job := range jobs {
		resp = append(resp, toPar2RepairJobResponse(job))
	}
	return RespondSuccess(c, resp)
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
