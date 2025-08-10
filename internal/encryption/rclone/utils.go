package rclone

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const splitter = ":salt:"

var ErrInvalidPassword = errors.New("invalid password")

func ExtractPasswordAndSalt(password string) (string, string) {
	p := strings.Split(password, splitter)
	if len(p) != 2 {
		return "", ""
	}

	return p[0], p[1]
}

func PasswordFromPasswordAndSalt(password, salt string) string {
	return fmt.Sprintf(
		"%s%s%s",
		password,
		splitter,
		salt,
	)
}

func DecryptedSize(size int64) (int64, error) {
	size -= int64(fileHeaderSize)
	if size < 0 {
		return 0, ErrorEncryptedFileTooShort
	}
	blocks, residue := size/blockSize, size%blockSize
	decryptedSize := blocks * blockDataSize
	if residue != 0 {
		residue -= blockHeaderSize
		if residue <= 0 {
			return 0, ErrorEncryptedFileBadHeader
		}
	}
	decryptedSize += residue
	return decryptedSize, nil
}

func getEncryptedFileSize(metadata map[string]string) (int64, error) {
	size, ok := metadata["cipher_file_size"]
	if !ok {
		return 0, ErrMissingEncryptedFileSize
	}

	return strconv.ParseInt(size, 10, 64)
}

func getPassword(metadata map[string]string) (string, error) {
	password, ok := metadata["password"]
	if !ok {
		return "", ErrMissingPassword
	}

	return url.QueryUnescape(password)
}

func getSalt(metadata map[string]string) (string, error) {
	salt, ok := metadata["salt"]
	if !ok {
		return "", ErrMissingSalt
	}

	return url.QueryUnescape(salt)
}
