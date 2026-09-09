# NZB streaming benchmark

**Run** `2026-09-09T05-50-54-806Z` · started 2026-09-09T05:50:54.806Z · finished 2026-09-09T09:04:02.379Z

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
| **Decypharr** | source | Go | `v2.5` (`0dd1cbb`) | webdav | 769 ms |
| **nzbdav** | source | C# (.NET 10) | `0c7d8e2` | webdav | 1.58 s |
| **raw NNTP baseline** | source | JavaScript (this harness) | `harness-builtin` | http-range | 6 ms |
| **InfiniDysk** | source | C# (.NET 10) | `63bde75` | webdav | 4.83 s |
| **AIOStreams** | source | TypeScript | `2026.09.07.2154-nightly` | http-range | 14.16 s |
| **StreamNZB** | source | Go | `3c69f1e` | http-range | 1.27 s |
| **StremThru (newz)** | source | Go | `0.104.1` | http-range | 1.53 s |
| **AltMount** | source | Go | `v0.3.2-97-g026bd939` | webdav | 1.27 s |

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

Every median below is taken over **the same 5 entries for every**
**application**: the perf-tier entries (`smoke`, `core`, `stress`) that at least
7 of the 8 applications served. Median post size across that set is
15.9 GiB.

> **Why a quorum and not the entries all of them served.** That strict intersection
> is 0 entries here, and it is defined by the weakest application in the field:
> one broken engine collapses the population for everybody, and the set moves between
> runs as the field changes. A quorum keeps it wide and stable. Where an application
> missed one of the 5, its `n` column says so.

Entries: `rar4-small`, `plain-medium`, `plain-large`, `plain-season-pack`, `rar-hdrenc-large`.

### Verdict

*Correct* is not *served*. Six corpus entries are built to be unservable: three
`negative` (compressed archives, no password) and three `failure` (dead post,
severe damage, missing volumes). Refusing those is the right answer, and serving
one means emitting bytes that cannot be the media, which is a worse result than
refusing, not a better one.

| App | Served | Capability gaps | Correctly refused | **Wrongly served** |
|---|---:|---:|---:|---:|
| **Decypharr** | 7/11 | 4 | 3/3 | 0 |
| **nzbdav** | 0/11 | 11 | 3/3 | 0 |
| **raw NNTP baseline** | 11/11 | 0 | 2/3 | **1** |
| **InfiniDysk** | 9/11 | 2 | 3/3 | 0 |
| **AIOStreams** | 10/11 | 1 | 3/3 | 0 |
| **StreamNZB** | 9/11 | 2 | 3/3 | 0 |
| **StremThru (newz)** | 10/11 | 1 | 3/3 | 0 |
| **AltMount** | 10/11 | 1 | 3/3 | 0 |

A *capability gap* is the number that ranks engines: entries that should stream
and did not. `raw` is not an application and its row is not a verdict: it serves
outer volume bytes without opening an archive, so it "wrongly serves" entries no
player could open. That is the point of the baseline, not a defect in it.

### Time to picture

