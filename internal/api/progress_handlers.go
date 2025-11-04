package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// handleProgressStream handles GET /api/queue/progress/stream
// Server-Sent Events endpoint for real-time progress updates
func (s *Server) handleProgressStream(c *fiber.Ctx) error {
	// Set SSE headers
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")
	c.Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create a context for this SSE connection with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		// Subscribe to progress updates
		subID, updateCh := s.progressBroadcaster.Subscribe()
		defer s.progressBroadcaster.Unsubscribe(subID)

		// Send initial progress state
		initialProgress := s.progressBroadcaster.GetAllProgress()
		initialData, err := json.Marshal(fiber.Map{
			"type": "initial",
			"data": initialProgress,
		})
		if err != nil {
			slog.ErrorContext(c.Context(), "failed to marshal initial progress", "error", err)
			return
		}

		// Send initial state
		fmt.Fprintf(w, "data: %s\n\n", initialData)
		if err := w.Flush(); err != nil {
			return
		}

		// Create a ticker for keep-alive messages (every 30 seconds)
		keepAliveTicker := time.NewTicker(30 * time.Second)
		defer keepAliveTicker.Stop()

		// Stream updates until client disconnects
		for {
			select {
			case update, ok := <-updateCh:
				if !ok {
					// Channel closed, subscriber removed
					return
				}

				// Marshal update
				updateData, err := json.Marshal(fiber.Map{
					"type": "update",
					"data": update,
				})
				if err != nil {
					slog.ErrorContext(c.Context(), "failed to marshal progress update", "error", err, "queue_id", update.QueueID)
					continue
				}

				// Send update to client
				fmt.Fprintf(w, "data: %s\n\n", updateData)
				if err := w.Flush(); err != nil {
					// Client disconnected
					return
				}

			case <-keepAliveTicker.C:
				// Send keep-alive comment to prevent connection timeout
				fmt.Fprintf(w, ": keep-alive\n\n")
				if err := w.Flush(); err != nil {
					// Client disconnected
					return
				}

			case <-ctx.Done():
				// Context cancelled
				return
			}
		}
	})

	return nil
}
