package parser

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
)

type recordingStore struct {
	mu   sync.Mutex
	puts map[string][]byte
}

func (s *recordingStore) Get(string) ([]byte, bool) { return nil, false }
func (s *recordingStore) Put(id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.puts == nil {
		s.puts = map[string][]byte{}
	}
	s.puts[id] = append([]byte(nil), data...)
	return nil
}

func (s *recordingStore) get(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.puts[id]
	return d, ok
}

// The first article of every file is fetched at import anyway; handing the
// whole decoded body to the streaming segment store means the cold open a
// moment later is served from memory instead of a second provider round trip.
func TestWarmFirstSegmentsPublishesWholeArticleToSegmentStore(t *testing.T) {
	article := bytes.Repeat([]byte("A"), 700*1024)
	fp := fakepool.New()
	fp.SetBehavior("vid-0", fakepool.SegmentBehavior{
		Bytes: article,
		YEnc:  nntppool.YEncMeta{FileName: "Real.Movie.2024.mkv", FileSize: int64(len(article)) * 2, Part: 1, PartSize: int64(len(article))},
	})
	nzb := &nzbparser.Nzb{Files: nzbparser.NzbFiles{
		{Filename: "Real.Movie.2024.mkv", Segments: nzbparser.NzbSegments{
			{Bytes: 720000, Number: 1, ID: "vid-0"},
			{Bytes: 720000, Number: 2, ID: "vid-1"},
		}},
	}}
	store := &recordingStore{}
	p := NewParser(newFakeFullPoolManager(fp), stormConfigGetter(4))
	p.SetSegmentStore(func() SegmentStore { return store })

	p.WarmFirstSegments(context.Background(), nzb.Files)

	got, ok := store.get("vid-0")
	if !ok {
		t.Fatal("warm-up did not put vid-0 into the segment store")
	}
	if !bytes.Equal(got, article) {
		t.Fatalf("stored %d bytes, want the whole %d-byte article (not the clipped head)", len(got), len(article))
	}
	if _, ok := store.get("vid-1"); ok {
		t.Fatal("warm-up stored vid-1, which it never fetched")
	}
}

func TestWarmFirstSegmentsWithoutStoreStillWarmsHeads(t *testing.T) {
	fp := fakepool.New()
	fp.SetBehavior("vid-0", fakepool.SegmentBehavior{Bytes: []byte("x"), YEnc: nntppool.YEncMeta{FileSize: 1, PartSize: 1}})
	nzb := &nzbparser.Nzb{Files: nzbparser.NzbFiles{
		{Filename: "Real.Movie.2024.mkv", Segments: nzbparser.NzbSegments{{Bytes: 10, Number: 1, ID: "vid-0"}}},
	}}
	p := NewParser(newFakeFullPoolManager(fp), stormConfigGetter(4))
	p.SetSegmentStore(func() SegmentStore { return nil })

	p.WarmFirstSegments(context.Background(), nzb.Files)
	if got := fp.PerMessageCalls("vid-0"); got != 1 {
		t.Fatalf("warm-up fetched vid-0 %d times, want 1", got)
	}
}
