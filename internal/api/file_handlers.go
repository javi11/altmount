package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// handleGetFileMetadata handles GET /files/info requests
func (s *Server) handleGetFileMetadata(c *fiber.Ctx) error {
	// Get path from query parameters
	path := c.Query("path")
	if path == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Path parameter is required",
			"details": "MISSING_PATH",
		})
	}

	// Get metadata from the reader
	metadata, err := s.metadataReader.GetFileMetadata(path)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to read metadata",
			"details": err.Error(),
		})
	}

	if metadata == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "File metadata not found",
		})
	}

	// Convert protobuf metadata to API response
	response := s.convertToFileMetadataResponse(metadata)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// convertToFileMetadataResponse converts protobuf FileMetadata to API response
func (s *Server) convertToFileMetadataResponse(metadata *metapb.FileMetadata) *FileMetadataResponse {
	// Convert status enum to string
	statusStr := s.convertFileStatusToString(metadata.Status)

	// Convert encryption enum to string
	encryptionStr := s.convertEncryptionToString(metadata.Encryption)

	// Convert segments
	segments := make([]SegmentInfoResponse, len(metadata.SegmentData))
	for i, segment := range metadata.SegmentData {
		segments[i] = SegmentInfoResponse{
			SegmentSize: segment.SegmentSize,
			StartOffset: segment.StartOffset,
			EndOffset:   segment.EndOffset,
			MessageID:   segment.Id,
			Available:   true, // TODO: Implement actual availability check
		}
	}

	// Convert timestamps
	createdAt := time.Unix(metadata.CreatedAt, 0).Format(time.RFC3339)
	modifiedAt := time.Unix(metadata.ModifiedAt, 0).Format(time.RFC3339)

	return &FileMetadataResponse{
		FileSize:          metadata.FileSize,
		SourceNzbPath:     metadata.SourceNzbPath,
		Status:            statusStr,
		SegmentCount:      len(metadata.SegmentData),
		AvailableSegments: nil, // TODO: Implement actual available segment count
		Encryption:        encryptionStr,
		CreatedAt:         createdAt,
		ModifiedAt:        modifiedAt,
		PasswordProtected: metadata.Password != "",
		Segments:          segments,
	}
}

// convertFileStatusToString converts FileStatus enum to string
func (s *Server) convertFileStatusToString(status metapb.FileStatus) string {
	switch status {
	case metapb.FileStatus_FILE_STATUS_HEALTHY:
		return "healthy"
	case metapb.FileStatus_FILE_STATUS_PARTIAL:
		return "partial"
	case metapb.FileStatus_FILE_STATUS_CORRUPTED:
		return "corrupted"
	default:
		return "unspecified"
	}
}

// convertEncryptionToString converts Encryption enum to string
func (s *Server) convertEncryptionToString(encryption metapb.Encryption) string {
	switch encryption {
	case metapb.Encryption_RCLONE:
		return "rclone"
	case metapb.Encryption_HEADERS:
		return "headers"
	default:
		return "none"
	}
}
