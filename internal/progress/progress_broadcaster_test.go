package progress

import "testing"

func TestBroadcastFullChannelKeepsLatestUpdate(t *testing.T) {
	pb := NewProgressBroadcaster()
	subID, ch := pb.Subscribe()
	defer pb.Unsubscribe(subID)

	const capacity = 10
	for i := range capacity + 1 {
		pb.UpdateProgress(1, i)
	}

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
