# Streaming Benchmark and Connection Hygiene Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the streaming benchmark and regression gate (spec phase 0) and land the first stacked improvement, connection hygiene (spec phase 1), proving the gate works.

**Architecture:** A reusable harness package `internal/streambench` (result schema, percentiles, JSON, compare) plus a `go test -bench` file inside `internal/nzbfilesystem` that drives the real `MetadataVirtualFile` → `UsenetReader` → `pool.Manager` → `nntppool` stack against `internal/testsupport/nntpserver`. A `cmd/compare` binary diffs two result files and fails on regression. Phase 1 then changes only `config.ToNNTPProvider` defaults.

**Tech Stack:** Go 1.24+, `testing.B`, `internal/testsupport/nntpserver`, `internal/pool`, `github.com/javi11/nntppool/v4`.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md`

## Global Constraints

- Never mention competitor projects in code, comments, commits, or PR text.
- Conventional Commits on every commit; branch names `<type>/<kebab>`.
- No phase may reduce throughput or raise TTFB/seek p50 by more than 5 % versus the phase below it (`make bench-compare`).
- Phase 0 lands on branch `feat/streaming-bench` (already exists, holds the spec). Phase 1 lands on `feat/streaming-conn-hygiene` branched from `feat/streaming-bench`.
- Run `make` (full build and checks) before every PR.

---

## File structure

| Path | Responsibility |
|---|---|
| `internal/streambench/result.go` | `Result`, `Scenario`, `Metric` types; `Save`/`Load` JSON |
| `internal/streambench/percentile.go` | `Samples` collector with `P(q)`, `Mean`, `Count` |
| `internal/streambench/compare.go` | `Compare(base, new) []Delta` and regression rule |
| `internal/streambench/cmd/compare/main.go` | CLI wrapper: `compare base.json new.json [-threshold 0.05]` |
| `internal/nzbfilesystem/stream_bench_test.go` | `BenchmarkStream*` scenarios B1-B8 against nntpserver |
| `internal/nzbfilesystem/stream_bench_harness_test.go` | harness: server(s) + real `pool.Manager` + MVF builder |
| `Makefile` | `bench-stream`, `bench-compare` targets |
| `bench/results/.gitkeep` + `bench/README.md` | result storage and how to run |
| `internal/config/manager.go:1400-1435` | phase 1: TLS session cache, idle timeout, min connections |
| `internal/config/manager_test.go` (new tests) | phase 1 assertions |

---

### Task 1: Result schema and percentile collector

**Files:**
- Create: `internal/streambench/result.go`
- Create: `internal/streambench/percentile.go`
- Test: `internal/streambench/result_test.go`

**Interfaces:**
- Produces:
  ```go
  type Metric struct { Name string; Unit string; Value float64; HigherIsBetter bool }
  type Scenario struct { Name string; Metrics []Metric }
  type Result struct { GitSHA string; Timestamp time.Time; Profile string; Scenarios []Scenario }
  func (r *Result) Add(scenario string, m ...Metric)
  func Save(path string, r *Result) error
  func Load(path string) (*Result, error)
  type Samples struct{ /* mutex + []time.Duration */ }
  func (s *Samples) Add(d time.Duration)
  func (s *Samples) P(q float64) time.Duration   // q in [0,1]
  func (s *Samples) Count() int
  func (s *Samples) Mean() time.Duration
  ```

- [ ] **Step 1: Write the failing tests**

```go
package streambench

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSamplesPercentiles(t *testing.T) {
	var s Samples
	for i := 1; i <= 100; i++ {
		s.Add(time.Duration(i) * time.Millisecond)
	}
	if got := s.P(0.5); got != 50*time.Millisecond {
		t.Fatalf("p50 = %v, want 50ms", got)
	}
	if got := s.P(0.99); got != 99*time.Millisecond {
		t.Fatalf("p99 = %v, want 99ms", got)
	}
	if got := s.P(1); got != 100*time.Millisecond {
		t.Fatalf("p100 = %v, want 100ms", got)
	}
	if s.Count() != 100 {
		t.Fatalf("count = %d", s.Count())
	}
}

func TestSamplesEmpty(t *testing.T) {
	var s Samples
	if s.P(0.5) != 0 || s.Mean() != 0 {
		t.Fatal("empty samples must report zero")
	}
}

func TestResultRoundTrip(t *testing.T) {
	r := &Result{GitSHA: "abc123", Profile: "premium-750k"}
	r.Add("B1", Metric{Name: "ttfb_p50", Unit: "ms", Value: 12.5})
	r.Add("B1", Metric{Name: "articles", Unit: "count", Value: 3})
	r.Add("B2", Metric{Name: "throughput", Unit: "MB/s", Value: 40, HigherIsBetter: true})

	path := filepath.Join(t.TempDir(), "r.json")
	if err := Save(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Scenarios) != 2 || len(got.Scenarios[0].Metrics) != 2 {
		t.Fatalf("unexpected shape: %+v", got.Scenarios)
	}
	if got.Scenarios[1].Metrics[0].HigherIsBetter != true {
		t.Fatal("HigherIsBetter lost in round trip")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/streambench/ -run 'TestSamples|TestResult' -v`
Expected: FAIL, package does not exist / undefined types.

- [ ] **Step 3: Implement**

`internal/streambench/percentile.go`:
```go
// Package streambench holds the result schema, statistics helpers and the
// regression comparison used by the streaming benchmarks in
// internal/nzbfilesystem and by cmd/compare.
package streambench

import (
	"sort"
	"sync"
	"time"
)

// Samples collects durations from concurrent goroutines and answers
// percentile queries over the sorted set.
type Samples struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *Samples) Add(d time.Duration) {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
}

// P returns the q-quantile (q in [0,1]) using nearest-rank on the sorted
// samples. Zero when there are no samples.
func (s *Samples) P(q float64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.d)
	if n == 0 {
		return 0
	}
	sort.Slice(s.d, func(i, j int) bool { return s.d[i] < s.d[j] })
	if q <= 0 {
		return s.d[0]
	}
	if q >= 1 {
		return s.d[n-1]
	}
	idx := int(q*float64(n)+0.5) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return s.d[idx]
}

