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
