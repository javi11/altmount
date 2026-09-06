# NZB streaming benchmark (AltMount rerun, 2026-09-06)

This is a local rerun of Viren070's public harness
[nzb-streaming-benchmarks](https://github.com/Viren070/nzb-streaming-benchmarks)
(harness commit `7192cab`) against AltMount `3fd79fad` (`v0.3.2-90`) and the same field of
applications the published
[docs/RESULTS.md](https://github.com/Viren070/nzb-streaming-benchmarks/blob/main/docs/RESULTS.md)
measures. The generated report follows unchanged below; `results.json` is the raw
run data and `CORPUS.md` describes the entries.

## How this run differs from the published one

- **Different corpus.** The published NZBs are not distributed, so this run uses a
  14-entry local corpus built with the harness's own `analyze`/`probe`/`select`
  pipeline (see `CORPUS.md`). It covers the same axes (direct video, RAR4/RAR5,
  encrypted headers, split 7z, obfuscation, missing articles, dead posts) but the
  posts, sizes and article counts differ. **Numbers are comparable across the rows
  of this report, not against the published tables.**
- **Different host and link.** Apple M4, 16 GB, macOS, 20 connections to a single
  Newshosting frontend. The published run is a Ryzen 7800X3D on Windows.
- **Four rows ran in Docker.** nzbdav, nzbdavex and InfiniDysk cannot reach the
  provider from source on macOS: .NET rejects the provider's TLS chain with
  `RevocationStatusUnknown`. Decypharr links cgofuse unconditionally and needs FUSE
  headers to compile. All four were run with `--docker`, which the report marks as
  `runtime: docker`. On macOS the harness could not read the container cgroup
  counters, so their CPU and memory columns are empty. Latency and throughput for
  those rows cross an extra NAT hop.
- **Comet is excluded.** Its native usenet engine fails to initialise inside Docker
  Desktop on macOS (`Native engine startup failed: initialization_failure`; it
  requires Landlock from the host kernel). The published run excludes it too.
- **Single pass.** The harness's own caveat applies: treat differences under about
  20% between applications as unresolved by one run.

## AltMount: this run vs the published row vs the prior local rerun

Shape only, since the corpus differs between all three. Published values are
AltMount `3ed4c47` from the 2026-08-28 run; "prior rerun" is AltMount `2f9a80d7`
from the 2026-09-05 local rerun recorded earlier in this file's history.

| Metric | Published (`3ed4c47`) | Prior rerun (`2f9a80d7`) | This run (`3fd79fad`) |
|---|---:|---:|---:|
| Capability gaps | 7 | 1 | 1 |
| Wrongly served | 1 | 0 | 0 |
| Click→byte (median) | 3.91 s | 1.18 s | 1.05 s |
| Cold TTFB | 234 ms | 62 ms | 58 ms |
| Warm TTFB | 328 ms | 11 ms | 6 ms |
| Full seek | 762 ms | 205 ms | 276 ms |
| Seek TTFB | 384 ms | 121 ms | 126 ms |
| Seq MB/s | 39.4 | 43.9 | 67.7 |
| p05 MB/s | 26.2 | 13.5 | 34.4 |
| CPU s/GiB | 4.2 | 4.7 | 5.6 |
| RSS/item | 766 MiB | 508 MiB | 557 MiB |
| Peak RSS | 2026 MiB | 564 MiB | 668 MiB |
| Drift | +618 MiB | -89 MiB | -116 MiB |

The one remaining gap is `rar-hdrenc-small`: an encrypted RAR whose inner file is a
nested RAR using rar2.9 compression. Every application in the field failed that
entry; only the raw baseline, which serves outer volume bytes, passed it. This run
also exercises three added `failure`-tier entries (`rar-partNN-large/maestras/satans`)
that turned out to have a first article missing from the provider itself — confirmed
by the raw baseline and several other apps failing them identically with
`ARTICLE_NOT_FOUND` — so they are a provider/corpus artifact, not an application gap,
and are scored `refused` across the board rather than counted against anyone.

Where AltMount does not lead in this run: sequential throughput and p05 sit below
nzbdav and nzbdavex, though both improved substantially over the prior rerun (seq
43.9→67.7 MB/s, p05 13.5→34.4 MB/s). AltMount has the best full-seek time of the
natively-measured apps and the lowest CPU s/GiB and RSS/item among them.

## Reproducing

```sh
git clone https://github.com/Viren070/nzb-streaming-benchmarks
cd nzb-streaming-benchmarks
ln -s /path/to/altmount apps/altmount        # or let scripts/clone-apps.sh clone it
cp .env.example .env                          # NNTP_HOST/PORT/TLS/USER/PASS/CONNS
# drop NZBs into corpus/pool/, then:
npm run corpus
node src/cli.mjs run --apps=raw,altmount,aiostreams,nzbdav,nzbdavex,infinidysk,stremthru,streamnzb,decypharr \
  --docker=nzbdav,nzbdavex,infinidysk,decypharr
```

On macOS the harness needs a `ps`-based process sampler in `src/metrics/procmon.mjs`
and a null-check on the timeout waiter in `src/nntp/client.mjs`; both patches are
small and upstream-worthy but not yet submitted.

---

## Generated report

**Run** `2026-09-06T15-21-52-327Z` · started 2026-09-06T15:21:52.327Z · finished 2026-09-06T16:29:21.108Z

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
| **StremThru (newz)** | source | Go | `73bf362` | http-range | 1.33 s |
| **AltMount** | source | Go | `v0.3.2-90-g3fd79fad` | webdav | 1.28 s |
| **AIOStreams** | source | TypeScript | `2026.09.04.2319-nightly` | http-range | 14.39 s |
| **raw NNTP baseline** | source | JavaScript (this harness) | `harness-builtin` | http-range | 3 ms |
| **nzbdav** | docker | C# (.NET 10) | `0c7d8e2` | webdav | 4.04 s |
| **StreamNZB** | source | Go | `v5.17.0` | http-range | 775 ms |
| **Decypharr** | docker | Go | `v2.5` (`0dd1cbb`) | webdav | 1.39 s |
| **nzbdavex** | docker | C# (.NET 10) | `312d3bc` | webdav | 1.96 s |
| **InfiniDysk** | docker | C# (.NET 10) | `dev` | webdav | 6.61 s |

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
**application**: the perf-tier entries (`smoke`, `core`, `stress`) that at least
8 of the 9 applications served. Median post size across that set is
15.9 GiB.

> **Why a quorum and not the entries all of them served.** That strict intersection
> is 5 entries here, and it is defined by the weakest application in the field:
> one broken engine collapses the population for everybody, and the set moves between
> runs as the field changes. A quorum keeps it wide and stable. Where an application
> missed one of the 9, its `n` column says so.

Entries: `rar4-small`, `plain-medium`, `obfuscated-direct`, `plain-large`, `plain-season-pack`, `rar4-rNN`, `rar-hdrenc-large`, `7z-split-bugonia`, `7z-split-tardes`.

### Verdict

*Correct* is not *served*. Six corpus entries are built to be unservable: three
`negative` (compressed archives, no password) and three `failure` (dead post,
severe damage, missing volumes). Refusing those is the right answer, and serving
one means emitting bytes that cannot be the media, which is a worse result than
refusing, not a better one.

| App | Served | Capability gaps | Correctly refused | **Wrongly served** |
|---|---:|---:|---:|---:|
| **StremThru (newz)** | 10/11 | 1 | 3/3 | 0 |
| **AltMount** | 10/11 | 1 | 3/3 | 0 |
| **AIOStreams** | 10/11 | 1 | 3/3 | 0 |
| **raw NNTP baseline** | 11/11 | 0 | 3/3 | 0 |
| **nzbdav** | 10/11 | 1 | 3/3 | 0 |
| **StreamNZB** | 9/11 | 2 | 3/3 | 0 |
| **Decypharr** | 7/11 | 4 | 3/3 | 0 |
| **nzbdavex** | 10/11 | 1 | 3/3 | 0 |
| **InfiniDysk** | 10/11 | 1 | 3/3 | 0 |

A *capability gap* is the number that ranks engines: entries that should stream
and did not. `raw` is not an application and its row is not a verdict: it serves
outer volume bytes without opening an archive, so it "wrongly serves" entries no
player could open. That is the point of the baseline, not a defect in it.

### Time to picture

| App | n | Click&rarr;byte | Import | Cold TTFB | Warm TTFB |
|---|---:|---:|---:|---:|---:|
| **StremThru (newz)** | 9/9 | **1.96 s** | 1.91 s | 72 ms | 38 ms |
| **AltMount** | 9/9 | **1.05 s** | 998 ms | 58 ms | 6 ms |
| **AIOStreams** | 9/9 | **1.05 s** | 790 ms | 106 ms | 3 ms |
| **raw NNTP baseline** | 9/9 | **335 ms** | 200 ms | 118 ms | 211 ms |
| **nzbdav** | 9/9 | **1.85 s** | 1.75 s | 99 ms | 141 ms |
| **StreamNZB** | 8/9 | **1.52 s** | 45 ms | 1.48 s | 1 ms |
| **Decypharr** | 6/9 | **5.66 s** | 5.44 s | 260 ms | 2 ms |
| **nzbdavex** | 9/9 | **1.64 s** | 1.52 s | 117 ms | 110 ms |
| **InfiniDysk** | 9/9 | **3.19 s** | 2.55 s | 60 ms | 248 ms |

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
| **StremThru (newz)** | 34.6 | **24.7** | **718 ms** | 253 ms | 417 ms |
| **AltMount** | 67.7 | **34.4** | **276 ms** | 126 ms | 329 ms |
| **AIOStreams** | 70.3 | **32.3** | **498 ms** | 124 ms | 231 ms |
| **raw NNTP baseline** | 62.5 | **33.9** | **209 ms** | 124 ms | 137 ms |
| **nzbdav** | 85.0 | **56.5** | **469 ms** | 200 ms | 451 ms |
| **StreamNZB** | 61.6 | **39.4** | **592 ms** | 230 ms | 526 ms |
| **Decypharr** | 29.7 | **2.3** | **383 ms** | 131 ms | 367 ms |
| **nzbdavex** | 75.1 | **40.4** | **402 ms** | 179 ms | 235 ms |
| **InfiniDysk** | 56.1 | **33.7** | **620 ms** | 117 ms | 264 ms |

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
| **StremThru (newz)** | **13.1** | 0.5 | 0.7 | 80% |
| **AltMount** | **5.6** | 0.6 | 0.6 | 84% |
| **AIOStreams** | **7.8** | 0.7 | 0.7 | 86% |
| **raw NNTP baseline** | **18.2** | 1.0 | 1.0 | 93% |
| **nzbdav** | **—** | — | — | — |
| **StreamNZB** | **7.6** | 0.7 | 0.7 | 73% |
| **Decypharr** | **—** | — | — | — |
| **nzbdavex** | **—** | — | — | — |
| **InfiniDysk** | **—** | — | — | — |

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
| **StremThru (newz)** | 223 MiB | **801 MiB** | 953 MiB | 14 entries | +39 MiB | 581 MiB |
| **AltMount** | 99 MiB | **557 MiB** | 668 MiB | 14 entries | -116 MiB | 531 MiB |
| **AIOStreams** | 667 MiB | **627 MiB** | 994 MiB | 11 entries | -179 MiB | 531 MiB |
| **raw NNTP baseline** | 222 MiB | **1285 MiB** | 1849 MiB | 14 entries | -469 MiB | 1187 MiB |
| **nzbdav** | — | **—** | — | 0 entries | — | — |
| **StreamNZB** | 51 MiB | **600 MiB** | 725 MiB | 14 entries | +39 MiB | 546 MiB |
| **Decypharr** | — | **—** | — | 0 entries | — | — |
| **nzbdavex** | — | **—** | — | 0 entries | — | — |
| **InfiniDysk** | — | **—** | — | 0 entries | — | — |

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

> **`runtime: docker` rows were measured in a container, not on this host.**
> nzbdav, Decypharr, nzbdavex, InfiniDysk are not buildable natively here, so they were run
> under Docker with `--docker`. The CPU and memory columns are real numbers, read
> from the daemon's cgroup counters rather than guessed, but they describe a process
> inside a Linux VM: the CPU is the VM's share of this machine, and every byte
> crosses an extra NAT hop on the way in.
>
> Compare container rows with each other freely. Against a native row, read them as
> indicative: a container row that is slower is not proof the application is.

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

| Entry | Tier | StremThru (newz) | AltMount | AIOStreams | raw NNTP baseline | nzbdav | StreamNZB | Decypharr | nzbdavex | InfiniDysk |
|---|---|---|---|---|---|---|---|---|---|---|
| `rar4-small` | smoke | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `plain-medium` | smoke | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `obfuscated-direct` | core | pass | pass | pass | pass | pass | pass | **FAIL** | pass | pass |
| `plain-large` | core | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `plain-season-pack` | core | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `rar4-rNN` | core | pass | pass | pass | pass | pass | **FAIL** | pass | pass | pass |
| `rar-hdrenc-small` | core | **FAIL** | **FAIL** | **FAIL** | pass | **FAIL** | **FAIL** | **FAIL** | **FAIL** | **FAIL** |
| `rar-hdrenc-large` | core | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `rar-partNN-large` | failure | refused | refused | refused | refused | refused | refused | refused | refused | refused |
| `rar-partNN-maestras` | failure | refused | refused | refused | refused | refused | refused | refused | refused | refused |
| `rar-partNN-satans` | failure | refused | refused | refused | refused | refused | refused | refused | refused | refused |
| `7z-split-bugonia` | core | pass | pass | pass | pass | pass | pass | **FAIL** | pass | pass |
| `7z-split-tardes` | core | pass | pass | pass | pass | pass | pass | **FAIL** | pass | pass |
| `damaged-partial` | failure | pass | pass | pass | pass | pass | pass | pass | pass | pass |

## Byte-identity cross-check

The same byte ranges hashed by every application and compared **against each**
**other**, since a fast application serving the wrong bytes is not fast. The
consensus hash is the one at least two applications agree on; a row that differs
is the one to investigate.

`raw` participates only on `direct-video` entries: for archived posts it serves
the outer volume stream rather than the assembled inner file, so it is not a
valid reference there.

**Every application agreed on every comparable range** (10 entries, 81 app-entry pairs).

## Per-entry detail

### StremThru (newz)

`stremthru` · Go · version `73bf362` · serving: http-range · runtime: source · startup 1.33 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.96 s (shared population: 1.96 s, 1.00×) · seq 34.6 MB/s · CPU 13.1 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.91 s | 56 ms | 18.8 | 34 ms | 14.9 | 271 ms | 13.1 | 571 MiB | ok |
| `plain-medium` | 734 ms | 28 ms | 78.7 | 377 ms | 69.0 | 1.10 s | 6.1 | 709 MiB | ok |
| `obfuscated-direct` | 113 ms | 10 ms | 1938.4† | 13 ms | 73.2 | 250 ms | 2.3 | 953 MiB | ok |
| `plain-large` | 737 ms | 37 ms | 60.8 | 214 ms | 47.0 | 940 ms | 7.2 | 774 MiB | ok |
| `plain-season-pack` | 1.17 s | 73 ms | 34.2 | 228 ms | 20.7 | 1.51 s | 10.4 | 540 MiB | ok |
| `rar4-rNN` | 5.95 s | 201 ms | 34.9 | 436 ms | — | — | 80.7 | 827 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 754 MiB | **failed** |
| `rar-hdrenc-large` | 5.73 s | 181 ms | 38.8 | 397 ms | 22.5 | 1.98 s | 23.3 | 839 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 836 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 839 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 842 MiB | **failed** |
| `7z-split-bugonia` | 6.03 s | 72 ms | 6.6 | 253 ms | 4.2 | 4.69 s | 64.4 | 858 MiB | ok |
| `7z-split-tardes` | 4.67 s | 75 ms | 5.6 | 305 ms | 4.7 | 5.79 s | 66.7 | 518 MiB | ok |
| `damaged-partial` | 630 ms | 25 ms | — | 329 ms | 65.2 | 1.08 s | 6.1 | 719 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): newz status failed
- `rar-partNN-large` (failure): newz status failed
- `rar-partNN-maestras` (failure): newz status failed
- `rar-partNN-satans` (failure): newz status failed

</details>

### AltMount

`altmount` · Go · version `v0.3.2-90-g3fd79fad` · serving: webdav · runtime: source · startup 1.28 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.05 s (shared population: 1.05 s, 1.00×) · seq 67.7 MB/s · CPU 5.6 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.09 s | 45 ms | 38.4 | 2 ms | 34.4 | 253 ms | 5.1 | 414 MiB | ok |
| `plain-medium` | 232 ms | 87 ms | 67.7 | 236 ms | 42.6 | 2.50 s | 4.9 | 650 MiB | ok |
| `obfuscated-direct` | 972 ms | 2 ms | 86.1 | 397 ms | 72.1 | 1.26 s | 4.4 | 668 MiB | ok |
| `plain-large` | 998 ms | 49 ms | 69.0 | 181 ms | 50.9 | 1.67 s | 4.9 | 570 MiB | ok |
| `plain-season-pack` | 620 ms | 76 ms | 79.9 | 95 ms | 8.9 | 689 ms | 5.8 | 579 MiB | ok |
| `rar4-rNN` | 1.63 s | 53 ms | 66.9 | 121 ms | 59.0 | 682 ms | 5.6 | 588 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 544 MiB | **failed** |
| `rar-hdrenc-large` | 1.58 s | 58 ms | 66.0 | 169 ms | 55.2 | 510 ms | 6.9 | 572 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 505 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 473 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 471 MiB | **failed** |
| `7z-split-bugonia` | 2.94 s | 66 ms | 15.8 | 126 ms | 3.4 | 2.73 s | 8.2 | 489 MiB | ok |
| `7z-split-tardes` | 715 ms | 70 ms | 89.2 | 120 ms | 56.3 | 1.03 s | 7.2 | 500 MiB | ok |
| `damaged-partial` | 241 ms | 87 ms | 51.6 | 142 ms | 32.9 | 1.25 s | 21.3 | 643 MiB | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: failed to process nested RAR archives: failed to process nested RAR "b-simpson": compressed media files are not supported: b-simpson.iso (uses rar2.9 compression)
- `rar-partNN-large` (failure): import failed: fast-fail segment check inconclusive: fast-fail validation inconclusive: 64 segment(s) remained unverified after 3 attempts: context deadline exceeded
- `rar-partNN-maestras` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)
- `rar-partNN-satans` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)

