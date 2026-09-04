package nzbfilesystem

import (
	"context"
	"io"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/streambench"
	"github.com/javi11/altmount/internal/testsupport/nntpserver"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// Streaming benchmarks over the real MetadataVirtualFile → UsenetReader →
// pool.Manager → nntppool stack against the simulated provider. Run with
// `make bench-stream`; results land in bench/results/<sha>.json and are
// compared by `make bench-compare BASE=<sha>`.
const (
	benchFileSegs   = 400
	benchPrefetch   = 60 // config default streaming.max_prefetch
	benchSeqBytes   = 200 << 20
	benchSeekReads  = 20
	benchSeekSize   = 64 << 10
	benchColdOpens  = 10
	benchPauseSleep = 5 * time.Second
)

var benchProfiles = []benchProfile{profilePremium750K, profileSlow4M}

func scenario(name string, p benchProfile) string { return name + "/" + p.Name }

// BenchmarkStreamColdOpen measures time to first byte on a fresh handle
// reading the first 1 MB, the way a player opens a file.
func BenchmarkStreamColdOpen(b *testing.B) {
	for _, p := range benchProfiles {
		b.Run(p.Name, func(b *testing.B) {
			h := newBenchHarness(b, p)
			var ttfb streambench.Samples
			buf := make([]byte, 1<<20)
			before := h.bodies()
			for i := 0; i < benchColdOpens; i++ {
				mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
				start := time.Now()
				n, err := mvf.ReadAt(buf[:1], 0)
				if err != nil || n != 1 {
					b.Fatalf("first byte: n=%d err=%v", n, err)
				}
				ttfb.Add(time.Since(start))
				if _, err := io.ReadFull(io.NewSectionReader(mvf, 1, int64(len(buf)-1)), buf[1:]); err != nil {
					b.Fatalf("first MB: %v", err)
				}
				_ = mvf.Close()
			}
			// Read-ahead started by the last open lands after Close returns.
			time.Sleep(time.Second)
			articles := float64(h.bodies()-before) / benchColdOpens
			benchResult().Add(scenario("B1-cold-open", p),
				streambench.Metric{Name: "ttfb_p50", Unit: "ms", Value: ms(ttfb.P(0.5))},
				streambench.Metric{Name: "ttfb_p99", Unit: "ms", Value: ms(ttfb.P(0.99))},
				streambench.Metric{Name: "articles", Unit: "count", Value: articles},
			)
			b.ReportMetric(ms(ttfb.P(0.5)), "ttfb_p50_ms")
			b.ReportMetric(articles, "articles/open")
		})
	}
}

// BenchmarkStreamSequential reads 200 MB sequentially through ReadAt in
// 1 MB chunks and reports throughput and how many bytes the provider sent
// per byte the reader consumed (1.0 is no waste).
func BenchmarkStreamSequential(b *testing.B) {
	for _, p := range benchProfiles {
		b.Run(p.Name, func(b *testing.B) {
			h := newBenchHarness(b, p)
			mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
			buf := make([]byte, 1<<20)
			beforeBytes := h.bytesWritten()
			start := time.Now()
			var read int64
			for read < benchSeqBytes {
				n, err := mvf.ReadAt(buf, read)
				if err != nil && err != io.EOF {
					b.Fatalf("ReadAt(%d): %v", read, err)
				}
				read += int64(n)
				if err == io.EOF {
					break
				}
			}
			elapsed := time.Since(start)
			_ = mvf.Close()
			fetched := h.bytesWritten() - beforeBytes
			waste := float64(fetched) / float64(read)
			benchResult().Add(scenario("B2-sequential", p),
				streambench.Metric{Name: "throughput", Unit: "MB/s", Value: mbps(read, elapsed), HigherIsBetter: true},
				streambench.Metric{Name: "waste_ratio", Unit: "ratio", Value: waste},
			)
			b.ReportMetric(mbps(read, elapsed), "MB/s")
			b.ReportMetric(waste, "waste")
		})
	}
}

// BenchmarkStreamSeekStorm models a media server probing a file: 20 random
// 64 KB reads, each far from the last. The interesting number is how many
// articles each probe drags in beyond the one it needs.
func BenchmarkStreamSeekStorm(b *testing.B) {
	for _, p := range benchProfiles {
		b.Run(p.Name, func(b *testing.B) {
			h := newBenchHarness(b, p)
			mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
			fileSize := int64(benchFileSegs) * int64(p.ArticleSize)
			rng := rand.New(rand.NewPCG(1, 2))
			buf := make([]byte, benchSeekSize)
			var lat streambench.Samples
			before := h.bodies()
			for i := 0; i < benchSeekReads; i++ {
				off := rng.Int64N(fileSize - benchSeekSize)
				start := time.Now()
				if _, err := mvf.ReadAt(buf, off); err != nil {
					b.Fatalf("ReadAt(%d): %v", off, err)
				}
				lat.Add(time.Since(start))
			}
			time.Sleep(2 * time.Second)
			perRead := float64(h.bodies()-before) / benchSeekReads
			benchResult().Add(scenario("B3-seek-storm", p),
				streambench.Metric{Name: "read_p50", Unit: "ms", Value: ms(lat.P(0.5))},
				streambench.Metric{Name: "read_p99", Unit: "ms", Value: ms(lat.P(0.99))},
				streambench.Metric{Name: "articles_per_read", Unit: "count", Value: perRead},
			)
			b.ReportMetric(ms(lat.P(0.5)), "read_p50_ms")
			b.ReportMetric(perRead, "articles/read")
		})
	}
}

// BenchmarkStreamPauseResume reads 20 MB, pauses like a player whose buffer
// has filled, and reports how much the provider sent during the pause.
func BenchmarkStreamPauseResume(b *testing.B) {
	for _, p := range benchProfiles {
		b.Run(p.Name, func(b *testing.B) {
			h := newBenchHarness(b, p)
			mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
			buf := make([]byte, 1<<20)
			var off int64
			for off < 20<<20 {
				n, err := mvf.ReadAt(buf, off)
				if err != nil {
					b.Fatalf("ReadAt: %v", err)
				}
				off += int64(n)
			}
			// Read-ahead already committed at the pause lands during the first
			// second; sample after it settles so the number reflects what keeps
			// flowing rather than what was in flight.
			time.Sleep(time.Second)
			before := h.bytesWritten()
			time.Sleep(benchPauseSleep)
			during := h.bytesWritten() - before
			benchResult().Add(scenario("B8-pause-resume", p),
				streambench.Metric{Name: "bytes_during_pause", Unit: "MB", Value: float64(during) / (1 << 20)},
			)
			b.ReportMetric(float64(during)/(1<<20), "MB_during_pause")
		})
	}
}

// BenchmarkStreamFourConcurrent runs four players on one 50-connection
// pool and reports the fairness spread and tail stall.
func BenchmarkStreamFourConcurrent(b *testing.B) {
	p := profilePremium750K
	h := newBenchHarness(b, p)
	const streams = 4
	h.streams.n = streams
	var wg sync.WaitGroup
	var stalls streambench.Samples
	rates := make([]float64, streams)
	for s := 0; s < streams; s++ {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
			buf := make([]byte, 1<<20)
			start := time.Now()
			var off int64
			for off < 100<<20 {
				t0 := time.Now()
				n, err := mvf.ReadAt(buf, off)
				if err != nil {
					b.Errorf("stream %d ReadAt: %v", s, err)
					return
				}
				stalls.Add(time.Since(t0))
				off += int64(n)
			}
			rates[s] = mbps(off, time.Since(start))
		}(s)
	}
	wg.Wait()
	minR, maxR := rates[0], rates[0]
	for _, r := range rates[1:] {
		minR = math.Min(minR, r)
		maxR = math.Max(maxR, r)
	}
	benchResult().Add(scenario("B4-four-streams", p),
		streambench.Metric{Name: "min_stream_mbps", Unit: "MB/s", Value: minR, HigherIsBetter: true},
		streambench.Metric{Name: "max_stream_mbps", Unit: "MB/s", Value: maxR, HigherIsBetter: true},
		streambench.Metric{Name: "stall_p99", Unit: "ms", Value: ms(stalls.P(0.99))},
	)
	b.ReportMetric(minR, "min_MB/s")
	b.ReportMetric(ms(stalls.P(0.99)), "stall_p99_ms")
}

