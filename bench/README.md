# Streaming benchmarks

`make bench-stream` runs `BenchmarkStream*` in `internal/nzbfilesystem` against
the in-process provider simulator (`internal/testsupport/nntpserver`) and writes
`bench/results/<short-sha>.json`. `make bench-compare BASE=<name>` diffs the
current results against `bench/results/<name>.json` and exits non-zero when any
gated metric regresses by more than 5 % (or its own wider tolerance) in its bad
direction. Each scenario is run three times and the median is recorded.

Scenarios: B1 cold open TTFB, B2 sequential startup and steady throughput, B3
seek storm, B4 four concurrent streams, B5 two handles on one file, B6 stream
under import contention, B7 provider failover, B8 bytes fetched while paused,
B9 forward skip, B10 close-and-reopen replay, B11 seek and resume.

Results are committed per landed change so a PR description can cite its delta
table; per-SHA files are scratch output. Run on an idle machine; the simulator
is CPU-bound at high aggregate bandwidth. Full guide: `docs/docs/6. Development/setup.md`.

## External comparison

`bench/nzb-streaming-benchmarks/` holds a rerun of Viren070's public
[nzb-streaming-benchmarks](https://github.com/Viren070/nzb-streaming-benchmarks)
harness against AltMount and the other NZB streaming applications it covers.
`RESULTS.md` is the generated report with a provenance preamble, `results.json` the
raw run, `CORPUS.md` the local corpus. Numbers are comparable within that report
only; the corpus differs from the published one.
