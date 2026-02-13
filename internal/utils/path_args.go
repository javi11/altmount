package utils

// contextKey is a type for context keys to avoid collisions
type contextKey string

func (c contextKey) String() string {
	return "webdav context key " + string(c)
}

// Context keys for passing WebDAV request metadata through context
const (
	ContentLengthKey          = contextKey("contentLength")
	RangeKey                  = contextKey("rangeKey")
	IsCopy                    = contextKey("isCopy")
	Origin                    = contextKey("origin")
	ShowCorrupted             = contextKey("showCorrupted")
	ActiveStreamKey           = contextKey("activeStream")
	StreamIDKey               = contextKey("streamID")
	StreamSourceKey           = contextKey("streamSource")
	StreamUserNameKey         = contextKey("streamUserName")
	ClientIPKey               = contextKey("clientIP")
	UserAgentKey              = contextKey("userAgent")
	MaxPrefetchKey            = contextKey("maxPrefetch")
	SuppressStreamTrackingKey = contextKey("suppressStreamTracking")
)
