package headers

import (
	"net/url"
)

func getNonce(salt string) ([]byte, error) {
	nc, err := url.QueryUnescape(salt)
	if err != nil {
		return nil, err
	}

	return []byte(nc), nil
}
