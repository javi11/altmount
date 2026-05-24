package archive

import (
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// NewFileMetadataFromContent creates a FileMetadata from a Content (with its NestedSources)
// for the metadata system. It mirrors the conversion previously inlined inside
// rar.CreateFileMetadataFromRarContent and sevenzip.CreateFileMetadataFromSevenZipContent
// so that non-RAR/non-7z callers (e.g. ISO expansion) can produce equivalent metadata.
//
// Behaviour:
//   - Sets CreatedAt/ModifiedAt to time.Now().Unix().
//   - Defaults Status to FILE_STATUS_HEALTHY.
//   - Copies SegmentData from content.Segments.
//   - When content.AesKey is non-empty, sets Encryption=AES with key/iv.
//   - Appends one NestedSegmentSource per content.NestedSources entry.
func NewFileMetadataFromContent(
	content Content,
	sourceNzbPath string,
	releaseDate int64,
	nzbdavId string,
) *metapb.FileMetadata {
	now := time.Now().Unix()

	meta := &metapb.FileMetadata{
		FileSize:      content.Size,
		SourceNzbPath: sourceNzbPath,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		CreatedAt:     now,
		ModifiedAt:    now,
		SegmentData:   content.Segments,
		ReleaseDate:   releaseDate,
		NzbdavId:      nzbdavId,
	}

	// Set AES encryption if keys are present (single-layer encrypted archive)
	if len(content.AesKey) > 0 {
		meta.Encryption = metapb.Encryption_AES
		meta.AesKey = content.AesKey
		meta.AesIv = content.AesIV
	}

	// Populate nested sources for encrypted nested archive files
	for _, ns := range content.NestedSources {
		meta.NestedSources = append(meta.NestedSources, &metapb.NestedSegmentSource{
			Segments:        ns.Segments,
			AesKey:          ns.AesKey,
			AesIv:           ns.AesIV,
			InnerOffset:     ns.InnerOffset,
			InnerLength:     ns.InnerLength,
			InnerVolumeSize: ns.InnerVolumeSize,
		})
	}

	return meta
}