| App | n | Click&rarr;byte | Import | Cold TTFB | Warm TTFB |
|---|---:|---:|---:|---:|---:|
| **Decypharr** | 5/5 | **2.56 s** | 2.32 s | 315 ms | 1 ms |
| **nzbdav** | 0/5 | **—** | — | — | — |
| **raw NNTP baseline** | 5/5 | **533 ms** | 274 ms | 138 ms | 112 ms |
| **InfiniDysk** | 5/5 | **2.13 s** | 2.08 s | 83 ms | 245 ms |
| **AIOStreams** | 5/5 | **826 ms** | 810 ms | 13 ms | 4 ms |
| **StreamNZB** | 5/5 | **1.65 s** | 40 ms | 1.57 s | 1 ms |
| **StremThru (newz)** | 5/5 | **1.02 s** | 943 ms | 42 ms | 15 ms |
| **AltMount** | 5/5 | **775 ms** | 771 ms | 4 ms | 4 ms |

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
| **Decypharr** | 76.0 | **52.5** | **292 ms** | 147 ms | 552 ms |
| **nzbdav** | — | **—** | **—** | — | — |
| **raw NNTP baseline** | 79.0 | **60.9** | **237 ms** | 124 ms | 149 ms |
| **InfiniDysk** | 72.9 | **52.5** | **610 ms** | 193 ms | 297 ms |
| **AIOStreams** | 81.3 | **29.9** | **489 ms** | 116 ms | 146 ms |
| **StreamNZB** | 56.2 | **41.2** | **492 ms** | 200 ms | 535 ms |
| **StremThru (newz)** | 37.3 | **24.7** | **490 ms** | 217 ms | 396 ms |
| **AltMount** | 65.0 | **50.1** | **347 ms** | 126 ms | 223 ms |

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
| **Decypharr** | **8.2** | 0.6 | 0.7 | 53% |
| **nzbdav** | **—** | — | — | — |
| **raw NNTP baseline** | **16.4** | 1.1 | 1.1 | 97% |
| **InfiniDysk** | **19.2** | 1.6 | 1.8 | 85% |
| **AIOStreams** | **5.9** | 0.7 | 0.7 | 89% |
| **StreamNZB** | **7.6** | 0.7 | 0.7 | 82% |
| **StremThru (newz)** | **9.4** | 0.6 | 0.6 | 93% |
| **AltMount** | **5.1** | 0.6 | 0.6 | 88% |

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
| **Decypharr** | 25 MiB | **114 MiB** | 153 MiB | 14 entries | +91 MiB | 113 MiB |
| **nzbdav** | 166 MiB | **134 MiB** | 203 MiB | 14 entries | -18 MiB | 119 MiB |
| **raw NNTP baseline** | 53 MiB | **456 MiB** | 1885 MiB | 14 entries | -1142 MiB | 1120 MiB |
| **InfiniDysk** | 245 MiB | **1369 MiB** | 2023 MiB | 14 entries | -585 MiB | 651 MiB |
| **AIOStreams** | 764 MiB | **1327 MiB** | 1442 MiB | 14 entries | -195 MiB | 1295 MiB |
| **StreamNZB** | 52 MiB | **641 MiB** | 794 MiB | 14 entries | +59 MiB | 711 MiB |
| **StremThru (newz)** | 223 MiB | **894 MiB** | 930 MiB | 14 entries | +104 MiB | 739 MiB |
| **AltMount** | 99 MiB | **825 MiB** | 897 MiB | 14 entries | +21 MiB | 827 MiB |

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

### Not measured

| App | Status | Reason |
|---|---|---|
| Comet (feat/usenet) | `unsupported-on-platform` | Comet (feat/usenet) cannot run from source on darwin (supported: linux) |

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

| Entry | Tier | Decypharr | nzbdav | raw NNTP baseline | InfiniDysk | AIOStreams | StreamNZB | StremThru (newz) | AltMount |
|---|---|---|---|---|---|---|---|---|---|
| `rar4-small` | smoke | pass | **FAIL** | pass | pass | pass | pass | pass | pass |
| `plain-medium` | smoke | pass | **FAIL** | pass | pass | pass | pass | pass | pass |
| `obfuscated-direct` | core | **FAIL** | **FAIL** | pass | pass | pass | pass | pass | pass |
| `plain-large` | core | pass | **FAIL** | pass | pass | pass | pass | pass | pass |
| `plain-season-pack` | core | pass | **FAIL** | pass | pass | pass | pass | pass | pass |
| `rar4-rNN` | core | pass | **FAIL** | pass | **FAIL** | pass | **FAIL** | pass | pass |
| `rar-hdrenc-small` | core | **FAIL** | **FAIL** | pass | **FAIL** | **FAIL** | **FAIL** | **FAIL** | **FAIL** |
| `rar-hdrenc-large` | core | pass | **FAIL** | pass | pass | pass | pass | pass | pass |
| `rar-partNN-large` | failure | refused | refused | refused | refused | refused | refused | refused | refused |
| `rar-partNN-maestras` | failure | refused | refused | **served** | refused | refused | refused | refused | refused |
| `rar-partNN-satans` | failure | refused | refused | refused | refused | refused | refused | refused | refused |
| `7z-split-bugonia` | core | **FAIL** | **FAIL** | pass | pass | pass | pass | pass | pass |
| `7z-split-tardes` | core | **FAIL** | **FAIL** | pass | pass | pass | pass | pass | pass |
| `damaged-partial` | failure | pass | **FAIL** | pass | pass | pass | pass | pass | pass |

