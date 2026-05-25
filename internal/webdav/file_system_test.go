package webdav

import (
	"errors"
	"net/http"
	"testing"

	"github.com/javi11/altmount/internal/nzbfilesystem"
)

func mappedStatus(t *testing.T, in error) int {
	t.Helper()
	c := &customErrorHandler{}
	out := c.mapError(in)
	var he *HTTPError
	if !errors.As(out, &he) {
		t.Fatalf("expected *HTTPError, got %T (%v)", out, out)
	}
	return he.StatusCode
}

func TestMapErrorCorruptedIsUnprocessable(t *testing.T) {
	err := &nzbfilesystem.CorruptedFileError{TotalExpected: 100, UnderlyingErr: errors.New("missing articles")}
	if got := mappedStatus(t, err); got != http.StatusUnprocessableEntity {
		t.Errorf("corrupted file mapped to %d, want 422", got)
	}
}

func TestMapErrorSentinelCorruptedIsUnprocessable(t *testing.T) {
	if got := mappedStatus(t, nzbfilesystem.ErrFileIsCorrupted); got != http.StatusUnprocessableEntity {
		t.Errorf("ErrFileIsCorrupted mapped to %d, want 422", got)
	}
}

func TestMapErrorPartialIs206(t *testing.T) {
	err := &nzbfilesystem.PartialContentError{BytesRead: 10, TotalExpected: 100, UnderlyingErr: errors.New("some missing")}
	if got := mappedStatus(t, err); got != http.StatusPartialContent {
		t.Errorf("partial content mapped to %d, want 206", got)
	}
}

func TestMapErrorPassthrough(t *testing.T) {
	plain := errors.New("some unrelated error")
	c := &customErrorHandler{}
	if out := c.mapError(plain); out != plain {
		t.Errorf("unrelated error should pass through unchanged, got %v", out)
	}
}
