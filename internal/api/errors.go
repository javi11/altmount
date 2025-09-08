package api

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
