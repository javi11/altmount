package usenet

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
)

func TestUsenetReader_Race_Close_GetBufferedOffset(t *testing.T) {
	// Setup
	ctx := context.Background()

	// Create a dummy segment range
	segments := []*segment{
		newSegment("1", 0, 100, 101, nil),
	}
	rg := &segmentRange{
		segments: segments,
		start:    0,
		end:      100,
		ctx:      ctx,
	}

	// Mock pool getter that returns error (so we don't need real pool)
	poolGetter := func() (*nntppool.Client, error) {
		return nil, fmt.Errorf("mock error")
	}

	ur, err := NewUsenetReader(ctx, poolGetter, rg, 10, nil, "", nil)
	if err != nil {
		t.Fatalf("Failed to create UsenetReader: %v", err)
	}

	// Start a goroutine that repeatedly calls GetBufferedOffset
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Run for a bit to ensure we overlap with Close
		for range 1000 {
			_ = ur.GetBufferedOffset()
			time.Sleep(10 * time.Microsecond)
		}
	}()

	// Give it a moment to start
	time.Sleep(2 * time.Millisecond)

	// Close concurrently
	// This was causing a race/panic before the fix
	err = ur.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Wait for goroutine
	<-done
}
