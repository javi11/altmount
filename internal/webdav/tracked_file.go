package webdav

import (
	"errors"
	"io"
)

// readTracker wraps a File so the handler can learn how a body ended:
// http.ServeContent swallows the reader's error and just stops writing, which
// the client sees as a truncated body with nothing on the server side to
// explain it.
type readTracker struct {
	File
	bytesRead int64
	err       error
}

func (t *readTracker) Read(p []byte) (int, error) {
	n, err := t.File.Read(p)
	t.bytesRead += int64(n)
	if err != nil && !errors.Is(err, io.EOF) && t.err == nil {
		t.err = err
	}
	return n, err
}
