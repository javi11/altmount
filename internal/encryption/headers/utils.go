package headers

import (
	"net/url"
	"strconv"
)

func getPassword(metadata map[string]string) (string, error) {
	password, ok := metadata["password"]
	if !ok {
		return "", ErrMissingPassword
	}

	return url.QueryUnescape(password)
}

func getNonce(metadata map[string]string) ([]byte, error) {
	salt, ok := metadata["salt"]
	if !ok {
		return nil, ErrMissingNonce
	}

	nc, err := url.QueryUnescape(salt)
	if err != nil {
		return nil, err
	}

	return []byte(nc), nil
}

func getFileSize(metadata map[string]string) (int64, error) {
	size, ok := metadata["file_size"]
	if !ok {
		return 0, ErrMissingFileSize
	}

	return strconv.ParseInt(size, 10, 64)
}