</details>

### AIOStreams

`aiostreams` · TypeScript · version `2026.09.04.2319-nightly` · serving: http-range · runtime: source · startup 14.39 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.05 s (shared population: 1.05 s, 1.00×) · seq 70.3 MB/s · CPU 7.8 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.61 s | 106 ms | 55.2 | 5 ms | 71.1 | 295 ms | 23.6 | 672 MiB | ok |
| `plain-medium` | 790 ms | 259 ms | 73.4 | 267 ms | 8.0 | 1.55 s | 7.5 | 994 MiB | ok |
| `obfuscated-direct` | 551 ms | 6 ms | 52.5 | 6 ms | 3.5 | 1.26 s | 9.7 | 715 MiB | ok |
| `plain-large` | 1.32 s | 251 ms | 72.2 | 216 ms | 47.0 | 2.19 s | 4.2 | 685 MiB | ok |
| `plain-season-pack` | 332 ms | 110 ms | 72.4 | 109 ms | 49.9 | 913 ms | 6.8 | 627 MiB | ok |
| `rar4-rNN` | 1.57 s | 124 ms | 70.3 | 124 ms | 45.2 | 1.03 s | 7.8 | 546 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 503 MiB | **failed** |
| `rar-hdrenc-large` | 1.35 s | 106 ms | 69.7 | 129 ms | 22.8 | 1.63 s | 8.3 | 474 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 540 ms | 38 ms | 78.5 | 117 ms | 40.1 | 1.45 s | 7.2 | 536 MiB | ok |
| `7z-split-tardes` | 585 ms | 21 ms | 69.9 | 134 ms | 52.8 | 1.47 s | 8.0 | 532 MiB | ok |
| `damaged-partial` | 540 ms | 163 ms | 60.3 | 299 ms | 51.5 | 2.46 s | 6.6 | 882 MiB | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Archive incomplete: volumes missing from the post [incomplete_archive]
- `rar-partNN-large` (failure): library/upload -> 500: {"success":false,"detail":null,"data":null,"error":{"code":"INTERNAL_SERVER_ERROR","message":"An unexpected error occurred"}}
- `rar-partNN-maestras` (failure): library/upload -> 500: {"success":false,"detail":null,"data":null,"error":{"code":"INTERNAL_SERVER_ERROR","message":"An unexpected error occurred"}}
- `rar-partNN-satans` (failure): library/upload -> 500: {"success":false,"detail":null,"data":null,"error":{"code":"INTERNAL_SERVER_ERROR","message":"An unexpected error occurred"}}