## Byte-identity cross-check

The same byte ranges hashed by every application and compared **against each**
**other**, since a fast application serving the wrong bytes is not fast. The
consensus hash is the one at least two applications agree on; a row that differs
is the one to investigate.

`raw` participates only on `direct-video` entries: for archived posts it serves
the outer volume stream rather than the assembled inner file, so it is not a
valid reference there.

**Every application agreed on every comparable range** (10 entries, 60 app-entry pairs).

## Per-entry detail

### Decypharr

`decypharr` · Go · version `v2.5` (`0dd1cbb`) · serving: webdav · runtime: source · startup 769 ms

**Own set**: 6 entries, median post 11.3 GiB · click&rarr;byte 3.13 s (shared population: 2.56 s, 0.82×) · seq 73.4 MB/s · CPU 8.3 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 2.32 s | 210 ms | 47.0 | 5 ms | — | 479 ms | 6.3 | 49 MiB | ok |
| `plain-medium` | 2.08 s | 478 ms | 101.6 | 495 ms | 11.4 | 827 ms | 8.2 | 56 MiB | ok |
| `obfuscated-direct` | — | — | — | — | — | — | — | 47 MiB | **failed** |
| `plain-large` | 2.13 s | 315 ms | 118.9 | 399 ms | 14.9 | 535 ms | 7.5 | 59 MiB | ok |
| `plain-season-pack` | 11.18 s | 331 ms | 34.9 | 136 ms | 2.1 | 793 ms | 9.0 | 90 MiB | ok |
| `rar4-rNN` | 3.41 s | 304 ms | 70.7 | 141 ms | 8.2 | 3.19 s | 8.4 | 114 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 114 MiB | **failed** |
| `rar-hdrenc-large` | 8.20 s | 241 ms | 76.0 | 147 ms | 5.8 | 1.04 s | 10.5 | 144 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 112 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 126 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 126 MiB | **failed** |
| `7z-split-bugonia` | — | — | — | — | — | — | — | 134 MiB | **failed** |
| `7z-split-tardes` | — | — | — | — | — | — | — | 153 MiB | **failed** |
| `damaged-partial` | 2.09 s | 224 ms | — | 353 ms | 2.7 | 843 ms | 11.8 | 153 MiB | ok |

<details><summary>Failures (7)</summary>

- `obfuscated-direct` (core): import failed: failed to process nzb: failed to process NZB archives: no valid files found in NZB
- `rar-hdrenc-small` (core): import failed: failed to process nzb: failed to process NZB archives: all files were skipped due to size or extension restrictions(error file extension not allowed)
- `rar-partNN-large` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-large.nzb: usenet parse failed: failed to stat segment XEQWzFs0v7WfZnBBzUxZ.part01.rar \u003cd3ce1d85f18444998817c8c9f5e4c3fb@ngPost\u003e: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `rar-partNN-maestras` (failure): import failed: failed to process nzb: failed to process NZB archives: no valid files found in NZB
- `rar-partNN-satans` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-satans.nzb: usenet parse failed: failed to stat segment rEN8svtCcJjJ74Q4qpvJf1K8A.part01.rar \u003c4b2b3480fd6543efb4f7bba8e7ccc3b7@ngPost\u003e: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `7z-split-bugonia` (core): import failed: failed to process nzb: content verification failed: head of "7z-split-bugonia.mkv" matches no media container signature: usenet file content is corrupt
- `7z-split-tardes` (core): import failed: failed to process nzb: content verification failed: head of "7z-split-tardes.mkv" matches no media container signature: usenet file content is corrupt

</details>

### nzbdav