func (s *Samples) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.d)
}

func (s *Samples) Mean() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.d) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range s.d {
		sum += d
	}
	return sum / time.Duration(len(s.d))
}
```

`internal/streambench/result.go`:
```go
package streambench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Metric is one measured value. HigherIsBetter drives the regression rule:
// throughput is better when higher, latency and byte waste when lower.
type Metric struct {
	Name           string  `json:"name"`
	Unit           string  `json:"unit"`
	Value          float64 `json:"value"`
	HigherIsBetter bool    `json:"higher_is_better"`
}

type Scenario struct {
	Name    string   `json:"name"`
	Metrics []Metric `json:"metrics"`
}

type Result struct {
	GitSHA    string     `json:"git_sha"`
	Timestamp time.Time  `json:"timestamp"`
	Profile   string     `json:"profile"`
	Scenarios []Scenario `json:"scenarios"`
}

// Add appends metrics to the named scenario, creating it on first use so
// benchmarks can report in any order.
func (r *Result) Add(scenario string, m ...Metric) {
	for i := range r.Scenarios {
		if r.Scenarios[i].Name == scenario {
			r.Scenarios[i].Metrics = append(r.Scenarios[i].Metrics, m...)
			return
		}
	}
	r.Scenarios = append(r.Scenarios, Scenario{Name: scenario, Metrics: m})
}

