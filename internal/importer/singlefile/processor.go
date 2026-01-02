package singlefile

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/javi11/altmount/internal/importer/common"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/utils"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
)

// ProcessSingleFile processes a single file (creates and writes metadata)
func ProcessSingleFile(
	ctx context.Context,
	virtualDir string,
	file parser.ParsedFile,
	par2Files []parser.ParsedFile,
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	segmentSamplePercentage int,
	allowedFileExtensions []string,
	timeout time.Duration,
) (string, error) {
	// Validate file extension before processing
	if !utils.HasAllowedFilesInRegular([]parser.ParsedFile{file}, allowedFileExtensions) {
		slog.WarnContext(ctx, "File does not match allowed extensions",
			"filename", file.Filename,
			"allowed_extensions", allowedFileExtensions)
		return "", fmt.Errorf("file '%s' does not match allowed extensions (allowed: %v)", file.Filename, allowedFileExtensions)
	}

	opts := common.ImportOptions{
		MetadataService:         metadataService,
		PoolManager:             poolManager,
		MaxValidationGoroutines: maxValidationGoroutines,
		SegmentSamplePercentage: segmentSamplePercentage,
		AllowedFileExtensions:   allowedFileExtensions,
		Timeout:                 timeout,
	}

	par2Refs := common.ConvertPar2Files(par2Files)

	virtualPath, err := common.ImportFile(ctx, virtualDir, file, par2Refs, nzbPath, opts)
	if err != nil {
		return "", err
	}

	// Double check if file was skipped (though we checked allowed extensions earlier)
	if virtualPath == "" {
		return "", fmt.Errorf("file '%s' is not allowed", file.Filename)
	}

	slog.InfoContext(ctx, "Successfully processed single file",
		"file", file.Filename,
		"virtual_path", virtualPath,
		"size", file.Size)

	return virtualDir, nil
}