</details>

### raw NNTP baseline

`raw` · JavaScript (this harness) · version `harness-builtin` · serving: http-range · runtime: source · startup 3 ms

**Own set**: 10 entries, median post 11.3 GiB · click&rarr;byte 303 ms (shared population: 335 ms, 1.11×) · seq 62.2 MB/s · CPU 18.7 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 600 ms | 118 ms | 35.2† | 103 ms | — | — | — | 280 MiB | ok |
| `plain-medium` | 466 ms | 886 ms | 84.0 | 913 ms | 61.5 | 2.06 s | 38.5 | 1679 MiB | ok |
| `obfuscated-direct` | 457 ms | 878 ms | 98.3 | 784 ms | 51.9 | 1.39 s | 38.3 | 1849 MiB | ok |
| `plain-large` | 200 ms | 135 ms | 45.5 | 270 ms | 57.9 | 812 ms | 16.6 | 1583 MiB | ok |
| `plain-season-pack` | 132 ms | 97 ms | 71.0 | 146 ms | 35.4 | 1.11 s | 14.9 | 1554 MiB | ok |
| `rar4-rNN` | 111 ms | 112 ms | 74.6† | 111 ms | — | 483 ms | 33.0 | 1300 MiB | ok |
| `rar-hdrenc-small` | 143 ms | 102 ms | 44.9 | 157 ms | 4.4 | 1.90 s | 27.2 | 1300 MiB | ok |
| `rar-hdrenc-large` | 138 ms | 133 ms | 62.0 | 124 ms | 113.6 | 569 ms | 17.7 | 1270 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 1163 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 1164 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 1164 MiB | **failed** |
| `7z-split-bugonia` | 343 ms | 98 ms | 46.2 | 107 ms | 93.1 | 601 ms | 18.7 | 1161 MiB | ok |
| `7z-split-tardes` | 147 ms | 109 ms | 62.5 | 116 ms | 115.0 | 580 ms | 15.7 | 903 MiB | ok |
| `damaged-partial` | 267 ms | 150 ms | 115.5 | 842 ms | 10.7 | 1.67 s | 34.0 | 1767 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (3)</summary>

