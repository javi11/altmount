# Streaming benchmarks

`make bench-stream` runs `BenchmarkStream*` in `internal/nzbfilesystem` against
the in-process provider simulator (`internal/testsupport/nntpserver`) and writes
`bench/results/<short-sha>.json`. `make bench-compare BASE=<name>` diffs the
current results against `bench/results/<name>.json` and exits non-zero when any
metric regresses by more than 5 % in its bad direction.

Scenarios: B1 cold open TTFB, B2 sequential throughput and waste, B3 seek
storm, B4 four concurrent streams, B5 two handles on one file, B6 stream under
import contention, B7 provider failover, B8 bytes fetched while paused.

Results are committed per phase so a PR description can cite its delta table.
Run on an idle machine; the simulator is CPU-bound at high aggregate bandwidth.