`nzbdav` · C# (.NET 10) · version `0c7d8e2` · serving: webdav · runtime: source · startup 1.58 s

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | — | — | — | — | — | — | — | 203 MiB | **failed** |
| `plain-medium` | — | — | — | — | — | — | — | 155 MiB | **failed** |
| `obfuscated-direct` | — | — | — | — | — | — | — | 149 MiB | **failed** |
| `plain-large` | — | — | — | — | — | — | — | 135 MiB | **failed** |
| `plain-season-pack` | — | — | — | — | — | — | — | 142 MiB | **failed** |
| `rar4-rNN` | — | — | — | — | — | — | — | 131 MiB | **failed** |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 133 MiB | **failed** |
| `rar-hdrenc-large` | — | — | — | — | — | — | — | 131 MiB | **failed** |
| `rar-partNN-large` | — | — | — | — | — | — | — | 127 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 124 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 135 MiB | **failed** |
| `7z-split-bugonia` | — | — | — | — | — | — | — | 134 MiB | **failed** |
| `7z-split-tardes` | — | — | — | — | — | — | — | 134 MiB | **failed** |
| `damaged-partial` | — | — | — | — | — | — | — | 131 MiB | **failed** |

<details><summary>Failures (14)</summary>

- `rar4-small` (smoke): timed out after 300000ms waiting for nzbdav import of rar4-small
- `plain-medium` (smoke): timed out after 300000ms waiting for nzbdav import of plain-medium
- `obfuscated-direct` (core): timed out after 300000ms waiting for nzbdav import of obfuscated-direct
- `plain-large` (core): timed out after 300000ms waiting for nzbdav import of plain-large
- `plain-season-pack` (core): timed out after 300000ms waiting for nzbdav import of plain-season-pack
- `rar4-rNN` (core): timed out after 300000ms waiting for nzbdav import of rar4-rNN
- `rar-hdrenc-small` (core): timed out after 300000ms waiting for nzbdav import of rar-hdrenc-small
- `rar-hdrenc-large` (core): timed out after 300000ms waiting for nzbdav import of rar-hdrenc-large
- `rar-partNN-large` (failure): timed out after 300000ms waiting for nzbdav import of rar-partNN-large
- `rar-partNN-maestras` (failure): timed out after 300000ms waiting for nzbdav import of rar-partNN-maestras
- `rar-partNN-satans` (failure): timed out after 300000ms waiting for nzbdav import of rar-partNN-satans
- `7z-split-bugonia` (core): timed out after 300000ms waiting for nzbdav import of 7z-split-bugonia
- `7z-split-tardes` (core): timed out after 300000ms waiting for nzbdav import of 7z-split-tardes
- `damaged-partial` (failure): timed out after 300000ms waiting for nzbdav import of damaged-partial

</details>

### raw NNTP baseline

`raw` · JavaScript (this harness) · version `harness-builtin` · serving: http-range · runtime: source · startup 6 ms

**Own set**: 10 entries, median post 11.3 GiB · click&rarr;byte 376 ms (shared population: 533 ms, 1.42×) · seq 73.2 MB/s · CPU 18.4 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 598 ms | 98 ms | 41.3† | 108 ms | — | — | — | 275 MiB | ok |
| `plain-medium` | 337 ms | 358 ms | 123.5 | 953 ms | 36.7 | 1.23 s | 37.5 | 1649 MiB | ok |
| `obfuscated-direct` | 242 ms | 822 ms | 61.6 | 1.22 s | 7.7 | 2.33 s | 39.9 | 1577 MiB | ok |
| `plain-large` | 274 ms | 259 ms | 83.4 | 324 ms | 81.1 | 607 ms | 14.4 | 1758 MiB | ok |
| `plain-season-pack` | 120 ms | 48 ms | 71.9 | 112 ms | 40.5 | 587 ms | 13.5 | 1765 MiB | ok |
| `rar4-rNN` | 146 ms | 114 ms | 63.0† | 120 ms | — | 424 ms | 30.4 | 395 MiB | ok |
| `rar-hdrenc-small` | 132 ms | 123 ms | 38.8 | 120 ms | 45.8 | 2.00 s | 24.4 | 426 MiB | ok |
| `rar-hdrenc-large` | 169 ms | 138 ms | 74.5 | 124 ms | 104.7 | 598 ms | 18.4 | 449 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 450 MiB | **failed** |
| `rar-partNN-maestras` | 192 ms | 107 ms | — | — | — | — | — | 438 MiB | ok |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 440 MiB | **failed** |
| `7z-split-bugonia` | 345 ms | 101 ms | 92.9 | 117 ms | 93.9 | 530 ms | 14.6 | 462 MiB | ok |
| `7z-split-tardes` | 93 ms | 86 ms | 68.4 | 121 ms | 110.1 | 537 ms | 16.1 | 479 MiB | ok |
| `damaged-partial` | 261 ms | 379 ms | 87.6 | 866 ms | 53.9 | 1.17 s | 32.6 | 1885 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (2)</summary>