// BenchmarkStreamTwoHandles reads the same 50 MB through two handles in
// lockstep and counts how many BODY commands the provider saw beyond the
// number of distinct articles plus one read-ahead window.
func BenchmarkStreamTwoHandles(b *testing.B) {
	p := profilePremium750K
	h := newBenchHarness(b, p)
	const span = 50 << 20
	unique := (span + p.ArticleSize - 1) / p.ArticleSize
	before := h.bodies()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
			buf := make([]byte, 1<<20)
			var off int64
			for off < span {
				n, err := mvf.ReadAt(buf, off)
				if err != nil {
					b.Errorf("ReadAt: %v", err)
					return
				}
				off += int64(n)
			}
		}()
	}
	wg.Wait()
	time.Sleep(2 * time.Second)
	dup := float64(h.bodies()-before) - float64(unique+benchPrefetch)
	if dup < 0 {
		dup = 0
	}
	benchResult().Add(scenario("B5-two-handles", p),
		streambench.Metric{Name: "duplicate_bodies", Unit: "count", Value: dup},
	)
	b.ReportMetric(dup, "dup_bodies")
}

// BenchmarkStreamUnderContention plays one stream while an importer
// saturates the normal lane with Body calls, the shape of a library scan
// during playback.
func BenchmarkStreamUnderContention(b *testing.B) {
	p := profilePremium750K
	h := newBenchHarness(b, p)
	h.streams.n = 1
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()

	var importBytes atomic.Int64
	var iwg sync.WaitGroup
	for w := 0; w < 160; w++ {
		iwg.Add(1)
		go func(w int) {
			defer iwg.Done()
			i := w
			for ctx.Err() == nil {
				release, err := h.mgr.AcquireImportConnection(ctx)
				if err != nil {
					return
				}
				body, err := h.client.Body(ctx, segments.MessageID(100000+i))
				release()
				if err == nil {
					importBytes.Add(int64(len(body.Bytes)))
				}
				i += 160
			}
		}(w)
	}

	mvf := h.openFile(b, benchFileSegs, benchPrefetch, -1)
	buf := make([]byte, 1<<20)
	var lat streambench.Samples
	start := time.Now()
	var off int64
	for off < 100<<20 {
		t0 := time.Now()
		n, err := mvf.ReadAt(buf, off)
		if err != nil {
			b.Fatalf("ReadAt: %v", err)
		}
		lat.Add(time.Since(t0))
		off += int64(n)
	}
	elapsed := time.Since(start)
	cancel()
	iwg.Wait()
	benchResult().Add(scenario("B6-contention", p),
		streambench.Metric{Name: "stream_p99", Unit: "ms", Value: ms(lat.P(0.99))},
		streambench.Metric{Name: "import_mbps", Unit: "MB/s", Value: mbps(importBytes.Load(), elapsed), HigherIsBetter: true},
	)
	b.ReportMetric(ms(lat.P(0.99)), "stream_p99_ms")
	b.ReportMetric(mbps(importBytes.Load(), elapsed), "import_MB/s")
}

