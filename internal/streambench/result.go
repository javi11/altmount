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
	Profile   string     `json:"profile,omitempty"`
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
