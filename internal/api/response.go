package api

import (
	"github.com/gofiber/fiber/v2"
)

// Response builder functions for Fiber handlers.
// These provide a unified interface for API responses.

// RespondSuccess sends a successful response with data.
func RespondSuccess(c *fiber.Ctx, data any) error {
	return c.JSON(fiber.Map{
		"success": true,
		"data":    data,
	})
}

// RespondSuccessWithMeta sends a successful response with data and pagination metadata.
func RespondSuccessWithMeta(c *fiber.Ctx, data any, meta *APIMeta) error {
	return c.JSON(fiber.Map{
		"success": true,
		"data":    data,
		"meta":    meta,
	})
}

// RespondCreated sends a 201 Created response with data.
func RespondCreated(c *fiber.Ctx, data any) error {
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    data,
	})
}

// RespondNoContent sends a 204 No Content response.
func RespondNoContent(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}

// RespondMessage sends a successful response with a message only.
func RespondMessage(c *fiber.Ctx, message string) error {
	return c.JSON(fiber.Map{
		"success": true,
		"message": message,
	})
}

// Error response functions - all use the unified error format.

// RespondError sends an error response with a custom status code.
func RespondError(c *fiber.Ctx, status int, code, message, details string) error {
	return c.Status(status).JSON(fiber.Map{
		"success": false,
		"error": fiber.Map{
			"code":    code,
			"message": message,
			"details": details,
		},
	})
}

// RespondBadRequest sends a 400 Bad Request error.
func RespondBadRequest(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusBadRequest, ErrCodeBadRequest, message, details)
}

// RespondValidationError sends a 400 Bad Request error for validation failures.
func RespondValidationError(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusBadRequest, ErrCodeValidation, message, details)
}

// RespondUnauthorized sends a 401 Unauthorized error.
func RespondUnauthorized(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusUnauthorized, ErrCodeUnauthorized, message, details)
}

// RespondForbidden sends a 403 Forbidden error.
func RespondForbidden(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusForbidden, ErrCodeForbidden, message, details)
}

// RespondNotFound sends a 404 Not Found error.
func RespondNotFound(c *fiber.Ctx, resource, details string) error {
	message := resource + " not found"
	return RespondError(c, fiber.StatusNotFound, ErrCodeNotFound, message, details)
}

// RespondConflict sends a 409 Conflict error.
func RespondConflict(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusConflict, ErrCodeConflict, message, details)
}

// RespondInternalError sends a 500 Internal Server Error.
func RespondInternalError(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusInternalServerError, ErrCodeInternalServer, message, details)
}

// RespondServiceUnavailable sends a 503 Service Unavailable error.
func RespondServiceUnavailable(c *fiber.Ctx, message, details string) error {
	return RespondError(c, fiber.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", message, details)
}

// Helper function to check for admin privileges and respond with error if not admin.
// Returns true if user is admin, false otherwise (and sends error response).
func RequireAdminPrivileges(c *fiber.Ctx, user interface{ IsAdminUser() bool }) bool {
	if user == nil || !user.IsAdminUser() {
		RespondForbidden(c, "Admin privileges required", "This endpoint requires admin access")
		return false
	}
	return true
}