// BenchmarkStreamFailover has provider A missing every tenth article and
// provider B complete; it reports the first-byte cost of a miss.
func BenchmarkStreamFailover(b *testing.B) {
	p := profilePremium750K
	missing := map[string]struct{}{}
	for i := 0; i < benchFileSegs; i += 10 {
		missing[segments.MessageID(i)] = struct{}{}
	}
	h := newBenchHarness(b, p, nntpserver.Config{Missing: missing}, nntpserver.Config{})
	mvf := h.openFile(b, benchFileSegs, 1, -1)
	buf := make([]byte, 1)
	var missLat streambench.Samples
	before := h.bodies()
	for i := 0; i < benchFileSegs; i += 10 {
		off := int64(i) * int64(p.ArticleSize)
		start := time.Now()
		if _, err := mvf.ReadAt(buf, off); err != nil {
			b.Fatalf("ReadAt(%d): %v", off, err)
		}
		missLat.Add(time.Since(start))
	}
	perMiss := float64(h.bodies()-before) / float64(missLat.Count())
	benchResult().Add(scenario("B7-failover", p),
		streambench.Metric{Name: "miss_ttfb_p50", Unit: "ms", Value: ms(missLat.P(0.5))},
		streambench.Metric{Name: "bodies_per_miss", Unit: "count", Value: perMiss},
	)
	b.ReportMetric(ms(missLat.P(0.5)), "miss_ttfb_p50_ms")
}