- `rar-partNN-large` (failure): first article missing
- `rar-partNN-satans` (failure): first article missing

</details>

### InfiniDysk

`infinidysk` · C# (.NET 10) · version `63bde75` · serving: webdav · runtime: source · startup 4.83 s

**Own set**: 8 entries, median post 17.6 GiB · click&rarr;byte 2.55 s (shared population: 2.13 s, 0.83×) · seq 70.6 MB/s · CPU 18.5 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.96 s | 83 ms | 65.0 | 46 ms | 46.5 | 1.38 s | 29.6 | 431 MiB | ok |
| `plain-medium` | 1.11 s | 83 ms | 48.0 | 349 ms | 2.9 | 2.63 s | 19.2 | 1924 MiB | ok |
| `obfuscated-direct` | 1.74 s | 58 ms | 35.0 | 285 ms | 3.8 | 4.98 s | 18.7 | 2023 MiB | ok |
| `plain-large` | 6.34 s | 494 ms | 135.2† | 214 ms | 25.5 | 6.21 s | 13.7 | 1676 MiB | ok |
| `plain-season-pack` | 2.08 s | 44 ms | 80.8 | 120 ms | 101.5 | 902 ms | 19.2 | 1426 MiB | ok |
| `rar4-rNN` | — | — | — | — | — | — | — | 1278 MiB | **failed** |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 1344 MiB | **failed** |
| `rar-hdrenc-large` | 2.39 s | 581 ms | 89.6 | 193 ms | 45.9 | 1.15 s | 14.8 | 1505 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 880 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 730 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 722 MiB | **failed** |
| `7z-split-bugonia` | 16.65 s | 130 ms | 70.6 | 137 ms | 6.7 | 1.09 s | 18.2 | 1393 MiB | ok |
| `7z-split-tardes` | 10.15 s | 160 ms | 89.5 | 112 ms | 75.3 | 982 ms | 13.7 | 1038 MiB | ok |
| `damaged-partial` | 1.02 s | 69 ms | 42.8 | 186 ms | 2.6 | 6.03 s | 16.5 | 1850 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (5)</summary>

- `rar4-rNN` (core): import failed: Article with message-id 52ab4444$0$9697$6d5eeec5@news.sunnyusenet.com not found. Server responded: Provider returned yEnc part 69/69 for a file with 66 parts.
- `rar-hdrenc-small` (core): import failed: Article with message-id SbVwZiPuDtFfMaMhFpIeNrFf-1737036699496@nyuu not found.
- `rar-partNN-large` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. XEQWzFs0v7WfZnBBzUxZ.part10.rar). NZB is likely DMCA'd or expired.
- `rar-partNN-maestras` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. Su7pfShZJ0uazNFfv70sB3.part02.rar). NZB is likely DMCA'd or expired.
- `rar-partNN-satans` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. rEN8svtCcJjJ74Q4qpvJf1K8A.part04.rar). NZB is likely DMCA'd or expired.

</details>

### AIOStreams

