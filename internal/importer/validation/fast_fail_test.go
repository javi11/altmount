package validation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
)

type fastFailPoolManager struct {
	client *fakepool.Client
}

func (m fastFailPoolManager) GetPool() (pool.NntpClient, error) { return m.client, nil }
func (m fastFailPoolManager) HasPool() bool                     { return m.client != nil }
func (m fastFailPoolManager) IncArticlesDownloaded()            {}
func (m fastFailPoolManager) IncArticlesPosted()                {}
func (m fastFailPoolManager) UpdateDownloadProgress(string, int64) {
}
func (m fastFailPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m fastFailPoolManager) ResetMetrics(context.Context, bool, bool) error { return nil }
func (m fastFailPoolManager) ResetProviderErrors(context.Context) error      { return nil }
func (m fastFailPoolManager) SetProviders([]nntppool.Provider) error         { return nil }
func (m fastFailPoolManager) ClearPool() error                               { return nil }
func (m fastFailPoolManager) AddProvider(nntppool.Provider) error            { return nil }
func (m fastFailPoolManager) RemoveProvider(string) error                    { return nil }
func (m fastFailPoolManager) ResetProviderQuota(context.Context, string) error {
	return nil
}
func (m fastFailPoolManager) SetProviderIDs(map[string]string) {}
func (m fastFailPoolManager) AcquireImportSlot(context.Context) (func(), error) {
	return func() {}, nil
}
func (m fastFailPoolManager) SetAdmissionCaps(int, int)                 {}
func (m fastFailPoolManager) SetStreamSource(pool.StreamActivitySource) {}
func (m fastFailPoolManager) NotifyStreamChange()                       {}

func TestFastFailSegmentCheckUsesSegmentSamplePercentageForEligibleFiles(t *testing.T) {
	client := fakepool.New()
	files := []FastFailFile{
		{
			Filename: "movie.mkv",
			Segments: makeTestSegments("video", 100),
		},
		{
			Filename: "book.pdf",
			Segments: []*metapb.SegmentData{
				{Id: "pdf-0"},
			},
		},
	}

	err := FastFailSegmentCheck(
		context.Background(),
		files,
		fastFailPoolManager{client: client},
		true,
		10,
		1,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("FastFailSegmentCheck returned error: %v", err)
	}

	if got := client.StatCalls(); got != 10 {
		t.Fatalf("StatCalls = %d, want 10", got)
	}
	if got := client.PerMessageCalls("pdf-0"); got != 0 {
		t.Errorf("PerMessageCalls(pdf-0) = %d, want 0", got)
	}
}

func TestFastFailSegmentCheckFailsOnUnreachableSelectedSegment(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("rar-2", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{
			Filename: "release.part01.rar",
			Segments: []*metapb.SegmentData{
				{Id: "rar-0"},
				{Id: "rar-1"},
				{Id: "rar-2"},
			},
		},
	}

	err := FastFailSegmentCheck(
		context.Background(),
		files,
		fastFailPoolManager{client: client},
		true,
		100,
		1,
		100*time.Millisecond,
	)
	if !errors.Is(err, nntppool.ErrArticleNotFound) {
		t.Fatalf("FastFailSegmentCheck error = %v, want ErrArticleNotFound", err)
	}
}

func TestFastFailSegmentCheckDisabledWhenToggleIsFalse(t *testing.T) {
	client := fakepool.New()

	err := FastFailSegmentCheck(
		context.Background(),
		[]FastFailFile{
			{Filename: "movie.mp4", Segments: []*metapb.SegmentData{{Id: "video-0"}}},
		},
		fastFailPoolManager{client: client},
		false,
		100,
		1,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("FastFailSegmentCheck returned error: %v", err)
	}
	if got := client.StatCalls(); got != 0 {
		t.Fatalf("StatCalls = %d, want 0", got)
	}
}

func makeTestSegments(prefix string, count int) []*metapb.SegmentData {
	segments := make([]*metapb.SegmentData, count)
	for i := range count {
		segments[i] = &metapb.SegmentData{Id: fmt.Sprintf("%s-%d", prefix, i)}
	}
	return segments
}
