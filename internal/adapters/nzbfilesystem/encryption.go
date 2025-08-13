package nzbfilesystem

import (
	"context"
	"fmt"
	"io"

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

// wrapWithEncryption wraps a usenet reader with rclone decryption
func (vf *VirtualFile) wrapWithEncryption(start, end int64) (io.ReadCloser, error) {
	if *vf.virtualFile.Encryption == string(encryption.RCloneCipherType) && vf.rcloneCipher == nil {
		return nil, ErrNoCipherConfig
	} else if *vf.virtualFile.Encryption == string(encryption.HeadersCipherType) && vf.headersCipher == nil {
		return nil, ErrNoCipherConfig
	}

	if vf.nzbFile == nil {
		return nil, ErrNoEncryptionParams
	}

	// Get password and salt from NZB metadata, with global fallback
	var password, salt string

	if vf.nzbFile.RclonePassword != nil && *vf.nzbFile.RclonePassword != "" {
		password = *vf.nzbFile.RclonePassword
	}

	if vf.nzbFile.RcloneSalt != nil && *vf.nzbFile.RcloneSalt != "" {
		salt = *vf.nzbFile.RcloneSalt
	}

	var chypher encryption.Cipher
	if *vf.virtualFile.Encryption == string(encryption.RCloneCipherType) {
		chypher = vf.rcloneCipher
	} else if *vf.virtualFile.Encryption == string(encryption.HeadersCipherType) {
		chypher = vf.headersCipher
	} else {
		return nil, fmt.Errorf("unsupported encryption type: %s", *vf.virtualFile.Encryption)
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