`aiostreams` · TypeScript · version `2026.09.07.2154-nightly` · serving: http-range · runtime: source · startup 14.16 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 826 ms (shared population: 826 ms, 1.00×) · seq 81.3 MB/s · CPU 6.8 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.34 s | 13 ms | 53.2 | 6 ms | — | 282 ms | 24.6 | 814 MiB | ok |
| `plain-medium` | 538 ms | 99 ms | 72.3 | 281 ms | 36.9 | 1.75 s | 5.7 | 1346 MiB | ok |
| `obfuscated-direct` | 539 ms | 3 ms | 83.3 | 5 ms | 62.3 | 1.05 s | 5.0 | 1309 MiB | ok |
| `plain-large` | 810 ms | 16 ms | 84.4 | 171 ms | 26.9 | 2.68 s | 3.8 | 1389 MiB | ok |
| `plain-season-pack` | 321 ms | 8 ms | 107.7 | 116 ms | 57.2 | 1.06 s | 5.9 | 1378 MiB | ok |
| `rar4-rNN` | 1.32 s | 56 ms | 93.8 | 129 ms | 42.2 | 1.20 s | 7.0 | 1380 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 1380 MiB | **failed** |
| `rar-hdrenc-large` | 1.35 s | 13 ms | 81.3 | 114 ms | 71.1 | 1.19 s | 7.0 | 1391 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 1098 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 1099 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 1100 MiB | **failed** |
| `7z-split-bugonia` | 831 ms | 3 ms | 80.6 | 89 ms | 75.7 | 1.27 s | 6.8 | 1118 MiB | ok |
| `7z-split-tardes` | 636 ms | 3 ms | 71.0 | 124 ms | 80.0 | 1.30 s | 6.8 | 1147 MiB | ok |
| `damaged-partial` | 540 ms | 8 ms | 52.1 | 210 ms | 30.1 | 2.57 s | 5.7 | 1442 MiB | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Archive incomplete: volumes missing from the post [incomplete_archive]
- `rar-partNN-large` (failure): import failed: Missing on providers: 8/8 sampled segments unavailable (incomplete or removed) [missing_on_providers]
- `rar-partNN-maestras` (failure): import failed: Missing on providers: 16/16 sampled segments unavailable (incomplete or removed) [missing_on_providers]
- `rar-partNN-satans` (failure): import failed: Missing on providers: 2/21 files unavailable (incomplete or removed) [missing_on_providers]

</details>

### StreamNZB

`streamnzb` · Go · version `3c69f1e` · serving: http-range · runtime: source · startup 1.27 s

**Own set**: 8 entries, median post 17.6 GiB · click&rarr;byte 1.62 s (shared population: 1.65 s, 1.02×) · seq 76.4 MB/s · CPU 7.3 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 13 ms | 2.33 s | 52.8 | 2 ms | — | 327 ms | 7.6 | 390 MiB | ok |
| `plain-medium` | 6 ms | 994 ms | 54.3 | 200 ms | 57.1 | 1.05 s | 6.7 | 615 MiB | ok |
| `obfuscated-direct` | 10 ms | 1.11 s | 80.8 | 113 ms | 78.4 | 1.08 s | 5.7 | 619 MiB | ok |
| `plain-large` | 40 ms | 1.78 s | 56.2 | 169 ms | 62.0 | 834 ms | 6.2 | 629 MiB | ok |
| `plain-season-pack` | 83 ms | 1.50 s | 72.0 | 611 ms | 32.4 | 1.77 s | 7.8 | 630 MiB | ok |
| `rar4-rNN` | 43 ms | — | — | — | — | — | — | 630 MiB | **failed** |
| `rar-hdrenc-small` | 33 ms | — | — | — | — | — | — | 630 MiB | **failed** |
| `rar-hdrenc-large` | 82 ms | 1.57 s | 89.1 | 279 ms | 70.8 | 1.13 s | 8.0 | 653 MiB | ok |
| `rar-partNN-large` | 61 ms | — | — | — | — | — | — | 653 MiB | **failed** |
| `rar-partNN-maestras` | 13 ms | — | — | — | — | — | — | 653 MiB | **failed** |
| `rar-partNN-satans` | 24 ms | — | — | — | — | — | — | 653 MiB | **failed** |
| `7z-split-bugonia` | 78 ms | 1.66 s | 114.7 | 199 ms | 76.8 | 1.08 s | 7.0 | 676 MiB | ok |
| `7z-split-tardes` | 77 ms | 1.05 s | 94.5 | 158 ms | 62.2 | 867 ms | 7.9 | 676 MiB | ok |
| `damaged-partial` | 9 ms | 879 ms | 48.5 | 341 ms | 54.3 | 1.60 s | 7.3 | 794 MiB | ok |

<details><summary>Failures (5)</summary>