- `rar-partNN-large` (failure): first article missing
- `rar-partNN-maestras` (failure): first article missing
- `rar-partNN-satans` (failure): first article missing

</details>

### nzbdav

`nzbdav` · C# (.NET 10) · version `0c7d8e2` · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 4.04 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.85 s (shared population: 1.85 s, 1.00×) · seq 85.0 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 2.33 s | 109 ms | 34.0 | 108 ms | 24.6 | 791 ms | — | — | ok |
| `plain-medium` | 554 ms | 61 ms | 93.0 | 794 ms | 109.9 | 1.19 s | — | — | ok |
| `obfuscated-direct` | 556 ms | 61 ms | 88.7 | 467 ms | 52.1 | 1.20 s | — | — | ok |
| `plain-large` | 666 ms | 121 ms | 113.4 | 323 ms | 17.9 | 1.09 s | — | — | ok |
| `plain-season-pack` | 1.13 s | 65 ms | 71.4 | 482 ms | 41.4 | 500 ms | — | — | ok |
| `rar4-rNN` | 2.36 s | 106 ms | 72.3 | 193 ms | 39.6 | 1.04 s | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 2.77 s | 143 ms | 72.0 | 126 ms | 35.8 | 521 ms | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 1.76 s | 91 ms | 110.6 | 136 ms | 60.8 | 760 ms | — | — | ok |
| `7z-split-tardes` | 1.75 s | 99 ms | 85.0 | 200 ms | 35.0 | 835 ms | — | — | ok |
| `damaged-partial` | 554 ms | 70 ms | — | 1.03 s | 67.8 | 1.85 s | — | — | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Article with message-id SbVwZiPuDtFfMaMhFpIeNrFf-1737036699496@nyuu not found.
- `rar-partNN-large` (failure): import failed: Article with message-id 0d98530cfea9409f8dffdc22a94b396a@ngPost not found.
- `rar-partNN-maestras` (failure): import failed: Article with message-id 86ea8585e28844ea85e01bd23fede30a@ngPost not found.
- `rar-partNN-satans` (failure): import failed: Article with message-id 6ffb61e249294817a52c2b422ea5a8ab@ngPost not found.

