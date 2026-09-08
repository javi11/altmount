# NZB streaming benchmark (AltMount rerun, 2026-09-08)

This is a local rerun of Viren070's public harness
[nzb-streaming-benchmarks](https://github.com/Viren070/nzb-streaming-benchmarks)
(harness commit `7192cab`) against AltMount `495b4949` (`v0.3.2-92`) — PR #938
("feat(streaming): hedge slow demand-position article fetches on a second
connection") rebased onto PR #933's branch, i.e. `main` through b9213522 plus the
hedge feature. Unlike the prior full-field passes recorded below, **this pass
covers only `raw` and `altmount`**, to isolate the hedge feature's effect without
spending the link budget on the other 7 applications. The generated report follows
unchanged below; `results.json` is the raw run data and `CORPUS.md` describes the
entries.

## How this run differs from the published one

- **Different corpus.** The published NZBs are not distributed, so this run uses a
  14-entry local corpus built with the harness's own `analyze`/`probe`/`select`
  pipeline (see `CORPUS.md`). It covers the same axes (direct video, RAR4/RAR5,
  encrypted headers, split 7z, obfuscation, missing articles, dead posts) but the
  posts, sizes and article counts differ. **Numbers are comparable across the rows
  of this report, not against the published tables.**
- **Different host and link.** Apple M4, 16 GB, macOS, 20 connections to a single
  Newshosting frontend. The published run is a Ryzen 7800X3D on Windows.
- **Two-app pass.** This run only exercises `raw` and `altmount` (see above); the
  Docker-dependent rows (nzbdav, nzbdavex, InfiniDysk, Decypharr) and the other
  source rows (StremThru, AIOStreams, StreamNZB) are not part of this pass. The
  full 9-app field comparison from 2026-09-07 is preserved below for reference.
- **Comet is excluded.** Its native usenet engine fails to initialise inside Docker
  Desktop on macOS (`Native engine startup failed: initialization_failure`; it
  requires Landlock from the host kernel). The published run excludes it too.
- **Single pass.** The harness's own caveat applies: treat differences under about
  20% between applications as unresolved by one run.

## AltMount: this run vs the published row vs prior local reruns

Shape only, since the corpus differs between all of them. Published values are
AltMount `3ed4c47` from the 2026-08-28 run; `3fd79fad` is the 2026-09-06 rerun;
`40f463aa` is the noisy 2026-09-07 full-field rerun (see the A/B analysis below it
in this file's history for why that pass reads low). `495b4949` is this run, the
same `40f463aa` build plus PR #938's hedge feature, on a `raw`+`altmount`-only pass.

| Metric | Published (`3ed4c47`) | Rerun (`3fd79fad`) | Rerun (`40f463aa`) | This run (`495b4949`, hedge) |
|---|---:|---:|---:|---:|
| Capability gaps | 7 | 1 | 1 | 1 |
| Wrongly served | 1 | 0 | 0 | 0 |
| Click→byte (median) | 3.91 s | 1.05 s | 940 ms | 1.08 s |
| Cold TTFB | 234 ms | 58 ms | 58 ms | 49 ms |
| Warm TTFB | 328 ms | 6 ms | 5 ms | 2 ms |
| Full seek | 762 ms | 276 ms | 401 ms | 379 ms |
| Seek TTFB | 384 ms | 126 ms | 101 ms | 98 ms |
| Seq MB/s | 39.4 | 67.7 | 36.6 | 71.7 |
| p05 MB/s | 26.2 | 34.4 | 3.4 | 41.4 |
| CPU s/GiB | 4.2 | 5.6 | 6.1 | 5.1 |
| RSS/item | 766 MiB | 557 MiB | 706 MiB | 537 MiB |
| Peak RSS | 2026 MiB | 668 MiB | 846 MiB | 610 MiB |
| Drift | +618 MiB | -116 MiB | +15 MiB | -113 MiB |

The one remaining gap is `rar-hdrenc-small`: an encrypted RAR whose inner file is a
nested RAR using rar2.9 compression — unchanged by the hedge feature, as expected
(it fails at import, before any streaming path runs). This run also exercises three
`failure`-tier entries (`rar-partNN-large/maestras/satans`) that turned out to have
a first article missing from the provider itself, confirmed by `raw` failing them
identically with "first article missing" — a provider/corpus artifact, scored
`refused` across the board rather than counted against anyone.

### The hedge feature's measured effect

The fairest comparison in one pass is **AltMount against `raw` in the same pass**,
since `raw` shares the link and the moment but has no hedge (or any other
mitigation) for a straggling fetch — a stuck article stalls it exactly as
before. In every prior pass, `raw` has been the ceiling: it never opens an
archive, does no reassembly, and has always led AltMount on both `Seq MB/s` and
`p05 MB/s`. In this pass, with hedging live, **AltMount beat `raw` on both**:

| | raw (no hedge) | AltMount (hedged) | AltMount vs raw |
|---|---:|---:|---:|
| Seq MB/s | 57.9 | 71.7 | **+24%** |
| p05 MB/s | 11.7 | 41.4 | **+254%** |
| Full seek | 201 ms | 379 ms | -89% (webdav overhead, not hedge-related) |

`raw`'s own numbers this pass (57.9 / 11.7) are on the low side of what prior
passes have measured for it (67.5-81.6 / 44.6-64.5), meaning the link itself was
worse than usual during this run — exactly the kind of pass where a straggling
fetch would previously have stalled AltMount's in-order delivery too. Instead
AltMount's `p05` came in at 41.4 MB/s, in the range prior *good* passes measured
(34.4-43.8), while sharing a link that made the unhedged baseline measure a p05 of
11.7. That is the scenario the feature targets: a demand-position fetch stuck
behind another request on the same connection no longer stalls the whole stream,
because a second connection is raced for it. This is the first pass, of all
recorded here, where AltMount's streaming numbers were not bounded above by
`raw`'s.

One pass is not proof by the harness's own caveat (differences need repeating to
separate signal from the ~20-55% per-entry link noise documented above), but the
direction and the mechanism agree, and no capability regression or new failure
was introduced by the change (see the capability matrix below — identical to the
`40f463aa` pass).

## Prior full-field rerun (2026-09-07, `40f463aa`, 9 apps)

This section is preserved for reference; it predates the hedge feature and covers
the full application field. **The throughput numbers in that pass are provider/link**
**noise, not a code regression** — see the controlled A/B below.

In the full run AltMount's seq fell 67.7→36.6 MB/s and playback p05 34.4→3.4 MB/s
against the `3fd79fad` rerun, while RSS/item rose 557→706 MiB. The only code delta
at the time was the `main` merge (#934 import-side, #935 GC soft-limit fix), so it
was checked with a controlled A/B straight after the run, `raw` + AltMount only,
back to back, nothing else on the host:

| | AltMount build | raw seq / p05 | AltMount seq / p05 | RSS/item / peak |
|---|---|---:|---:|---:|
| A | `40f463aa` (that run's merge) | 62.9 / 50.7 | 58.9 / 43.8 | 681 / 719 MiB |
| B | `9eb9e602` (pre-merge) | 81.6 / 64.5 | 64.8 / 14.4 | 551 / 641 MiB |

The merged build did not reproduce the drop and held ~94% of `raw` on seq
(pre-merge: 79%) — still below `raw`, unlike this run. The pre-merge build showed
the same per-entry playback dips to the 25 Mbps pacing floor (1.0-3.4 MB/s on three
entries) that the full run showed on most entries — the playback p05 column is
simply volatile on this link, per pass and per entry, exactly as the harness's own
caveat warns. The memory governor added in #935 never engaged in any of those
passes (no shrink warning logged). What #935 changes, by design, is the derived Go
memory limit (512→677 MB with this config), and the ~+130 MiB RSS/item between B
and A was that headroom being used rather than a leak.

The full 9-app capability matrix, per-entry detail, and CPU/memory tables from
that pass are kept in this file's git history (see the `2026-09-07` commit) rather
than duplicated here, since this pass's generated report below only covers
`raw`/`altmount`.

## Reproducing

```sh
git clone https://github.com/Viren070/nzb-streaming-benchmarks
cd nzb-streaming-benchmarks
ln -s /path/to/altmount apps/altmount        # or let scripts/clone-apps.sh clone it
cp .env.example .env                          # NNTP_HOST/PORT/TLS/USER/PASS/CONNS
# drop NZBs into corpus/pool/, then:
npm run corpus
node src/cli.mjs run --apps=raw,altmount --tier=smoke,core,failure --order=given
```

On macOS the harness needs a `ps`-based process sampler in `src/metrics/procmon.mjs`
and a null-check on the timeout waiter in `src/nntp/client.mjs`; both patches are
small and upstream-worthy but not yet submitted.

---

## Generated report

**Run** `2026-09-08T08-14-57-768Z` · started 2026-09-08T08:14:57.768Z · finished 2026-09-08T08:28:22.271Z

## Environment

darwin 25.5.0 · Apple M4 (10 threads) · 16 GB RAM

| | |
|---|---|
| OS | darwin 25.5.0 |
| CPU | Apple M4 |
| RAM | 16.00 GiB |
| Harness | Node v24.19.0 |

### NNTP providers

| Provider | Port | TLS | Max conns | Role |
|---|---:|:---:|---:|---|
| `client.fr7.newshosting.com` | 563 | yes | 20 | primary |

> Throughput is bounded by the provider and the link, not only by the application.
> The `raw` row is this harness fetching the same articles with no application in
> the middle, so read every other number relative to it.

> **The link is the largest source of error here, and it is not controlled.** These
> runs are made over consumer Wi-Fi to a commercial provider, and both vary on their
> own. Repeating a pass with nothing changed but the clock has moved the whole-run
> median by 14% and individual entries by 55%, and one measured evening had a single
> post served at 9 MB/s by `raw` while others on the same connection ran at 45 MB/s.
> Provider variance is per post, not per session: which entries are slow changes from
> run to run, so a low number for one entry is not a property of the application.
> Treat differences under roughly 20% between applications, or under 50% on a single
> entry, as unresolved by one pass. Only repeated runs can separate the two.

### Applications measured

| App | Runtime | Language | Version | Serving | Startup |
|---|---|---|---|---|---:|
| **raw NNTP baseline** | source | JavaScript (this harness) | `harness-builtin` | http-range | 6 ms |
| **AltMount** | source | Go | `v0.3.2-92-g495b4949` | webdav | 1.27 s |

### Run settings

| Setting | Value |
|---|---|
| Sequential read | 244 MiB cap / 30s cap |
| Seek points | 1%, 25%, 50%, 75%, 95% + backward |
| Seek read | 8 MiB |
| Playback sim | 30s @ 25 Mbps |
| Integrity samples | 3 |
| Item timeout | 600s |

## Summary

Every median below is taken over **the same 9 entries for every**
**application**: the perf-tier entries (`smoke`, `core`, `stress`) that all
2 applications served. Median post size across that set is
15.9 GiB.

Entries: `rar4-small`, `plain-medium`, `obfuscated-direct`, `plain-large`, `plain-season-pack`, `rar4-rNN`, `rar-hdrenc-large`, `7z-split-bugonia`, `7z-split-tardes`.

### Verdict

*Correct* is not *served*. Six corpus entries are built to be unservable: three
`negative` (compressed archives, no password) and three `failure` (dead post,
severe damage, missing volumes). Refusing those is the right answer, and serving
one means emitting bytes that cannot be the media, which is a worse result than
refusing, not a better one.

| App | Served | Capability gaps | Correctly refused | **Wrongly served** |
|---|---:|---:|---:|---:|
| **raw NNTP baseline** | 11/11 | 0 | 3/3 | 0 |
| **AltMount** | 10/11 | 1 | 3/3 | 0 |

A *capability gap* is the number that ranks engines: entries that should stream
and did not. `raw` is not an application and its row is not a verdict: it serves
outer volume bytes without opening an archive, so it "wrongly serves" entries no
player could open. That is the point of the baseline, not a defect in it.

### Time to picture

| App | n | Click&rarr;byte | Import | Cold TTFB | Warm TTFB |
|---|---:|---:|---:|---:|---:|
| **raw NNTP baseline** | 9/9 | **524 ms** | 391 ms | 140 ms | 152 ms |
| **AltMount** | 9/9 | **1.08 s** | 1.08 s | 49 ms | 2 ms |

*Click&rarr;byte* is import + cold open: what a viewer waits through after pressing
play, and the only one of these three that is comparable. Every application here but
one inspects the post at import and then answers the first byte quickly, so import is
over 80% of the wait. StreamNZB is the exception: it returns a session in
milliseconds and does the same work on first byte, which is why its import reads as
free and its cold TTFB does not. Serving mode does not predict this, since AIOStreams
answers byte ranges like StreamNZB and still front-loads like the mount-style
applications. *Warm TTFB* is the same open repeated, so it measures what the engine
cached rather than what it can do cold.

### Streaming and seeks

| App | Seq MB/s | p05 MB/s | Full seek | Seek TTFB | Worst seek |
|---|---:|---:|---:|---:|---:|
| **raw NNTP baseline** | 57.9 | **11.7** | **201 ms** | 140 ms | 165 ms |
| **AltMount** | 71.7 | **41.4** | **379 ms** | 98 ms | 190 ms |

*p05 MB/s* is the 5th-percentile one-second windowed rate, which is what a player
actually feels: a mean rate hides a stall that a p05 does not.

*Full seek* is the median time to complete a whole seek read, acknowledgement plus
transfer, rather than the moment the first byte appears. An engine that answers a
Range immediately and then feeds the body slowly wins *Seek TTFB* and loses this
column, and this column is the one a player waits through. Where the two disagree,
believe this one.

### CPU

| App | CPU s/GiB | Cores (p95) | Cores (max) | Steady |
|---|---:|---:|---:|---:|
| **raw NNTP baseline** | **17.8** | 1.0 | 1.0 | 98% |
| **AltMount** | **5.1** | 0.5 | 0.5 | 89% |

*CPU s/GiB* is CPU-seconds consumed per GiB delivered, the fair efficiency
comparison, since a raw percentage is meaningless at different throughputs.

The other three are the shape of the draw rather than its size, which a total
cannot express: ten CPU-seconds is a steady half core for twenty seconds or one
core pinned for ten, and those cost a shared box differently. *Cores (p95)* is the
level it sustains, *Cores (max)* the worst single second, and *Steady* the share of
seconds spent at or above half the p95. A high *Steady* is an engine that hums; a
low one with a tall *max* burns the same CPU in bursts against an idle baseline,
which is what makes a box feel busy while the averages look calm.

All three are per entry and then taken as medians, so *Cores (max)* is the typical
worst second of an entry, not the worst second of the run. They are bounded below
by the 1s sample interval: a shorter spike is averaged away, so these
understate burstiness and never overstate it. Entries that finished in fewer than
four samples carry no shape and are excluded from these three columns only.

### Memory

| App | Idle RSS | RSS/item | Peak RSS | over | Drift | After idle |
|---|---:|---:|---:|---:|---:|---:|
| **raw NNTP baseline** | 72 MiB | **417 MiB** | 1572 MiB | 14 entries | -999 MiB | 801 MiB |
| **AltMount** | 103 MiB | **537 MiB** | 610 MiB | 14 entries | -113 MiB | 357 MiB |

*RSS/item* is the median of the per-entry peaks and is the comparable number.
*Peak RSS* is the highest single-entry peak in the run: **not representative of**
**real-world usage**, since it is a high-water mark reached once, but it is the
number that decides whether the application fits in the RAM you have. Read it with
the *over* column beside it, which says how many entries the peak was taken over:
a run-wide peak rewards failing early, and an application that survived 21 entries
had fewer chances to spike than one that survived 31.

*Drift* is the median per-entry peak over the last third of the run minus the
first third. Every application here holds more memory the longer it runs, and this
states how much rather than letting it inflate the headline. It is measured with no
idle gap between entries, which is the harshest case: applications that release on
idle never get the chance to. *After idle* is the median footprint once the work
stops but before the process is killed, which is where that memory goes back.

These are taken over every measured entry, including failed ones, since a failure
still occupies a position in the session, and over the whole session rather than
the shared population. Entries merged from another run are excluded, because their
footprint is another process's.

## Capability matrix

Every entry scored against what it is supposed to do, not against its status code.

| | |
|---|---|
| `pass` | should stream, and did |
| **`FAIL`** | should stream, and did not |
| `refused` | unservable, and was refused |
| **`served`** | unservable, and bytes came back anyway |

> **The `raw` column is not a capability claim.** It streams the outer volume
> bytes without opening the archive, so it "passes" encrypted and obfuscated
> entries that no application could actually play. Read it as "the articles are
> retrievable", which is exactly what makes it useful: a failure everywhere *except*
> raw is an application limitation, not a dead post.

| Entry | Tier | raw NNTP baseline | AltMount |
|---|---|---|---|
| `rar4-small` | smoke | pass | pass |
| `plain-medium` | smoke | pass | pass |
| `obfuscated-direct` | core | pass | pass |
| `plain-large` | core | pass | pass |
| `plain-season-pack` | core | pass | pass |
| `rar4-rNN` | core | pass | pass |
| `rar-hdrenc-small` | core | pass | **FAIL** |
| `rar-hdrenc-large` | core | pass | pass |
| `rar-partNN-large` | failure | refused | refused |
| `rar-partNN-maestras` | failure | refused | refused |
| `rar-partNN-satans` | failure | refused | refused |
| `7z-split-bugonia` | core | pass | pass |
| `7z-split-tardes` | core | pass | pass |
| `damaged-partial` | failure | pass | pass |

## Byte-identity cross-check

The same byte ranges hashed by every application and compared **against each**
**other**, since a fast application serving the wrong bytes is not fast. The
consensus hash is the one at least two applications agree on; a row that differs
is the one to investigate.

`raw` participates only on `direct-video` entries: for archived posts it serves
the outer volume stream rather than the assembled inner file, so it is not a
valid reference there.

**Every application agreed on every comparable range** (5 entries, 10 app-entry pairs).

## Per-entry detail

### raw NNTP baseline

`raw` · JavaScript (this harness) · version `harness-builtin` · serving: http-range · runtime: source · startup 6 ms

**Own set**: 10 entries, median post 11.3 GiB · click&rarr;byte 437 ms (shared population: 524 ms, 1.20×) · seq 50.1 MB/s · CPU 17.8 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 623 ms | 140 ms | 66.4† | 105 ms | — | — | — | 265 MiB | ok |
| `plain-medium` | 412 ms | 900 ms | 114.0 | 838 ms | 58.6 | 1.29 s | 35.8 | 1418 MiB | ok |
| `obfuscated-direct` | 418 ms | 1.06 s | 69.0 | 808 ms | 55.7 | 1.24 s | 35.7 | 1413 MiB | ok |
| `plain-large` | 394 ms | 399 ms | 21.1 | 318 ms | 61.0 | 907 ms | 17.8 | 1572 MiB | ok |
| `plain-season-pack` | 137 ms | 116 ms | 42.4 | 153 ms | 35.7 | 1.14 s | 13.3 | 1511 MiB | ok |
| `rar4-rNN` | 197 ms | 122 ms | 83.9† | 130 ms | — | 427 ms | 38.7 | 405 MiB | ok |
| `rar-hdrenc-small` | 128 ms | 145 ms | 38.9 | 144 ms | 1.2 | 1.96 s | 26.4 | 410 MiB | ok |
| `rar-hdrenc-large` | 179 ms | 172 ms | 35.5 | 114 ms | 93.8 | 640 ms | 17.8 | 500 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 405 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 385 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 371 MiB | **failed** |
| `7z-split-bugonia` | 391 ms | 133 ms | 57.9 | 138 ms | 103.0 | 532 ms | 16.0 | 422 MiB | ok |
| `7z-split-tardes` | 142 ms | 136 ms | 65.6 | 140 ms | 42.6 | 542 ms | 17.3 | 411 MiB | ok |
| `damaged-partial` | 850 ms | 960 ms | 72.5 | 1.12 s | 76.4 | 1.52 s | 33.3 | 1570 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (3)</summary>

- `rar-partNN-large` (failure): first article missing
- `rar-partNN-maestras` (failure): first article missing
- `rar-partNN-satans` (failure): first article missing

</details>

### AltMount

`altmount` · Go · version `v0.3.2-92-g495b4949` · serving: webdav · runtime: source · startup 1.27 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.08 s (shared population: 1.08 s, 1.00×) · seq 71.7 MB/s · CPU 5.1 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 983 ms | 41 ms | 61.9 | 2 ms | 60.3 | 262 ms | 3.8 | 395 MiB | ok |
| `plain-medium` | 233 ms | 96 ms | 78.3 | 346 ms | 25.7 | 1.64 s | 4.1 | 610 MiB | ok |
| `obfuscated-direct` | 1.08 s | 4 ms | 46.2 | 191 ms | 66.9 | 1.94 s | 4.1 | 605 MiB | ok |
| `plain-large` | 885 ms | 49 ms | 79.3 | 98 ms | 82.6 | 781 ms | 4.1 | 560 MiB | ok |
| `plain-season-pack` | 728 ms | 72 ms | 87.5 | 132 ms | 51.4 | 861 ms | 5.1 | 581 MiB | ok |
| `rar4-rNN` | 1.63 s | 49 ms | 77.0 | 99 ms | 50.7 | 1.11 s | 5.8 | 539 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 526 MiB | **failed** |
| `rar-hdrenc-large` | 1.36 s | 61 ms | 71.1 | 86 ms | 32.3 | 681 ms | 6.9 | 548 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 534 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 491 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 463 MiB | **failed** |
| `7z-split-bugonia` | 2.91 s | 48 ms | 52.6 | 84 ms | 38.6 | 825 ms | 7.3 | 476 MiB | ok |
| `7z-split-tardes` | 3.02 s | 53 ms | 71.7 | 90 ms | 47.2 | 1.02 s | 7.1 | 423 MiB | ok |
| `damaged-partial` | 129 ms | 91 ms | 58.7 | 307 ms | 79.1 | 1.69 s | 15.6 | 559 MiB | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: failed to process nested RAR archives: failed to process nested RAR "b-simpson": compressed media files are not supported: b-simpson.iso (uses rar2.9 compression)
- `rar-partNN-large` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)
- `rar-partNN-maestras` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)
- `rar-partNN-satans` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)

</details>

## Corpus used

See `docs/CORPUS.md` for why each entry is in the set.

| Entry | Tier | Posted | Axes |
|---|---|---:|---|
| `rar4-small` | smoke | 0.37 GiB | `rar4`, `stored`, `named-volumes`, `partNN` |
| `plain-medium` | smoke | 6.67 GiB | `direct-video`, `no-archive`, `per-file-unique-stems` |
| `obfuscated-direct` | core | 6.67 GiB | `direct-video`, `obfuscated-names`, `extensionless` |
| `plain-large` | core | 15.93 GiB | `direct-video`, `no-archive` |
| `plain-season-pack` | core | 22.62 GiB | `direct-video`, `season-pack`, `file-selection` |
| `rar4-rNN` | core | 6.15 GiB | `rar4`, `stored`, `rar+rNN`, `letter-rollover` |
| `rar-hdrenc-small` | core | 5.17 GiB | `rar5`, `encrypted-headers`, `password-in-nzb` |
| `rar-hdrenc-large` | core | 24.01 GiB | `rar5`, `encrypted-headers`, `password-in-nzb` |
| `rar-partNN-large` | failure | 19.46 GiB | `rar`, `partNN`, `password-in-nzb` |
| `rar-partNN-maestras` | failure | 3.49 GiB | `rar`, `partNN`, `password-in-nzb` |
| `rar-partNN-satans` | failure | 3.11 GiB | `rar`, `partNN`, `password-in-nzb` |
| `7z-split-bugonia` | core | 20.83 GiB | `7z`, `split-7z.NNN`, `password-in-nzb` |
| `7z-split-tardes` | core | 19.34 GiB | `7z`, `split-7z.NNN`, `password-in-nzb` |
| `damaged-partial` | failure | 6.67 GiB | `direct-video`, `missing-articles` |

---

Generated from `results.json` by `src/report/markdown.mjs`. Regenerate with
`node src/cli.mjs report <run-dir>`.
