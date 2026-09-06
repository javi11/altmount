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
	nw.Add("B2", Metric{Name: "throughput", Unit: "MB/s", Value: 90, HigherIsBetter: true})
	nw.Add("B1", Metric{Name: "ttfb_p50", Unit: "ms", Value: 104})
	nw.Add("B3", Metric{Name: "articles", Unit: "count", Value: 3})
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

func TestMedianAndInformational(t *testing.T) {
	runs := [][]Metric{
		{{Name: "a", Value: 3}, {Name: "p99", Value: 100, Informational: true}},
		{{Name: "a", Value: 1}, {Name: "p99", Value: 300, Informational: true}},
		{{Name: "a", Value: 2}, {Name: "p99", Value: 200, Informational: true}},
	}
	med := Median(runs)
	if med[0].Value != 2 || med[1].Value != 200 {
		t.Fatalf("median = %+v", med)
	}
	base, nw := &Result{}, &Result{}
	base.Add("S", Metric{Name: "p99", Value: 100, Informational: true})
	nw.Add("S", Metric{Name: "p99", Value: 300, Informational: true})
	if AnyRegressed(Compare(base, nw, 0.05)) {
		t.Fatal("informational metrics must never regress")
	}
}

func TestToleranceRaisesTheBar(t *testing.T) {
	base, nw := &Result{}, &Result{}
	base.Add("S", Metric{Name: "noisy", Value: 100, HigherIsBetter: true, Tolerance: 0.12})
	nw.Add("S", Metric{Name: "noisy", Value: 90, HigherIsBetter: true, Tolerance: 0.12})
	if AnyRegressed(Compare(base, nw, 0.05)) {
		t.Fatal("a 10% drop inside a 12% tolerance must not regress")
	}
	nw.Scenarios[0].Metrics[0].Value = 85
	if !AnyRegressed(Compare(base, nw, 0.05)) {
		t.Fatal("a 15% drop outside a 12% tolerance must regress")
	}
}

func TestFormatTableMarksRegressions(t *testing.T) {
	base, nw := twoResults()
	out := FormatTable(Compare(base, nw, 0.05))
	if !strings.Contains(out, "B2") || !strings.Contains(out, "REGRESSION") {
		t.Fatalf("table missing regression marker:\n%s", out)
	}
}
