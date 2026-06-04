package metadata

import (
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// MetadataReader provides read operations for the virtual filesystem
type MetadataReader struct {
	service *MetadataService
}

// NewMetadataReader creates a new metadata reader
func NewMetadataReader(service *MetadataService) *MetadataReader {
	return &MetadataReader{
		service: service,
	}
}

// GetFileMetadata gets metadata for a virtual file
func (mr *MetadataReader) GetFileMetadata(virtualPath string) (*metapb.FileMetadata, error) {
	return mr.service.ReadFileMetadata(virtualPath)
}

// GetMetadataService returns the underlying metadata service
func (mr *MetadataReader) GetMetadataService() *MetadataService {
	return mr.service
}
