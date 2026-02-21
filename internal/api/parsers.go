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

// ParseDuration parses a duration string, supporting 'd' for days
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	// Handle simple days or weeks format (e.g., "1d", "7d", "1w")
	if len(s) > 1 {
		unit := s[len(s)-1]
		valStr := s[:len(s)-1]
		val, err := strconv.Atoi(valStr)
		if err == nil {
			if unit == 'd' {
				return time.Duration(val) * 24 * time.Hour, nil
			}
			if unit == 'w' {
				return time.Duration(val) * 7 * 24 * time.Hour, nil
			}
		}
	}

	return time.ParseDuration(s)
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
// First checks if there's a key_override in config (must be exactly 32 characters)
// Then falls back to checking the database
func (s *Server) validateAPIKey(c *fiber.Ctx, apiKey string) bool {
	if apiKey == "" {
		return false
	}

	// Check config key_override first (must be exactly 32 characters to be valid)
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		if cfg.API.KeyOverride != "" && len(cfg.API.KeyOverride) == 32 {
			if apiKey == cfg.API.KeyOverride {
				return true
			}
		}
	}

	// Fall back to database validation
	if s.userRepo == nil {
		return false
	}

	user, err := s.userRepo.GetUserByAPIKey(c.Context(), apiKey)
	if err != nil || user == nil {
		return false
	}

	return true
}