- `rar4-rNN` (core): served only 2.4 MB from a 6600 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-hdrenc-small` (core): served only 2.4 MB from a 5554 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-large` (failure): served only 2.4 MB from a 20891 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-maestras` (failure): served only 2.4 MB from a 3743 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-satans` (failure): served only 2.4 MB from a 3335 MB post. That is a placeholder, a sample, or the wrong file, not the media.

</details>

### StremThru (newz)

`stremthru` · Go · version `0.104.1` · serving: http-range · runtime: source · startup 1.53 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.94 s (shared population: 1.02 s, 0.52×) · seq 32.8 MB/s · CPU 13.1 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.90 s | 41 ms | 20.4 | 29 ms | 20.3 | 281 ms | 13.1 | 584 MiB | ok |
| `plain-medium` | 630 ms | 14 ms | 115.3 | 235 ms | 78.6 | 954 ms | 5.4 | 705 MiB | ok |
| `obfuscated-direct` | 105 ms | 15 ms | 2062.9† | 12 ms | 94.7 | 250 ms | 1.7 | 930 MiB | ok |
| `plain-large` | 642 ms | 42 ms | 37.3 | 217 ms | 61.5 | 748 ms | 6.9 | 883 MiB | ok |
| `plain-season-pack` | 943 ms | 75 ms | 37.8 | 191 ms | 20.6 | 1.39 s | 9.4 | 890 MiB | ok |
| `rar4-rNN` | 6.39 s | 193 ms | 28.8 | 369 ms | — | — | 84.0 | 890 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 890 MiB | **failed** |
| `rar-hdrenc-large` | 5.44 s | 170 ms | 36.9 | 376 ms | 25.6 | 1.84 s | 20.4 | 902 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 901 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 897 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 897 MiB | **failed** |
| `7z-split-bugonia` | 5.03 s | 67 ms | 7.8 | 219 ms | 6.6 | 4.33 s | 58.5 | 899 MiB | ok |
| `7z-split-tardes` | 4.71 s | 59 ms | 7.9 | 225 ms | 7.0 | 4.49 s | 57.9 | 899 MiB | ok |
| `damaged-partial` | 633 ms | 23 ms | — | 289 ms | 94.7 | 878 ms | 5.5 | 823 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): newz status failed
- `rar-partNN-large` (failure): newz status failed
- `rar-partNN-maestras` (failure): newz status failed
- `rar-partNN-satans` (failure): newz status failed

</details>

### AltMount

`altmount` · Go · version `v0.3.2-97-g026bd939` · serving: webdav · runtime: source · startup 1.27 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 628 ms (shared population: 775 ms, 1.23×) · seq 80.6 MB/s · CPU 5.5 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 988 ms | 4 ms | 65.0 | 2 ms | — | 372 ms | 5.1 | 391 MiB | ok |
| `plain-medium` | 340 ms | 3 ms | 64.9 | 208 ms | 106.5 | 906 ms | 4.3 | 808 MiB | ok |
| `obfuscated-direct` | 558 ms | 7 ms | 73.4 | 186 ms | 50.7 | 1.97 s | 4.8 | 812 MiB | ok |
| `plain-large` | 771 ms | 4 ms | 93.0 | 126 ms | 61.4 | 1.08 s | 4.8 | 815 MiB | ok |
| `plain-season-pack` | 624 ms | 5 ms | 118.0 | 101 ms | 5.6 | 987 ms | 6.0 | 820 MiB | ok |
| `rar4-rNN` | 1.41 s | 7 ms | 109.5 | 90 ms | 10.1 | 879 ms | 5.5 | 820 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 829 MiB | **failed** |
| `rar-hdrenc-large` | 1.67 s | 11 ms | 62.2 | 181 ms | 43.3 | 1.23 s | 6.5 | 831 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 831 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 831 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 831 MiB | **failed** |
| `7z-split-bugonia` | 590 ms | 8 ms | 86.8 | 96 ms | 85.2 | 513 ms | 5.7 | 832 MiB | ok |
| `7z-split-tardes` | 397 ms | 9 ms | 80.6 | 79 ms | 49.8 | 593 ms | 6.1 | 792 MiB | ok |
| `damaged-partial` | 237 ms | 85 ms | 46.0 | 201 ms | 67.3 | 1.79 s | 4.6 | 897 MiB | ok |

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