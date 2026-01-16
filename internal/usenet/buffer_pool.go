package usenet

import "bytes"

const (
	// defaultBufferSize is the default pre-allocation size for segment buffers.
	// Most usenet segments are around 768KB.
	defaultBufferSize = 768 * 1024
)

// getBuffer allocates a new buffer with the requested capacity.
// We don't use sync.Pool because:
// 1. sync.Pool doesn't bound memory - it can grow indefinitely
// 2. Slice references (from bytes.NewReader) prevent proper recycling
// 3. maxCacheSize already limits concurrent segments effectively
func getBuffer(size int64) *bytes.Buffer {
	capacity := size
	if capacity < defaultBufferSize {
		capacity = defaultBufferSize
	}
	return bytes.NewBuffer(make([]byte, 0, capacity))
}

// putBuffer is a no-op - we let GC handle buffer cleanup.
// The buffer will be collected once segment.Close() clears all references
// (both s.buffer and s.cachedReader).
func putBuffer(buf *bytes.Buffer) {
	// No-op: GC will collect when segment.Close() clears all references
}
