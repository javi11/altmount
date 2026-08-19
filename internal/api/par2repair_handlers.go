package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/par2repair"
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

// Par2RepairProgressSource reports a running job's live sweep progress
// (implemented by par2repair.Service).
type Par2RepairProgressSource interface {
	Progress(jobID int64) (par2repair.JobProgressSnapshot, bool)
}

// Par2RepairJobResponse is the JSON shape of one repair job row.
type Par2RepairJobResponse struct {
	ID            int64      `json:"id"`
	FilePath      string     `json:"file_path"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	LastError     *string    `json:"last_error,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	// DurationSeconds is how long the last attempt ran: final for a finished
	// job, elapsed so far for a running one.
	DurationSeconds *float64  `json:"duration_seconds,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Sweep progress, present only while the job is running and a sweep is
	// under way.
	ProgressDone  *int `json:"progress_done,omitempty"`
	ProgressTotal *int `json:"progress_total,omitempty"`
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
	if job.StartedAt.Valid {
		started := job.StartedAt.Time
		resp.StartedAt = &started
	}
	if job.FinishedAt.Valid {
		finished := job.FinishedAt.Time
		resp.FinishedAt = &finished
	}
	// Served rather than computed client-side so the number does not depend on
	// the browser's clock agreeing with the server's.
	if d, ok := job.RunDuration(time.Now().UTC()); ok {
		secs := d.Seconds()
		resp.DurationSeconds = &secs
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
	progressSource, _ := s.par2Repair.(Par2RepairProgressSource)
	resp := make([]Par2RepairJobResponse, 0, len(jobs))
	for _, job := range jobs {
		row := toPar2RepairJobResponse(job)
		if progressSource != nil && job.Status == database.Par2RepairStatusRunning {
			if p, ok := progressSource.Progress(job.ID); ok {
				done, total := p.DoneArticles, p.TotalArticles
				row.ProgressDone, row.ProgressTotal = &done, &total
			}
		}
		resp = append(resp, row)
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

// Par2RepairCanceller stops an in-flight or queued repair and cleans its
// transient artifacts (implemented by par2repair.Service).
type Par2RepairCanceller interface {
	Cancel(ctx context.Context, jobID int64) error
}

// canceller returns the wired repair service as a canceller, or nil when PAR2
// repair is unavailable in this build/config.
func (s *Server) canceller() Par2RepairCanceller {
	c, _ := s.par2Repair.(Par2RepairCanceller)
	return c
}

// handleCancelPar2Repair handles DELETE /api/par2repair/:id: stop one queued or
// running repair and clean the artifacts it generated.
func (s *Server) handleCancelPar2Repair(c *fiber.Ctx) error {
	canceller := s.canceller()
	if canceller == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return RespondBadRequest(c, "Invalid job ID", c.Params("id"))
	}
	switch err := canceller.Cancel(c.Context(), id); {
	case err == nil:
		return RespondMessage(c, "PAR2 repair cancelled")
	case errors.Is(err, par2repair.ErrJobNotFound):
		return RespondNotFound(c, "PAR2 repair job", c.Params("id"))
	default:
		return RespondInternalError(c, "Failed to cancel PAR2 repair", err.Error())
	}
}

// handleCancelAllPar2Repair handles DELETE /api/par2repair: cancel every queued
// and running repair.
//
// It drains in passes rather than one shot because List caps its result at 100
// rows, so a single pass is not guaranteed to see the whole queue. A pass that
// cancels nothing ends the loop, which keeps it finite when a row cannot be
// removed.
func (s *Server) handleCancelAllPar2Repair(c *fiber.Ctx) error {
	canceller := s.canceller()
	if canceller == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}
	if s.par2RepairRepo == nil {
		return RespondServiceUnavailable(c, "PAR2 repair is not available", "")
	}

	var cancelled int
	for {
		jobs, err := s.par2RepairRepo.List(c.Context(), 0)
		if err != nil {
			return RespondInternalError(c, "Failed to list PAR2 repair jobs", err.Error())
		}
		if len(jobs) == 0 {
			break
		}
		progressed := false
		for _, job := range jobs {
			err := canceller.Cancel(c.Context(), job.ID)
			switch {
			case err == nil:
				cancelled++
				progressed = true
			case errors.Is(err, par2repair.ErrJobNotFound):
				// Finished between the list and the cancel; nothing to do.
			default:
				return RespondInternalError(c, "Failed to cancel PAR2 repair", err.Error())
			}
		}
		if !progressed {
			break
		}
	}
	return RespondSuccess(c, fiber.Map{"cancelled": cancelled})
}
