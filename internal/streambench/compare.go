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
	Informational  bool
	Regressed      bool
}

// Compare pairs metrics by scenario and name. A metric regresses when it
// moves more than threshold (fraction, e.g. 0.05) in its bad direction; a
// metric-level Tolerance on either side raises that bar for that metric.
// Metrics present in only one result are reported with a zero delta.
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
			d := Delta{Scenario: sc.Name, Metric: m.Name, Unit: m.Unit, New: m.Value, HigherIsBetter: m.HigherIsBetter, Informational: m.Informational}
			if b, ok := baseIdx[key{sc.Name, m.Name}]; ok && b.Value != 0 {
				d.Base = b.Value
				d.Pct = (m.Value - b.Value) / b.Value
				threshold := max(threshold, m.Tolerance, b.Tolerance)
				switch {
				case m.Informational:
				case m.HigherIsBetter:
					d.Regressed = d.Pct < -threshold
				default:
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
		switch {
		case x.Regressed:
			mark = "REGRESSION"
		case x.Informational:
			mark = "info"
		}
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%+.1f%%\t%s\t%s\n",
			x.Scenario, x.Metric, x.Base, x.New, x.Pct*100, x.Unit, mark)
	}
	_ = w.Flush()
	return sb.String()
}
