package nzbfilesystem

import (
	"context"
	"fmt"
	"io"

	"github.com/javi11/altmount/internal/database"
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

// wrapWithEncryption wraps a usenet reader with rclone decryption
func (vf *VirtualFile) wrapWithEncryption(start, end int64) (io.ReadCloser, error) {
	if vf.rcloneCipher == nil {
		return nil, ErrNoCipherConfig
	}

	if vf.nzbFile == nil {
		return nil, ErrNoEncryptionParams
	}

	// Get password and salt from NZB metadata, with global fallback
	var password, salt string

	if vf.nzbFile.RclonePassword != nil && *vf.nzbFile.RclonePassword != "" {
		password = *vf.nzbFile.RclonePassword
	} else {
		// Fallback to global password
		password = vf.globalPassword
	}

	if vf.nzbFile.RcloneSalt != nil && *vf.nzbFile.RcloneSalt != "" {
		salt = *vf.nzbFile.RcloneSalt
	} else {
		// Fallback to global salt
		salt = vf.globalSalt
	}

	decryptedReader, err := vf.rcloneCipher.Open(
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
