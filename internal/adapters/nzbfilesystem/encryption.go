package nzbfilesystem

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nzbparser"
)

// dbSegmentLoader adapts DB segments to the usenet.SegmentLoader interface
type dbSegmentLoader struct {
	segs database.NzbSegments
}

func (l dbSegmentLoader) GetSegment(index int) (segment nzbparser.NzbSegment, groups []string, ok bool) {
	if index < 0 || index >= len(l.segs) {
		return nzbparser.NzbSegment{}, nil, false
	}
	s := l.segs[index]
	return nzbparser.NzbSegment{Number: s.Number, Bytes: int(s.Bytes), ID: s.MessageID}, s.Groups, true
}

// segmentDataLoader adapts the new SegmentData format to the usenet.SegmentLoader interface
type segmentDataLoader struct {
	segmentData *database.SegmentData
	messageIDs  []string
}

func newSegmentDataLoader(segmentData *database.SegmentData) *segmentDataLoader {
	if segmentData == nil || segmentData.ID == "" {
		return &segmentDataLoader{
			segmentData: segmentData,
			messageIDs:  []string{},
		}
	}

	// Split comma-separated message IDs
	messageIDs := strings.Split(segmentData.ID, ",")
	for i, id := range messageIDs {
		messageIDs[i] = strings.TrimSpace(id)
	}

	return &segmentDataLoader{
		segmentData: segmentData,
		messageIDs:  messageIDs,
	}
}

func (l *segmentDataLoader) GetSegment(index int) (segment nzbparser.NzbSegment, groups []string, ok bool) {
	if l.segmentData == nil || index < 0 || index >= len(l.messageIDs) {
		return nzbparser.NzbSegment{}, nil, false
	}

	// Calculate approximate bytes per segment (total bytes divided by number of segments)
	bytesPerSegment := l.segmentData.Bytes
	if len(l.messageIDs) > 0 {
		bytesPerSegment = l.segmentData.Bytes / int64(len(l.messageIDs))
	}

	return nzbparser.NzbSegment{
		Number: index + 1, // 1-based numbering
		Bytes:  int(bytesPerSegment),
		ID:     l.messageIDs[index],
	}, []string{}, true // Empty groups for now - this might need to be stored separately
}

// wrapWithEncryption wraps a usenet reader with rclone decryption
func (vf *VirtualFile) wrapWithEncryption(start, end int64) (io.ReadCloser, error) {
	if vf.nzbFile == nil || vf.nzbFile.Encryption == nil {
		return nil, ErrNoEncryptionParams
	}

	encryptionType := *vf.nzbFile.Encryption
	if encryptionType == string(encryption.RCloneCipherType) && vf.rcloneCipher == nil {
		return nil, ErrNoCipherConfig
	} else if encryptionType == string(encryption.HeadersCipherType) && vf.headersCipher == nil {
		return nil, ErrNoCipherConfig
	}

	// Get password and salt from NZB metadata, with global fallback
	var password, salt string

	if vf.nzbFile.Password != nil && *vf.nzbFile.Password != "" {
		password = *vf.nzbFile.Password
	} else {
		password = vf.globalPassword
	}

	if vf.nzbFile.Salt != nil && *vf.nzbFile.Salt != "" {
		salt = *vf.nzbFile.Salt
	} else {
		salt = vf.globalSalt
	}

	var chypher encryption.Cipher
	if encryptionType == string(encryption.RCloneCipherType) {
		chypher = vf.rcloneCipher
	} else if encryptionType == string(encryption.HeadersCipherType) {
		chypher = vf.headersCipher
	} else {
		return nil, fmt.Errorf("unsupported encryption type: %s", encryptionType)
	}

	decryptedReader, err := chypher.Open(
		vf.ctx,
		&utils.RangeHeader{
			Start: start,
			End:   end,
		},
		vf.virtualFile.Size,
		password,
		salt,
		func(ctx context.Context, start, end int64) (io.ReadCloser, error) {
			return vf.createUsenetReader(ctx, start, end)
		},
	)
	if err != nil {
		return nil, fmt.Errorf(ErrMsgFailedCreateDecryptReader, err)
	}

	return decryptedReader, nil
}
