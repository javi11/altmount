package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ParsePagination extracts pagination parameters from query string
func ParsePagination(r *http.Request) Pagination {
	pagination := DefaultPagination()

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit <= 1000 {
			pagination.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			pagination.Offset = offset
		}
	}

	return pagination
}

// ParseTimeParam extracts time parameter from query string
func ParseTimeParam(r *http.Request, param string) (*time.Time, error) {
	value := r.URL.Query().Get(param)
	if value == "" {
		return nil, nil
	}

	// Try different time formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, value); err == nil {
			return &t, nil
		}
	}

	return nil, &ValidationError{Message: "Invalid time format for parameter: " + param}
}

// ValidationError represents a validation error
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// ParsePaginationFiber extracts pagination parameters from Fiber context
func ParsePaginationFiber(c *fiber.Ctx) Pagination {
	pagination := DefaultPagination()

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit <= 1000 {
			pagination.Limit = limit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			pagination.Offset = offset
		}
	}

	return pagination
}

// ParseTimeParamFiber extracts time parameter from Fiber context
func ParseTimeParamFiber(c *fiber.Ctx, param string) (*time.Time, error) {
	value := c.Query(param)
	if value == "" {
		return nil, nil
	}

	// Try different time formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, value); err == nil {
			return &t, nil
		}
	}

	return nil, &ValidationError{Message: "Invalid time format for parameter: " + param}
}

// validateAPIKey validates the API key using AltMount's authentication system
func (s *Server) validateAPIKey(c *fiber.Ctx, apiKey string) bool {
	if s.userRepo == nil {
		return false
	}

	user, err := s.userRepo.GetUserByAPIKey(apiKey)
	if err != nil || user == nil {
		return false
	}

	return true
}