</details>

### StreamNZB

`streamnzb` · Go · version `v5.17.0` · serving: http-range · runtime: source · startup 775 ms

**Own set**: 8 entries, median post 17.6 GiB · click&rarr;byte 1.52 s (shared population: 1.52 s, 1.00×) · seq 61.6 MB/s · CPU 7.6 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 14 ms | 2.17 s | 66.8 | 2 ms | 49.8 | 353 ms | 7.6 | 347 MiB | ok |
| `plain-medium` | 7 ms | 1.35 s | 43.0 | 216 ms | 66.4 | 1.35 s | 6.8 | 599 MiB | ok |
| `obfuscated-direct` | 13 ms | 1.16 s | 55.0 | 155 ms | 46.7 | 1.17 s | 6.3 | 579 MiB | ok |
| `plain-large` | 38 ms | 1.98 s | 43.9 | 129 ms | 10.3 | 1.55 s | 7.6 | 597 MiB | ok |
| `plain-season-pack` | 81 ms | 917 ms | 56.4 | 296 ms | 37.5 | 1.15 s | 8.0 | 581 MiB | ok |
| `rar4-rNN` | 51 ms | — | — | — | — | — | — | 577 MiB | **failed** |
| `rar-hdrenc-small` | 21 ms | — | — | — | — | — | — | 577 MiB | **failed** |
| `rar-hdrenc-large` | 86 ms | 1.60 s | 79.1 | 254 ms | 51.6 | 1.35 s | 8.5 | 640 MiB | ok |
| `rar-partNN-large` | 75 ms | — | — | — | — | — | — | 620 MiB | **failed** |
| `rar-partNN-maestras` | 23 ms | — | — | — | — | — | — | 615 MiB | **failed** |
| `rar-partNN-satans` | 13 ms | — | — | — | — | — | — | 615 MiB | **failed** |
| `7z-split-bugonia` | 81 ms | 1.82 s | 95.3 | 393 ms | 65.0 | 1.40 s | 7.6 | 640 MiB | ok |
| `7z-split-tardes` | 52 ms | 1.14 s | 76.5 | 244 ms | 11.6 | 1.03 s | 8.8 | 602 MiB | ok |
| `damaged-partial` | 10 ms | 1.28 s | 39.2 | 182 ms | 59.8 | 1.25 s | 6.7 | 725 MiB | ok |

