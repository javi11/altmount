// Package httpclient provides a centralized HTTP client factory with preset configurations.
package httpclient

import (
	"net/http"
	"time"
)

// Preset timeout durations for common use cases.
const (
	// DefaultTimeout is the standard timeout for most HTTP requests (30s).
	DefaultTimeout = 30 * time.Second

	// LongTimeout is for operations that may take longer (60s).
	LongTimeout = 60 * time.Second

	// FileUploadTimeout is for file upload operations (2 minutes).
	FileUploadTimeout = 2 * time.Minute
)

// Options configures an HTTP client.
type Options struct {
	Timeout   time.Duration
	Transport *http.Transport
}

// Option is a functional option for configuring HTTP clients.
type Option func(*Options)

// WithTimeout sets the client timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.Timeout = d
	}
}

// WithTransport sets a custom transport.
func WithTransport(t *http.Transport) Option {
	return func(o *Options) {
		o.Transport = t
	}
}

// New creates a new HTTP client with the given options.
// If no timeout is specified, DefaultTimeout (30s) is used.
func New(opts ...Option) *http.Client {
	cfg := &Options{
		Timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
	}

	if cfg.Transport != nil {
		client.Transport = cfg.Transport
	}

	return client
}

// NewDefault creates a new HTTP client with the default timeout (30s).
func NewDefault() *http.Client {
	return New()
}

// NewLong creates a new HTTP client with a longer timeout (60s).
// Suitable for operations that may take longer, such as external API calls.
func NewLong() *http.Client {
	return New(WithTimeout(LongTimeout))
}

// NewFileUpload creates a new HTTP client for file upload operations (2 minutes).
func NewFileUpload() *http.Client {
	return New(WithTimeout(FileUploadTimeout))
}
