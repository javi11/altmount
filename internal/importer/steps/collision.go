package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/metadata"
)

// HandleCollisionStep generates unique filenames for potential collisions
type HandleCollisionStep struct {
	metadataService *metadata.MetadataService
}

// NewHandleCollisionStep creates a step to handle filename collisions
func NewHandleCollisionStep(metadataService *metadata.MetadataService) *HandleCollisionStep {
	return &HandleCollisionStep{metadataService: metadataService}
}

// Execute handles filename collision (placeholder - actual collision handling is done per-file)
func (s *HandleCollisionStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	// Collision handling is done per-file during metadata creation
	return nil
}

// Name returns the step name
func (s *HandleCollisionStep) Name() string {
	return "HandleCollision"
}

// GetUniqueFilename generates a unique filename handling two types of collisions:
// 1. Within-batch collision (file from current import): Add suffix (_1, _2, etc.)
// 2. Cross-batch collision (file from previous import): Override by deleting old metadata
func GetUniqueFilename(
	basePath string,
	filename string,
	currentBatchFiles map[string]bool,
	metadataService *metadata.MetadataService,
) string {
	// Start with the original filename
	candidatePath := filepath.Join(basePath, filename)
	candidatePath = strings.ReplaceAll(candidatePath, string(filepath.Separator), "/")

	// Check if this path collides with a file from the current batch
	if currentBatchFiles[candidatePath] {
		// Within-batch collision: Add suffix to keep both files
		counter := 1
		candidateFilename := filename
		ext := filepath.Ext(filename)
		nameWithoutExt := strings.TrimSuffix(filename, ext)

		// Find next available suffix
		for {
			candidateFilename = fmt.Sprintf("%s_%d%s", nameWithoutExt, counter, ext)
			candidatePath = filepath.Join(basePath, candidateFilename)
			candidatePath = strings.ReplaceAll(candidatePath, string(filepath.Separator), "/")

			// Check if this suffixed path is also in current batch or exists on disk
			if !currentBatchFiles[candidatePath] {
				metadataPath := metadataService.GetMetadataFilePath(candidatePath)
				if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
					// Path is available
					return candidateFilename
				}
			}
			counter++
		}
	}

	// Check if metadata file exists from a previous import
	metadataPath := metadataService.GetMetadataFilePath(candidatePath)
	if _, err := os.Stat(metadataPath); err == nil {
		// Delete old metadata to allow override
		_ = metadataService.DeleteFileMetadata(candidatePath)
	}

	// No collision or handled cross-batch collision, use original filename
	return filename
}

// TrackBatchFile adds a file path to the current batch tracking map
func TrackBatchFile(virtualPath string, currentBatchFiles map[string]bool) {
	currentBatchFiles[virtualPath] = true
}