<details><summary>Failures (5)</summary>

- `rar4-rNN` (core): served only 2.4 MB from a 6600 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-hdrenc-small` (core): served only 2.4 MB from a 5554 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-large` (failure): served only 2.4 MB from a 20891 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-maestras` (failure): served only 2.4 MB from a 3743 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-satans` (failure): served only 2.4 MB from a 3335 MB post. That is a placeholder, a sample, or the wrong file, not the media.

</details>

### Decypharr

`decypharr` · Go · version `v2.5` (`0dd1cbb`) · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 1.39 s

**Own set**: 6 entries, median post 11.3 GiB · click&rarr;byte 5.66 s (shared population: 5.66 s, 1.00×) · seq 29.7 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 7.44 s | 259 ms | 13.1 | 3 ms | 124.9 | 250 ms | — | — | ok |
| `plain-medium` | 2.07 s | 403 ms | 84.0 | 586 ms | 4.4 | 1.43 s | — | — | ok |
| `obfuscated-direct` | — | — | — | — | — | — | — | — | **failed** |
| `plain-large` | 3.12 s | 178 ms | 28.8 | 242 ms | 9.5 | 1.16 s | — | — | ok |
| `plain-season-pack` | 16.33 s | 261 ms | 30.6 | 129 ms | 6.7 | 1.28 s | — | — | ok |
| `rar4-rNN` | 3.43 s | 199 ms | 65.4 | 134 ms | 6.5 | 612 ms | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 9.26 s | 278 ms | 21.0 | 120 ms | 3.8 | 997 ms | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-tardes` | — | — | — | — | — | — | — | — | **failed** |
| `damaged-partial` | 2.09 s | 211 ms | — | 463 ms | 7.9 | 4.17 s | — | — | ok |

<details><summary>Failures (7)</summary>

- `obfuscated-direct` (core): import failed: failed to process nzb: failed to process NZB archives: no valid files found in NZB
- `rar-hdrenc-small` (core): import failed: failed to process nzb: failed to process NZB archives: all files were skipped due to size or extension restrictions(error file extension not allowed)
- `rar-partNN-large` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-large.nzb: usenet parse failed: failed to stat segment XEQWzFs0v7WfZnBBzUxZ.part01.rar <d3ce1d85f18444998817c8c9f5e4c3fb@ngPost>: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `rar-partNN-maestras` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-maestras.nzb: usenet parse failed: failed to stat segment Su7pfShZJ0uazNFfv70sB3.part01.rar <bf4a33b4f2cd47d2829c3c5688da9667@ngPost>: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `rar-partNN-satans` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-satans.nzb: usenet parse failed: failed to stat segment rEN8svtCcJjJ74Q4qpvJf1K8A.part01.rar <4b2b3480fd6543efb4f7bba8e7ccc3b7@ngPost>: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `7z-split-bugonia` (core): import failed: failed to process nzb: content verification failed: head of "7z-split-bugonia.mkv" matches no media container signature: usenet file content is corrupt
- `7z-split-tardes` (core): import failed: failed to process nzb: content verification failed: head of "7z-split-tardes.mkv" matches no media container signature: usenet file content is corrupt

</details>

### nzbdavex

`nzbdavex` · C# (.NET 10) · version `312d3bc` · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 1.96 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.64 s (shared population: 1.64 s, 1.00×) · seq 75.1 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.52 s | 111 ms | 41.0 | 98 ms | 32.3 | 821 ms | — | — | ok |
| `plain-medium` | 666 ms | 340 ms | 64.8 | 434 ms | 29.3 | 2.04 s | — | — | ok |
| `obfuscated-direct` | 780 ms | 842 ms | 127.9 | 303 ms | 63.8 | 1.58 s | — | — | ok |
| `plain-large` | 1.13 s | 236 ms | 85.9 | 296 ms | 84.8 | 1.24 s | — | — | ok |
| `plain-season-pack` | 934 ms | 90 ms | 81.3 | 179 ms | 64.0 | 1.02 s | — | — | ok |
| `rar4-rNN` | 1.74 s | 65 ms | 60.7 | 159 ms | 35.7 | 1.51 s | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 2.00 s | 777 ms | 75.1 | 129 ms | 17.2 | 790 ms | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 2.10 s | 103 ms | 72.1 | 127 ms | 46.8 | 967 ms | — | — | ok |
| `7z-split-tardes` | 2.02 s | 117 ms | 86.2 | 241 ms | 48.8 | 871 ms | — | — | ok |
| `damaged-partial` | 898 ms | 164 ms | 93.2 | 406 ms | 33.4 | 2.02 s | — | — | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Corrupt file. Cannot find byte position 40747732.
- `rar-partNN-large` (failure): import failed: Article with message-id 0d98530cfea9409f8dffdc22a94b396a@ngPost not found.
- `rar-partNN-maestras` (failure): import failed: Article with message-id 86ea8585e28844ea85e01bd23fede30a@ngPost not found.
- `rar-partNN-satans` (failure): import failed: Article with message-id 226c3ffed91942069ed8821bc4980374@ngPost not found.

</details>

### InfiniDysk

`infinidysk` · C# (.NET 10) · version `dev` · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 6.61 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 3.19 s (shared population: 3.19 s, 1.00×) · seq 56.1 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 2.30 s | 79 ms | 56.1 | 50 ms | 29.4 | 879 ms | — | — | ok |
| `plain-medium` | 1.21 s | 58 ms | 37.4 | 412 ms | 4.0 | 4.22 s | — | — | ok |
| `obfuscated-direct` | 1.01 s | 60 ms | 23.2 | 396 ms | 4.0 | 2.40 s | — | — | ok |
| `plain-large` | 890 ms | 57 ms | 48.8 | 644 ms | 28.3 | 2.91 s | — | — | ok |
| `plain-season-pack` | 3.75 s | 51 ms | 86.1 | 107 ms | 52.4 | 775 ms | — | — | ok |
| `rar4-rNN` | 3.57 s | 56 ms | 55.6 | 117 ms | — | — | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 2.55 s | 640 ms | 72.0 | 139 ms | 4.5 | 1.56 s | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 16.31 s | 129 ms | 71.8 | 84 ms | 52.4 | 989 ms | — | — | ok |
| `7z-split-tardes` | 9.95 s | 176 ms | 85.5 | 115 ms | 32.2 | 1.24 s | — | — | ok |
| `damaged-partial` | 1.45 s | 165 ms | 59.3 | 1.30 s | 6.8 | 4.15 s | — | — | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Article with message-id SbVwZiPuDtFfMaMhFpIeNrFf-1737036699496@nyuu not found.
- `rar-partNN-large` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. XEQWzFs0v7WfZnBBzUxZ.part16.rar). NZB is likely DMCA'd or expired.
- `rar-partNN-maestras` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. Su7pfShZJ0uazNFfv70sB3.part01.rar). NZB is likely DMCA'd or expired.
- `rar-partNN-satans` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. rEN8svtCcJjJ74Q4qpvJf1K8A.part01.rar). NZB is likely DMCA'd or expired.

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
