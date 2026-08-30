package par2_test

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	"github.com/javi11/altmount/internal/testsupport/par2gen"
)

// barrierReader blocks its first Read until `need` barrierReaders have all
// started reading, then serves its buffer. Sequential stream consumption
// deadlocks on it (stream 0 waits for streams that are never started);
// concurrent consumption sails through.
type barrierReader struct {
	data    *bytes.Reader
	started *atomic.Int32
	need    int32
	release chan struct{}
	once    atomic.Bool
}

func (b *barrierReader) Read(p []byte) (int, error) {
	if b.once.CompareAndSwap(false, true) {
		if b.started.Add(1) == b.need {
			close(b.release)
		}
		select {
		case <-b.release:
		case <-time.After(5 * time.Second):
			return 0, errors.New("barrier timeout: streams were not read concurrently")
		}
	}
	return b.data.Read(p)
}

func (b *barrierReader) Seek(offset int64, whence int) (int64, error) {
	return b.data.Seek(offset, whence)
}

// ParseIndex must walk independent streams concurrently: each stream is a
// serial packet chain over the network, so consuming them one after another
// serializes per-article latency across the whole set.
func TestParseIndexReadsStreamsConcurrently(t *testing.T) {
	content := bytes.Repeat([]byte{0xAB}, 4096)
	set := par2gen.BuildFull(1024, []par2gen.FileEntry{{Name: "a.bin", Content: content}}, 4)

	raw := append([][]byte{set.Index}, set.Volumes...)
	need := int32(3) // index + first two volumes must be in flight together
	started := &atomic.Int32{}
	release := make(chan struct{})
	streams := make([]io.Reader, 0, len(raw))
	for i, b := range raw {
		if i < int(need) {
			streams = append(streams, &barrierReader{
				data: bytes.NewReader(b), started: started, need: need, release: release,
			})
			continue
		}
		streams = append(streams, bytes.NewReader(b))
	}

	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	if len(idx.Recovery) != 4 {
		t.Fatalf("recovery refs = %d, want 4", len(idx.Recovery))
	}
}

// Parallel parsing must produce the same index as before: recovery refs in
// stream order, and an exponent served by several streams kept from the
// earliest stream (first-wins), exactly like the sequential walk.
func TestParseIndexParallelMatchesSequentialSemantics(t *testing.T) {
	content := bytes.Repeat([]byte{0x5C}, 8192)
	set := par2gen.BuildFull(1024, []par2gen.FileEntry{{Name: "a.bin", Content: content}}, 3)

	// Duplicate volume 0 at the tail: its exponent must stay attributed to the
	// early stream (FileIndex 1), not the late duplicate.
	streams := []io.Reader{bytes.NewReader(set.Index)}
	for _, v := range set.Volumes {
		streams = append(streams, bytes.NewReader(v))
	}
	streams = append(streams, bytes.NewReader(set.Volumes[0]))

	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	if len(idx.Recovery) != 3 {
		t.Fatalf("recovery refs = %d, want 3 (duplicate exponent deduped)", len(idx.Recovery))
	}
	for i, ref := range idx.Recovery {
		if want := i + 1; ref.FileIndex != want {
			t.Fatalf("ref %d FileIndex = %d, want %d (stream order, first-wins)", i, ref.FileIndex, want)
		}
	}
}

// The progress variant reports completed streams out of the total.
func TestParseIndexWithProgressReportsCompletions(t *testing.T) {
	content := bytes.Repeat([]byte{0x11}, 4096)
	set := par2gen.BuildFull(1024, []par2gen.FileEntry{{Name: "a.bin", Content: content}}, 2)
	streams := []io.Reader{bytes.NewReader(set.Index)}
	for _, v := range set.Volumes {
		streams = append(streams, bytes.NewReader(v))
	}

	var calls atomic.Int32
	var last atomic.Int32
	_, err := par2.ParseIndexWithProgress(streams, func(done, total int) {
		calls.Add(1)
		last.Store(int32(done))
		if total != len(streams) {
			t.Errorf("total = %d, want %d", total, len(streams))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != int32(len(streams)) {
		t.Fatalf("progress calls = %d, want %d", got, len(streams))
	}
	if got := last.Load(); got != int32(len(streams)) {
		t.Fatalf("final done = %d, want %d", got, len(streams))
	}
}
