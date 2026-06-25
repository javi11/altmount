package health

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"golift.io/starr"
)

// TestIsArrUnreachable_ServerErrors verifies that the transient-error classifier defers
// (returns true) for server-side HTTP 5xx responses — whether they arrive as a typed
// *starr.ReqError, wrapped in the error chain, or already flattened to a string — while
// still rejecting client errors and ARR-confirmed sentinels that must condemn the file.
func TestIsArrUnreachable_ServerErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Typed starr server errors -> retryable
		{"typed 500", &starr.ReqError{Code: 500}, true},
		{"typed 502", &starr.ReqError{Code: 502}, true},
		{"typed 503", &starr.ReqError{Code: 503}, true},
		{"typed 599", &starr.ReqError{Code: 599}, true},
		// Typed starr error wrapped in the chain (as the scanner manager does with %w)
		{"wrapped typed 500", fmt.Errorf("failed to get movies from Radarr: %w", &starr.ReqError{Code: 500}), true},
		// Stringified starr 5xx (typed error flattened upstream)
		{"stringified starr 5xx", errors.New("invalid status code, 503 >= 300, Some Body"), true},
		// Generic textual 5xx phrasings
		{"500 internal server error text", errors.New("500 Internal Server Error"), true},
		{"status code 500 text", errors.New("unexpected status code 500"), true},
		// Existing gateway phrases still match
		{"502 bad gateway", errors.New("502 Bad Gateway"), true},
		// Client errors must NOT defer (they are not transient server outages)
		{"typed 404", &starr.ReqError{Code: 404}, false},
		{"typed 401", &starr.ReqError{Code: 401}, false},
		{"typed 400", &starr.ReqError{Code: 400}, false},
		// Unrelated errors and nil
		{"plain error", errors.New("something unexpected happened"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isArrUnreachable(tt.err))
		})
	}
}
