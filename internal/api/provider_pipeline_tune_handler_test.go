package api

import "testing"

func TestPickPipelineDepth(t *testing.T) {
	tests := []struct {
		name     string
		baseline float64
		samples  []PipelineDepthSample
		wantInfl int
		wantOn   bool
	}{
		{
			name:     "clear win enables smallest winning depth",
			baseline: 10,
			samples:  []PipelineDepthSample{{Depth: 4, Mbps: 12}, {Depth: 8, Mbps: 18}, {Depth: 16, Mbps: 18}},
			wantInfl: 8,
			wantOn:   true,
		},
		{
			name:     "sub-threshold gain stays off",
			baseline: 10,
			samples:  []PipelineDepthSample{{Depth: 4, Mbps: 10.5}, {Depth: 8, Mbps: 10.9}, {Depth: 16, Mbps: 10.2}},
			wantInfl: 1,
			wantOn:   false,
		},
		{
			name:     "no gain stays off",
			baseline: 10,
			samples:  []PipelineDepthSample{{Depth: 4, Mbps: 9}, {Depth: 8, Mbps: 8}},
			wantInfl: 1,
			wantOn:   false,
		},
		{
			name:     "zero baseline stays off",
			baseline: 0,
			samples:  []PipelineDepthSample{{Depth: 4, Mbps: 5}, {Depth: 8, Mbps: 9}},
			wantInfl: 1,
			wantOn:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infl, on, _, _ := pickPipelineDepth(tt.baseline, tt.samples)
			if infl != tt.wantInfl || on != tt.wantOn {
				t.Fatalf("got (%d, %v), want (%d, %v)", infl, on, tt.wantInfl, tt.wantOn)
			}
		})
	}
}
