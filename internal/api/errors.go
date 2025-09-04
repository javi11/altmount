package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Standard error codes
const (
	ErrCodeInternalServer = "INTERNAL_SERVER_ERROR"
	ErrCodeBadRequest     = "BAD_REQUEST"
	ErrCodeNotFound       = "NOT_FOUND"
	ErrCodeValidation     = "VALIDATION_ERROR"
	ErrCodeConflict       = "CONFLICT"
	ErrCodeUnauthorized   = "UNAUTHORIZED"
	ErrCodeForbidden      = "FORBIDDEN"
)

// Standard error messages
const (
	ErrMsgInternalServer = "An internal server error occurred"
	ErrMsgBadRequest     = "Invalid request format"
	ErrMsgNotFound       = "Resource not found"
	ErrMsgValidation     = "Request validation failed"
	ErrMsgConflict       = "Resource conflict"
	ErrMsgUnauthorized   = "Authentication required"
	ErrMsgForbidden      = "Access forbidden"
)

// APIErrorResponse represents a structured error response
type APIErrorResponse struct {
	Success bool      `json:"success"`
	Error   *APIError `json:"error"`
}

// NewAPIError creates a new API error
func NewAPIError(code, message, details string) *APIError {
	return &APIError{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// NewAPIErrorResponse creates a new API error response
func NewAPIErrorResponse(code, message, details string) *APIErrorResponse {
	return &APIErrorResponse{
		Success: false,
		Error:   NewAPIError(code, message, details),
	}
}

// WriteError writes an error response to the HTTP response writer
func WriteError(w http.ResponseWriter, statusCode int, code, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := NewAPIErrorResponse(code, message, details)

	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		slog.Error("Failed to encode error response", "err", err)
		// Fallback to plain text if JSON encoding fails
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal server error"))
	}
}

// WriteSuccess writes a successful response to the HTTP response writer
func WriteSuccess(w http.ResponseWriter, data interface{}, meta *APIMeta) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := &APIResponse{
		Success: true,
		Data:    data,
		Meta:    meta,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode success response", "err", err)
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalServer, ErrMsgInternalServer, "Failed to encode response")
	}
}

// WriteCreated writes a successful creation response
func WriteCreated(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := &APIResponse{
		Success: true,
		Data:    data,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode created response", "err", err)
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalServer, ErrMsgInternalServer, "Failed to encode response")
	}
}

// WriteNoContent writes a successful no content response
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Common error response helpers

// WriteBadRequest writes a 400 Bad Request response
func WriteBadRequest(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgBadRequest
	}
	WriteError(w, http.StatusBadRequest, ErrCodeBadRequest, message, details)
}

// WriteNotFound writes a 404 Not Found response
func WriteNotFound(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgNotFound
	}
	WriteError(w, http.StatusNotFound, ErrCodeNotFound, message, details)
}

// WriteValidationError writes a 400 validation error response
func WriteValidationError(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgValidation
	}
	WriteError(w, http.StatusBadRequest, ErrCodeValidation, message, details)
}

// WriteInternalError writes a 500 Internal Server Error response
func WriteInternalError(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgInternalServer
	}
	WriteError(w, http.StatusInternalServerError, ErrCodeInternalServer, message, details)
}

// WriteConflict writes a 409 Conflict response
func WriteConflict(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgConflict
	}
	WriteError(w, http.StatusConflict, ErrCodeConflict, message, details)
}

// WriteUnauthorized writes a 401 Unauthorized response
func WriteUnauthorized(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgUnauthorized
	}
	WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, message, details)
}

// WriteForbidden writes a 403 Forbidden response
func WriteForbidden(w http.ResponseWriter, message, details string) {
	if message == "" {
		message = ErrMsgForbidden
	}
	WriteError(w, http.StatusForbidden, ErrCodeForbidden, message, details)
}
