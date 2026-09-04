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

func TestFormatTableMarksRegressions(t *testing.T) {
	base, nw := twoResults()
	out := FormatTable(Compare(base, nw, 0.05))
	if !strings.Contains(out, "B2") || !strings.Contains(out, "REGRESSION") {
		t.Fatalf("table missing regression marker:\n%s", out)
	}
}