func Save(path string, r *Result) error {
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Load(path string) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/streambench/ -run 'TestSamples|TestResult' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/streambench/
git commit -m "feat(streambench): result schema and percentile collector"
```

---

### Task 2: Compare with regression rule and CLI

**Files:**
- Create: `internal/streambench/compare.go`
- Create: `internal/streambench/cmd/compare/main.go`
- Test: `internal/streambench/compare_test.go`

**Interfaces:**
- Consumes: `Result`, `Metric` from Task 1.
- Produces:
  ```go
  type Delta struct { Scenario, Metric, Unit string; Base, New, Pct float64; HigherIsBetter bool; Regressed bool }
  func Compare(base, new *Result, threshold float64) []Delta
  func AnyRegressed(d []Delta) bool
  func FormatTable(d []Delta) string
  ```
  `Pct` is `(new-base)/base`. `Regressed` is true when the change is worse than `threshold` in the metric's bad direction. Metrics present in only one result are reported with `Pct = 0` and never regress.

- [ ] **Step 1: Write the failing tests**

```go
package streambench

import (
	"strings"
	"testing"
)

func twoResults() (*Result, *Result) {
	base := &Result{}
	base.Add("B2", Metric{Name: "throughput", Unit: "MB/s", Value: 100, HigherIsBetter: true})
	base.Add("B1", Metric{Name: "ttfb_p50", Unit: "ms", Value: 100})
	base.Add("B3", Metric{Name: "articles", Unit: "count", Value: 60})
	nw := &Result{}
	nw.Add("B2", Metric{Name: "throughput", Unit: "MB/s", Value: 90, HigherIsBetter: true}) // -10%: regression
	nw.Add("B1", Metric{Name: "ttfb_p50", Unit: "ms", Value: 104})                          // +4%: within threshold
	nw.Add("B3", Metric{Name: "articles", Unit: "count", Value: 3})                         // -95%: improvement
	nw.Add("B9", Metric{Name: "new_metric", Unit: "x", Value: 1})
	return base, nw
}

func TestCompareFlagsRegressions(t *testing.T) {
	base, nw := twoResults()
	deltas := Compare(base, nw, 0.05)
	byKey := map[string]Delta{}
	for _, d := range deltas {
		byKey[d.Scenario+"/"+d.Metric] = d
	}
	if !byKey["B2/throughput"].Regressed {
		t.Fatal("10% throughput loss must regress")
	}
	if byKey["B1/ttfb_p50"].Regressed {
		t.Fatal("4% TTFB increase is within a 5% threshold")
	}
	if byKey["B3/articles"].Regressed {
		t.Fatal("fewer articles is an improvement")
	}
	if byKey["B9/new_metric"].Regressed {
		t.Fatal("a metric only in the new result cannot regress")
	}
	if !AnyRegressed(deltas) {
		t.Fatal("AnyRegressed must be true")
	}
}

func TestFormatTableMarksRegressions(t *testing.T) {
	base, nw := twoResults()
	out := FormatTable(Compare(base, nw, 0.05))
	if !strings.Contains(out, "B2") || !strings.Contains(out, "REGRESSION") {
		t.Fatalf("table missing regression marker:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/streambench/ -run 'TestCompare|TestFormat' -v`
Expected: FAIL, undefined `Compare`.

- [ ] **Step 3: Implement**

`internal/streambench/compare.go`:
```go
package streambench

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

type Delta struct {
	Scenario       string
	Metric         string
	Unit           string
	Base, New      float64
	Pct            float64
	HigherIsBetter bool
	Regressed      bool
}

// Compare pairs metrics by scenario and name. A metric regresses when it
// moves more than threshold (fraction, e.g. 0.05) in its bad direction.
func Compare(base, nw *Result, threshold float64) []Delta {
	type key struct{ s, m string }
	baseIdx := map[key]Metric{}
	for _, sc := range base.Scenarios {
		for _, m := range sc.Metrics {
			baseIdx[key{sc.Name, m.Name}] = m
		}
	}
	var out []Delta
	for _, sc := range nw.Scenarios {
		for _, m := range sc.Metrics {
			d := Delta{Scenario: sc.Name, Metric: m.Name, Unit: m.Unit, New: m.Value, HigherIsBetter: m.HigherIsBetter}
			if b, ok := baseIdx[key{sc.Name, m.Name}]; ok && b.Value != 0 {
				d.Base = b.Value
				d.Pct = (m.Value - b.Value) / b.Value
				if m.HigherIsBetter {
					d.Regressed = d.Pct < -threshold
				} else {
					d.Regressed = d.Pct > threshold
				}
			}
			out = append(out, d)
		}
	}
	return out
}

func AnyRegressed(d []Delta) bool {
	for _, x := range d {
		if x.Regressed {
			return true
		}
	}
	return false
}

func FormatTable(d []Delta) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SCENARIO\tMETRIC\tBASE\tNEW\tDELTA\tUNIT\t")
	for _, x := range d {
		mark := ""
		if x.Regressed {
			mark = "REGRESSION"
		}
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%+.1f%%\t%s\t%s\n",
			x.Scenario, x.Metric, x.Base, x.New, x.Pct*100, x.Unit, mark)
	}
	_ = w.Flush()
	return sb.String()
}
```

`internal/streambench/cmd/compare/main.go`:
```go
// Command compare diffs two streaming benchmark result files and exits 1
// when any metric regressed past the threshold.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/javi11/altmount/internal/streambench"
)

func main() {
	threshold := flag.Float64("threshold", 0.05, "regression threshold as a fraction")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: compare [-threshold 0.05] base.json new.json")
		os.Exit(2)
	}
	base, err := streambench.Load(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "load base:", err)
		os.Exit(2)
	}
	nw, err := streambench.Load(flag.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "load new:", err)
		os.Exit(2)
	}
	deltas := streambench.Compare(base, nw, *threshold)
	fmt.Print(streambench.FormatTable(deltas))
	if streambench.AnyRegressed(deltas) {
		fmt.Fprintln(os.Stderr, "regression detected")
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/streambench/... -v && go build ./internal/streambench/cmd/compare`
Expected: PASS, build OK.

- [ ] **Step 5: Commit**

```bash
git add internal/streambench/
git commit -m "feat(streambench): compare results with regression threshold and CLI"
```

---

### Task 3: Benchmark harness in nzbfilesystem

**Files:**
- Create: `internal/nzbfilesystem/stream_bench_harness_test.go`

**Interfaces:**
- Consumes: `nntpserver.New/Config/Dial/Counters`, `pool.NewManager`, `pool.Manager.SetProviders/GetPool/SetStreamSource/SetImportConnCapacity/SetStreamHeadroom`, `MetadataVirtualFile` fields as used by `newTestMVF` in `streamtest_helpers_test.go`, `noopStreamTracker`, `buildSegmentData`.
- Produces (test-package only):
  ```go
  type benchProfile struct { Name string; RTT, Jitter time.Duration; PerConnBW, AggBW int64; ArticleSize, Conns, Inflight, StatInflight int }
  var profilePremium750K, profileSlow4M benchProfile
  type benchHarness struct { srv []*nntpserver.Server; mgr pool.Manager; client pool.NntpClient; ctx context.Context; cancel func(); profile benchProfile }
  func newBenchHarness(tb testing.TB, p benchProfile, providers ...nntpserver.Config) *benchHarness
  func (h *benchHarness) openFile(tb testing.TB, nSegs, maxPrefetch int, rangeEnd int64) *MetadataVirtualFile
  func (h *benchHarness) bodies() int64       // sum of server BODY counters
  func (h *benchHarness) bytesWritten() int64
  func benchResult() *streambench.Result       // process-wide accumulator
  func flushBenchResult(tb testing.TB)         // writes bench/results/<sha>.json when ALTMOUNT_BENCH_OUT is set
  ```

- [ ] **Step 1: Write the harness**

```go
package nzbfilesystem

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/streambench"
	"github.com/javi11/altmount/internal/testsupport/nntpserver"
	"github.com/javi11/nntppool/v4"
)

// Provider models for the streaming benchmarks. The premium profile matches
// internal/pool/contention_bench_test.go so numbers are comparable across
// the two suites; the slow profile models large parts over a distant link,
// where time-to-first-byte is dominated by the article transfer itself.
type benchProfile struct {
	Name         string
	RTT, Jitter  time.Duration
	PerConnBW    int64
	AggBW        int64
	ArticleSize  int
	Conns        int
	Inflight     int
	StatInflight int
}

var profilePremium750K = benchProfile{
	Name: "premium-750k", RTT: 40 * time.Millisecond, Jitter: 10 * time.Millisecond,
	PerConnBW: 8 << 20, AggBW: 400 << 20, ArticleSize: 750 << 10,
	Conns: 50, Inflight: 10, StatInflight: 100,
}

var profileSlow4M = benchProfile{
	Name: "slow-4m", RTT: 100 * time.Millisecond, Jitter: 10 * time.Millisecond,
	PerConnBW: 3 << 20, AggBW: 60 << 20, ArticleSize: 4 << 20,
	Conns: 20, Inflight: 10, StatInflight: 100,
}

type benchStreams struct{ n int }

func (b *benchStreams) ActiveStreams() int { return b.n }

// noopBenchStats satisfies pool.StatsRepository without a database.
type noopBenchStats struct{}

func (noopBenchStats) UpdateSystemStat(context.Context, string, int64) error          { return nil }
func (noopBenchStats) BatchUpdateSystemStats(context.Context, map[string]int64) error { return nil }
func (noopBenchStats) GetSystemStats(context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (noopBenchStats) AddBytesDownloadedToDailyStat(context.Context, int64) error        { return nil }
func (noopBenchStats) AddProviderBytesToHourlyStat(context.Context, string, int64) error { return nil }
func (noopBenchStats) RecordProviderSpeedTest(context.Context, string, float64) error    { return nil }
func (noopBenchStats) GetProviderHourlyStats(context.Context, int) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (noopBenchStats) ClearProviderHourlyStats(context.Context) error { return nil }
func (noopBenchStats) GetOldestStatDate(context.Context) (time.Time, error) {
	return time.Time{}, nil
}
func (noopBenchStats) GetOldestProviderStatDates(context.Context) (map[string]time.Time, error) {
	return map[string]time.Time{}, nil
}

type benchHarness struct {
	srv     []*nntpserver.Server
	mgr     pool.Manager
	client  pool.NntpClient
	ctx     context.Context
	cancel  context.CancelFunc
	profile benchProfile
	streams *benchStreams
}

// newBenchHarness starts one simulated provider per cfg (defaults to a
// single provider built from the profile), wires a real pool.Manager the
// way cmd/altmount does, and pre-warms every connection so the measurement
// window is not spent dialing.
func newBenchHarness(tb testing.TB, p benchProfile, cfgs ...nntpserver.Config) *benchHarness {
	tb.Helper()
	if len(cfgs) == 0 {
		cfgs = []nntpserver.Config{{}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &benchHarness{ctx: ctx, cancel: cancel, profile: p, streams: &benchStreams{}}

	var providers []nntppool.Provider
	for i, cfg := range cfgs {
		if cfg.RTT == 0 {
			cfg.RTT = p.RTT
		}
		if cfg.Jitter == 0 {
			cfg.Jitter = p.Jitter
		}
		if cfg.BandwidthPerConn == 0 {
			cfg.BandwidthPerConn = p.PerConnBW
		}
		if cfg.AggregateBandwidth == 0 {
			cfg.AggregateBandwidth = p.AggBW
		}
		if cfg.ArticleSize == 0 {
			cfg.ArticleSize = p.ArticleSize
		}
		srv, err := nntpserver.New(cfg)
		if err != nil {
			cancel()
			tb.Fatalf("nntpserver.New: %v", err)
		}
		h.srv = append(h.srv, srv)
		providers = append(providers, nntppool.Provider{
			Host:           "bench-" + string(rune('a'+i)),
			Factory:        srv.Dial,
			Auth:           nntppool.Auth{Username: "bench", Password: "bench"},
			Connections:    p.Conns,
			MinConnections: p.Conns,
			Inflight:       p.Inflight,
			StatInflight:   p.StatInflight,
			SkipPing:       true,
			IdleTimeout:    time.Hour,
		})
	}

	h.mgr = pool.NewManager(ctx, noopBenchStats{})
	if err := h.mgr.SetProviders(providers); err != nil {
		tb.Fatalf("SetProviders: %v", err)
	}
	client, err := h.mgr.GetPool()
	if err != nil {
		tb.Fatalf("GetPool: %v", err)
	}
	h.client = client
	h.mgr.SetStreamSource(h.streams)
	h.mgr.SetImportConnCapacity(p.Conns * len(cfgs))
	h.mgr.SetStreamHeadroom(p.Conns / 4)

	tb.Cleanup(func() {
		_ = h.mgr.ClearPool()
		cancel()
		for _, s := range h.srv {
			_ = s.Close()
		}
	})

	// Warm-up: one body per connection so every slot has dialed.
	var wg sync.WaitGroup
	for i := 0; i < p.Conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.BodyPriority(ctx, "<warm@bench>")
		}()
	}
	wg.Wait()
	for _, s := range h.srv {
		s.ResetPeakInflight()
	}
	return h
}

// openFile builds a plain (unencrypted, non-nested) virtual file of nSegs
// articles wired to the real pool. rangeEnd < 0 means no Range header.
func (h *benchHarness) openFile(tb testing.TB, nSegs, maxPrefetch int, rangeEnd int64) *MetadataVirtualFile {
	tb.Helper()
	segSize := h.profile.ArticleSize
	mvf := &MetadataVirtualFile{
		name: "bench-file",
		meta: &fileHandleMeta{
			FileSize:    int64(nSegs) * int64(segSize),
			SegmentData: buildSegmentData(tb, nSegs, segSize),
		},
		poolManager:      h.mgr,
		ctx:              h.ctx,
		maxPrefetch:      maxPrefetch,
		originalRangeEnd: rangeEnd,
		streamTracker:    noopStreamTracker{},
		streamID:         "bench-stream",
	}
	tb.Cleanup(func() { _ = mvf.Close() })
	return mvf
}

func (h *benchHarness) bodies() int64 {
	var n int64
	for _, s := range h.srv {
		n += s.Counters().Bodies
	}
	return n
}

func (h *benchHarness) bytesWritten() int64 {
	var n int64
	for _, s := range h.srv {
		n += s.Counters().BytesWritten
	}
	return n
}

// Process-wide result accumulator, flushed by the last benchmark via
// TestMain when ALTMOUNT_BENCH_OUT names an output path.
var (
	benchResultMu sync.Mutex
	benchResultV  *streambench.Result
)

func benchResult() *streambench.Result {
	benchResultMu.Lock()
	defer benchResultMu.Unlock()
	if benchResultV == nil {
		benchResultV = &streambench.Result{GitSHA: gitShortSHA()}
	}
	return benchResultV
}

func gitShortSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func flushBenchResult() {
	path := os.Getenv("ALTMOUNT_BENCH_OUT")
	if path == "" {
		return
	}
	benchResultMu.Lock()
	r := benchResultV
	benchResultMu.Unlock()
	if r == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = streambench.Save(path, r)
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func mbps(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) / (1 << 20) / d.Seconds()
}
```

If `internal/nzbfilesystem` already has a `TestMain`, extend it; otherwise add to the same file:

```go
func TestMain(m *testing.M) {
	code := m.Run()
	flushBenchResult()
	os.Exit(code)
}
```

Run `grep -rn "func TestMain" internal/nzbfilesystem/` first to know which.

- [ ] **Step 2: Verify it compiles**

Run: `go vet ./internal/nzbfilesystem/`
Expected: no errors. If `pool.NewManager` signature differs from `NewManager(ctx, statsRepo)`, read `internal/pool/manager.go` and adapt the call; the pool benchmark at `internal/pool/contention_bench_test.go:145` is the reference.

- [ ] **Step 3: Commit**

```bash
git add internal/nzbfilesystem/stream_bench_harness_test.go
git commit -m "test(nzbfilesystem): streaming benchmark harness over the simulated provider"
```

---

### Task 4: Scenarios B1, B2, B3, B8 (single stream)

**Files:**
- Create: `internal/nzbfilesystem/stream_bench_test.go`

**Interfaces:**
- Consumes: Task 3 harness.
- Produces: scenario names and metric names, fixed here and reused by every later phase:
  - `B1-cold-open`: `ttfb_p50` ms, `ttfb_p99` ms, `articles` count
  - `B2-sequential`: `throughput` MB/s (higher better), `waste_ratio` (fetched bytes / read bytes)
  - `B3-seek-storm`: `read_p50` ms, `read_p99` ms, `articles_per_read` count
  - `B8-pause-resume`: `bytes_during_pause` MB
  Each scenario is run for both `profilePremium750K` and `profileSlow4M`; scenario names get a `/<profile>` suffix.

- [ ] **Step 1: Write the benchmarks**

```go
package nzbfilesystem

import (
	"io"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/streambench"
)

const (
	benchFileSegs   = 400 // 400 × 750 KB ≈ 293 MB; 400 × 4 MB ≈ 1.6 GB
	benchPrefetch   = 60  // config default streaming.max_prefetch
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
			// Let any read-ahead the last probe started settle before counting.
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

// BenchmarkStreamPauseResume reads 20 MB, pauses like a player buffer that
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
			// Read-ahead that was already in flight at the pause lands during
			// the first second; sample after it settles so the number reflects
			// what keeps flowing, not what was already committed.
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
```

- [ ] **Step 2: Run the benchmarks once**

Run: `go test ./internal/nzbfilesystem/ -run '^$' -bench 'BenchmarkStream(ColdOpen|Sequential|SeekStorm|PauseResume)' -benchtime 1x -timeout 20m`
Expected: all four complete and print metrics. If `ReadAt` on a fresh handle errors because a collaborator is nil, compare with `newTestMVF` in `streamtest_helpers_test.go` and add the missing field to `openFile`.

- [ ] **Step 3: Commit**

```bash
git add internal/nzbfilesystem/stream_bench_test.go
git commit -m "test(nzbfilesystem): single-stream benchmarks (cold open, sequential, seek storm, pause)"
```

---

### Task 5: Scenarios B4, B5, B6, B7 (concurrency, dedup, contention, failover)

**Files:**
- Modify: `internal/nzbfilesystem/stream_bench_test.go`

**Interfaces:**
- Produces metric names:
  - `B4-four-streams`: `min_stream_mbps` (higher better), `max_stream_mbps`, `stall_p99` ms (per-1 MB-read latency p99 across streams)
  - `B5-two-handles`: `duplicate_bodies` count (server bodies minus unique segments touched)
  - `B6-contention`: `stream_p99` ms, `import_mbps` (higher better)
  - `B7-failover`: `miss_ttfb_p50` ms, `bodies_per_miss` count

- [ ] **Step 1: Append the benchmarks**

```go
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
// number of distinct articles.
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
	time.Sleep(2 * time.Second) // let read-ahead beyond span land
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
	mvf := h.openFile(b, benchFileSegs, 1, -1) // prefetch 1 isolates the miss cost
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
```

Add imports `context`, `math`, `sync`, `sync/atomic`, `github.com/javi11/altmount/internal/testsupport/segments`, `github.com/javi11/altmount/internal/testsupport/nntpserver` to the file.

Note on B7: the nntpserver `Missing` set makes provider A answer 430 for those ids; every other read hits the same provider set, so a hit costs one BODY and a miss costs one 430 plus the STAT race and one BODY. `bodies()` counts served bodies only, so `bodies_per_miss` should be 1.0; the latency is the interesting number.

- [ ] **Step 2: Run all eight**

Run: `go test ./internal/nzbfilesystem/ -run '^$' -bench 'BenchmarkStream' -benchtime 1x -timeout 40m`
Expected: all complete. Total wall time should be under 10 minutes; if B4 or B6 take longer, halve their byte targets and keep the change in the constants block.

- [ ] **Step 3: Commit**

```bash
git add internal/nzbfilesystem/stream_bench_test.go
git commit -m "test(nzbfilesystem): concurrency, dedup, contention and failover benchmarks"
```

---

### Task 6: Makefile targets, results directory, README

**Files:**
- Modify: `Makefile` (after the `test:` target, around line 60)
- Create: `bench/README.md`
- Create: `bench/results/.gitkeep`

- [ ] **Step 1: Add targets**

```makefile
.PHONY: bench-stream bench-compare
BENCH_SHA := $(shell git rev-parse --short HEAD)
BENCH_OUT ?= bench/results/$(BENCH_SHA).json

# Runs the streaming benchmarks against the simulated provider and writes
# bench/results/<sha>.json. Takes several minutes.
bench-stream:
	ALTMOUNT_BENCH_OUT=$(BENCH_OUT) $(GO) test ./internal/nzbfilesystem/ -run '^$$' -bench 'BenchmarkStream' -benchtime 1x -timeout 60m

# Compares the current results against BASE (a short sha with results in
# bench/results/) and fails on regression. Usage: make bench-compare BASE=abc1234
bench-compare:
	@test -n "$(BASE)" || (echo "BASE=<sha> required" && exit 2)
	$(GO) run ./internal/streambench/cmd/compare bench/results/$(BASE).json $(BENCH_OUT)
```

- [ ] **Step 2: Write `bench/README.md`**

```markdown
# Streaming benchmarks

`make bench-stream` runs `BenchmarkStream*` in `internal/nzbfilesystem` against
the in-process provider simulator (`internal/testsupport/nntpserver`) and writes
`bench/results/<short-sha>.json`. `make bench-compare BASE=<sha>` diffs the
current results against a stored baseline and exits non-zero when any metric
regresses by more than 5 % in its bad direction.

Scenarios: B1 cold open TTFB, B2 sequential throughput and waste, B3 seek
storm, B4 four concurrent streams, B5 two handles on one file, B6 stream under
import contention, B7 provider failover, B8 bytes fetched while paused.

Results are committed per phase so a PR's description can cite its delta table.
Run on an idle machine; the simulator is CPU-bound at high aggregate bandwidth.
```

- [ ] **Step 3: Run the baseline and store it**

Run: `make bench-stream && ls bench/results/`
Expected: a JSON file named after the current short sha. Rename it to `bench/results/baseline-main.json` as well (`cp`), so later phases compare against a stable name: `make bench-compare BASE=baseline-main`.

- [ ] **Step 4: Verify compare works against itself**

Run: `make bench-compare BASE=baseline-main`
Expected: table with all deltas 0.0 %, exit 0.

- [ ] **Step 5: Commit**

```bash
git add Makefile bench/
git commit -m "build(bench): bench-stream and bench-compare targets with committed baseline"
```

---

### Task 7: Live A/B script (outside the repo)

**Files:**
- Create: `~/altmount-dev/bench-live.sh` (not tracked by the repo)

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
# Live A/B against the dev server. Usage: bench-live.sh <label> [file ...]
# Reads three files from the WebDAV endpoint; prints TTFB, seek latency and
# sustained throughput. Run once per phase and compare the tables by hand.
set -euo pipefail
LABEL=${1:?label}; shift || true
BASE=${ALTMOUNT_URL:-http://localhost:8080/webdav}
AUTH=${ALTMOUNT_AUTH:-usenet:usenet}
FILES=("$@")
if [ ${#FILES[@]} -eq 0 ]; then
  mapfile -t FILES < "${ALTMOUNT_DEV_ROOT:-$HOME/altmount-dev}/bench-files.txt"
fi
OUT="${ALTMOUNT_DEV_ROOT:-$HOME/altmount-dev}/bench-live-$LABEL.tsv"
printf 'file\tttfb_ms\tseek_p50_ms\tthroughput_MBps\n' > "$OUT"
for f in "${FILES[@]}"; do
  url="$BASE/$f"
  ttfb=$(curl -s -u "$AUTH" -o /dev/null -r 0-1048575 -w '%{time_starttransfer}' "$url")
  size=$(curl -sI -u "$AUTH" "$url" | awk 'tolower($1)=="content-length:"{print $2+0}')
  seeks=()
  for i in 1 2 3 4 5 6 7 8 9 10; do
    off=$(( (RANDOM * 32768 + RANDOM) % (size - 65536) ))
    seeks+=("$(curl -s -u "$AUTH" -o /dev/null -r "$off-$((off+65535))" -w '%{time_starttransfer}' "$url")")
  done
  seek_p50=$(printf '%s\n' "${seeks[@]}" | sort -n | sed -n 5p)
  bytes=$(curl -s -u "$AUTH" -o /dev/null -m 60 -w '%{size_download}' "$url" || true)
  printf '%s\t%.0f\t%.0f\t%.1f\n' "$f" "$(echo "$ttfb*1000" | bc)" "$(echo "$seek_p50*1000" | bc)" \
    "$(echo "$bytes/60/1048576" | bc -l)" >> "$OUT"
done
column -t -s$'\t' "$OUT"
```

Also create `~/altmount-dev/bench-files.txt` with three WebDAV-relative paths from the seeded library (pick one plain mkv, one RAR-sourced mkv, one large remux). List candidates with `ls ~/altmount-dev/library-seed/metadata/complete | head`.

- [ ] **Step 2: Run baseline on main**

Run from a `main` worktree: `~/altmount-dev/run.sh` in one terminal, then `~/altmount-dev/bench-live.sh main`.
Expected: a three-row table; keep `bench-live-main.tsv` for later phases.

- [ ] **Step 3: Push phase 0 and open the first stacked PR**

```bash
make
git push -u origin feat/streaming-bench
gh pr create --base main --title "feat(bench): streaming benchmark suite and regression gate" --body-file - <<'EOF'
## Summary
- design spec for the streaming demand-shaping work (docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md)
- `internal/streambench`: result schema, percentiles, compare CLI with 5 % regression rule
- `BenchmarkStream*` in `internal/nzbfilesystem`: eight scenarios over the real MVF → UsenetReader → pool → simulated provider stack
- `make bench-stream` / `make bench-compare BASE=<sha>`; baseline committed under bench/results/

## Baseline (premium-750k profile)
<paste the B1-B8 rows from bench/results/baseline-main.json>

## Test plan
- [ ] `make` green
- [ ] `make bench-stream` completes under 10 min on an idle machine
- [ ] `make bench-compare BASE=baseline-main` reports 0 % deltas
EOF
```

---

### Task 8: Phase 1, connection hygiene

**Branch:** `git checkout -b feat/streaming-conn-hygiene feat/streaming-bench`

**Files:**
- Modify: `internal/config/manager.go:1400-1435` (`ToNNTPProvider`)
- Test: `internal/config/provider_nntp_test.go` (new)
- Modify: `config.sample.yaml` (document `min_connections_alive` default 2)
- Modify: docs page for providers if one exists under `docs/` or `frontend/src/...` config reference (grep `min_connections_alive`)

**Interfaces:**
- Consumes: `config.ProviderConfig` fields `MaxConnections`, `MinConnectionsAlive`, `InsecureTLS`, `TLS`, `Host`.
- Produces: unchanged signature `func (p *ProviderConfig) ToNNTPProvider() nntppool.Provider` (verify receiver name/signature by reading lines 1380-1400 first).

- [ ] **Step 1: Write the failing tests**

```go
package config

import (
	"testing"
	"time"
)

func tlsProvider() ProviderConfig {
	return ProviderConfig{Host: "news.example", Port: 563, TLS: true, MaxConnections: 20}
}

func TestToNNTPProviderEnablesTLSSessionResumption(t *testing.T) {
	p := tlsProvider()
	np := p.ToNNTPProvider()
	if np.TLSConfig == nil || np.TLSConfig.ClientSessionCache == nil {
		t.Fatal("TLS providers must carry a session cache so reconnects resume instead of re-handshaking")
	}
}

func TestToNNTPProviderIdleTimeoutIsTwoMinutes(t *testing.T) {
	p := tlsProvider()
	if got := p.ToNNTPProvider().IdleTimeout; got != 2*time.Minute {
		t.Fatalf("IdleTimeout = %v, want 2m", got)
	}
}

func TestToNNTPProviderDefaultsMinConnectionsToTwo(t *testing.T) {
	p := tlsProvider()
	if got := p.ToNNTPProvider().MinConnections; got != 2 {
		t.Fatalf("MinConnections = %d, want 2 when unset", got)
	}
	p.MinConnectionsAlive = 5
	if got := p.ToNNTPProvider().MinConnections; got != 5 {
		t.Fatalf("explicit MinConnectionsAlive must win, got %d", got)
	}
	p.MinConnectionsAlive = 0
	p.MaxConnections = 1
	if got := p.ToNNTPProvider().MinConnections; got != 1 {
		t.Fatalf("default must be capped at MaxConnections, got %d", got)
	}
}

func TestToNNTPProviderSetsReconnectDelay(t *testing.T) {
	p := tlsProvider()
	if got := p.ToNNTPProvider().ReconnectDelay; got != 30*time.Second {
		t.Fatalf("ReconnectDelay = %v, want 30s so a provider removed on 502 comes back", got)
	}
}
```

Adjust struct field names (`Port`, `TLS`, `MaxConnections`, `MinConnectionsAlive`) to the real `ProviderConfig` after reading its definition (`grep -n "type ProviderConfig struct" -A40 internal/config/manager.go`). Confirm `nntppool.Provider.ReconnectDelay` exists at `~/mio/nntppool/nntp.go` (`grep -n ReconnectDelay`); it does in v4.20.x.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestToNNTPProvider -v`
Expected: FAIL on all four.

- [ ] **Step 3: Implement**

In `ToNNTPProvider`, where `tlsCfg` is built (around line 1400):

```go
	var tlsCfg *tls.Config
	if p.TLS {
		tlsCfg = &tls.Config{
			InsecureSkipVerify: p.InsecureTLS,
			ServerName:         host,
			// One cached session per connection the allowance permits, so
			// every reconnect after the first is a resumption handshake.
			ClientSessionCache: tls.NewLRUClientSessionCache(max(p.MaxConnections, 1)),
		}
	}
```

Keep whatever the existing code does for `host` and `InsecureSkipVerify`; only add the `ClientSessionCache` line.

Then:

```go
	minConns := p.MinConnectionsAlive
	if minConns <= 0 {
		minConns = defaultMinConnectionsAlive
	}
	if minConns > p.MaxConnections {
		minConns = p.MaxConnections
	}
```

and in the returned struct:

```go
		MinConnections: minConns,
		IdleTimeout:    providerIdleTimeout,
		ReconnectDelay: providerReconnectDelay,
```

Constants near the top of the file (or next to the other provider defaults):

```go
const (
	// defaultMinConnectionsAlive keeps a couple of sockets warm so a stream
	// started after an idle period does not pay TCP + TLS + AUTHINFO first.
	defaultMinConnectionsAlive = 2
	// providerIdleTimeout stays under the ~3 minute idle cut most providers
	// apply, while keeping warm connections through short pauses.
	providerIdleTimeout = 2 * time.Minute
	// providerReconnectDelay lets a provider dropped after a 502 rejoin the
	// pool instead of staying out until restart.
	providerReconnectDelay = 30 * time.Second
)
```

- [ ] **Step 4: Run tests and the package**

Run: `go test ./internal/config/ -v -run TestToNNTPProvider && go test ./internal/config/ ./internal/pool/`
Expected: PASS.

- [ ] **Step 5: Update config docs**

In `config.sample.yaml` under a provider entry, set or document `min_connections_alive: 2  # default when omitted; warm sockets for fast first byte`. Grep `min_connections_alive` under `docs/` and `frontend/src` and update any stated default.

- [ ] **Step 6: Benchmark and compare**

Run: `make bench-stream && make bench-compare BASE=baseline-main`
Expected: no regressions. Simulated B1 is unchanged by design (the harness pre-warms every slot and uses a plain factory dial, so TLS resumption is not exercised there); the live A/B is the evidence for this phase: `~/altmount-dev/bench-live.sh conn-hygiene` after letting the server sit idle for 3 minutes, versus `bench-live-main.tsv` measured the same way.

- [ ] **Step 7: Commit, push, open stacked PR**

```bash
make
git add internal/config/ config.sample.yaml bench/results/ docs/
git commit -m "feat(pool): TLS session resumption, warm connections and reconnect for providers"
git push -u origin feat/streaming-conn-hygiene
gh pr create --base feat/streaming-bench --title "feat(pool): TLS session resumption, warm connections and reconnect for providers" --body-file - <<'EOF'
## Summary
- TLS providers get a `ClientSessionCache` sized to `max_connections`, so reconnects after the idle cut resume instead of full handshakes
- provider idle timeout 60 s → 120 s
- `min_connections_alive` defaults to 2 (capped at `max_connections`) when unset
- `ReconnectDelay` 30 s so a provider removed after a 502 rejoins the pool

## Benchmark
`make bench-compare BASE=baseline-main`: <paste table; expect 0 % on simulated profile>
Live A/B after 3 min idle: <paste bench-live-main.tsv vs bench-live-conn-hygiene.tsv>

## Test plan
- [ ] `make` green
- [ ] new `TestToNNTPProvider*` tests
- [ ] live TTFB after idle drops by roughly one TLS handshake RTT
EOF
```

---

## Self-review

- **Spec coverage:** Phase 0 scenarios B1-B8 → Tasks 4-5; JSON + compare + Makefile → Tasks 1, 2, 6; live A/B → Task 7. Phase 1 (TLS cache, idle 120 s, min connections 2) → Task 8, which also picks up the `ReconnectDelay` fix from phase 9 because it lives in the same function and is a hygiene item; phase 9's plan must not redo it.
- **Placeholders:** none; the two `<paste ...>` markers are PR-body fill-ins for measured numbers, not code.
- **Type consistency:** `streambench.Samples.P`, `Result.Add`, `Metric{Name,Unit,Value,HigherIsBetter}` used identically in Tasks 1-5; `benchHarness.openFile(tb, nSegs, maxPrefetch, rangeEnd)` used identically in Tasks 4-5; `scenario(name, profile)` naming fixed in Task 4 and reused in Task 5.
