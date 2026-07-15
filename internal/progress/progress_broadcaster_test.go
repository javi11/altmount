package progress

import "testing"

// TestBroadcastFullChannelKeepsLatestUpdate verifies that when a subscriber's
// buffered channel is full, the broadcaster drops the oldest queued update
// rather than the new one, so the subscriber eventually observes the most
// recent progress state instead of getting stuck behind stale values.
func TestBroadcastFullChannelKeepsLatestUpdate(t *testing.T) {
	pb := NewProgressBroadcaster()
	subID, ch := pb.Subscribe()
	defer pb.Unsubscribe(subID)

	// Fill the channel (capacity 10) and send one more to force the
	// full-channel path.
	const capacity = 10
	for i := range capacity + 1 {
		pb.UpdateProgress(1, i)
	}

	// Drain the channel and remember the last percentage observed.
	var last int
	var count int
	for {
		select {
		case update := <-ch:
			last = update.Percentage
			count++
			continue
		default:
		}
		break
	}

	if count != capacity {
		t.Fatalf("expected channel to hold %d updates, got %d", capacity, count)
	}
	if last != capacity {
		t.Fatalf("expected latest update (percentage=%d) to survive, got %d", capacity, last)
	}
}
