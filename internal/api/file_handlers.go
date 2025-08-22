package api

import (
	"net/http"
	"net/url"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// handleGetFileMetadata handles GET /files/info requests
func (s *Server) handleGetFileMetadata(w http.ResponseWriter, r *http.Request) {
	// Get path from query parameters
	path := r.URL.Query().Get("path")
	if path == "" {
		WriteBadRequest(w, "Path parameter is required", "MISSING_PATH")
		return
	}

	// Decode the path in case it's URL encoded
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		WriteBadRequest(w, "Invalid path encoding", err.Error())
		return
	}

	// Get metadata from the reader
	metadata, err := s.metadataReader.GetFileMetadata(decodedPath)
	if err != nil {
		WriteInternalError(w, "Failed to read metadata", err.Error())
		return
	}

	if metadata == nil {
		WriteNotFound(w, "File metadata not found", "")
		return
	}

	// Convert protobuf metadata to API response
	response := s.convertToFileMetadataResponse(metadata)
	WriteSuccess(w, response, nil)
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
		AvailableSegments: len(metadata.SegmentData), // TODO: Implement actual available segment count
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