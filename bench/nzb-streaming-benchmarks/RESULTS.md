# NZB streaming benchmark (AltMount rerun, 2026-09-05)

This is a local rerun of Viren070's public harness
[nzb-streaming-benchmarks](https://github.com/Viren070/nzb-streaming-benchmarks)
(harness commit `7192cab`) against AltMount `2f9a80d7` (`v0.3.2-68`) and the same field of
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

## AltMount: this run vs the published row

Shape only, since the corpus differs. Published values are AltMount `3ed4c47`
from the 2026-08-28 run.

| Metric | Published (`3ed4c47`) | This run (`2f9a80d7`) |
|---|---:|---:|
| Capability gaps | 7 | 1 |
| Wrongly served | 1 | 0 |
| Click→byte (median) | 3.91 s | 1.18 s |
| Cold TTFB | 234 ms | 62 ms |
| Warm TTFB | 328 ms | 11 ms |
| Full seek | 762 ms | 205 ms |
| Seek TTFB | 384 ms | 121 ms |
| Seq MB/s | 39.4 | 43.9 |
| p05 MB/s | 26.2 | 13.5 |
| CPU s/GiB | 4.2 | 4.7 |
| RSS/item | 766 MiB | 508 MiB |
| Peak RSS | 2026 MiB | 564 MiB |
| Drift | +618 MiB | -89 MiB |

The one remaining gap is `rar-hdrenc-small`: an encrypted RAR whose inner file is a
nested RAR using rar2.9 compression. Every application in the field failed that
entry; only the raw baseline, which serves outer volume bytes, passed it.

Where AltMount does not lead in this run: sequential throughput and p05 sit below
nzbdav/nzbdavex, Decypharr and AIOStreams, and below the raw baseline's 70 MB/s on
the same posts. The link is not the limit there; the cause is not established by
this run.

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

**Run** `2026-09-05T13-48-25-529Z` · started 2026-09-05T13:48:25.529Z · finished 2026-09-05T14:57:44.374Z

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
| **nzbdavex** | docker | C# (.NET 10) | `312d3bc` | webdav | 2.22 s |
| **raw NNTP baseline** | source | JavaScript (this harness) | `harness-builtin` | http-range | 3 ms |
| **AltMount** | source | Go | `v0.3.2-68-g2f9a80d7` | webdav | 778 ms |
| **StreamNZB** | source | Go | `v5.17.0` | http-range | 776 ms |
| **nzbdav** | docker | C# (.NET 10) | `0c7d8e2` | webdav | 1.64 s |
| **Decypharr** | docker | Go | `v2.5` (`0dd1cbb`) | webdav | 1.03 s |
| **StremThru (newz)** | source | Go | `73bf362` | http-range | 559 ms |
| **InfiniDysk** | docker | C# (.NET 10) | `dev` | webdav | 6.32 s |
| **AIOStreams** | source | TypeScript | `2026.09.04.2319-nightly` | http-range | 2.93 s |

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
| **nzbdavex** | 10/11 | 1 | 3/3 | 0 |
| **raw NNTP baseline** | 11/11 | 0 | 3/3 | 0 |
| **AltMount** | 10/11 | 1 | 3/3 | 0 |
| **StreamNZB** | 9/11 | 2 | 3/3 | 0 |
| **nzbdav** | 10/11 | 1 | 3/3 | 0 |
| **Decypharr** | 7/11 | 4 | 3/3 | 0 |
| **StremThru (newz)** | 10/11 | 1 | 3/3 | 0 |
| **InfiniDysk** | 10/11 | 1 | 3/3 | 0 |
| **AIOStreams** | 10/11 | 1 | 3/3 | 0 |

A *capability gap* is the number that ranks engines: entries that should stream
and did not. `raw` is not an application and its row is not a verdict: it serves
outer volume bytes without opening an archive, so it "wrongly serves" entries no
player could open. That is the point of the baseline, not a defect in it.

### Time to picture

| App | n | Click&rarr;byte | Import | Cold TTFB | Warm TTFB |
|---|---:|---:|---:|---:|---:|
| **nzbdavex** | 9/9 | **1.66 s** | 1.51 s | 197 ms | 140 ms |
| **raw NNTP baseline** | 9/9 | **382 ms** | 240 ms | 113 ms | 108 ms |
| **AltMount** | 9/9 | **1.18 s** | 1.09 s | 62 ms | 11 ms |
| **StreamNZB** | 8/9 | **1.48 s** | 45 ms | 1.44 s | 1 ms |
| **nzbdav** | 9/9 | **1.78 s** | 1.62 s | 92 ms | 116 ms |
| **Decypharr** | 6/9 | **5.02 s** | 4.77 s | 292 ms | 1 ms |
| **StremThru (newz)** | 9/9 | **3.49 s** | 3.45 s | 59 ms | 41 ms |
| **InfiniDysk** | 9/9 | **3.24 s** | 3.19 s | 77 ms | 96 ms |
| **AIOStreams** | 9/9 | **1.12 s** | 804 ms | 134 ms | 4 ms |

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
| **nzbdavex** | 92.2 | **66.9** | **368 ms** | 161 ms | 288 ms |
| **raw NNTP baseline** | 70.2 | **42.9** | **336 ms** | 190 ms | 415 ms |
| **AltMount** | 43.9 | **13.5** | **205 ms** | 121 ms | 291 ms |
| **StreamNZB** | 45.1 | **20.7** | **627 ms** | 237 ms | 614 ms |
| **nzbdav** | 80.9 | **53.6** | **379 ms** | 165 ms | 360 ms |
| **Decypharr** | 66.4 | **36.6** | **326 ms** | 134 ms | 450 ms |
| **StremThru (newz)** | 28.5 | **6.2** | **700 ms** | 217 ms | 432 ms |
| **InfiniDysk** | 53.6 | **7.7** | **635 ms** | 133 ms | 260 ms |
| **AIOStreams** | 67.2 | **19.6** | **486 ms** | 121 ms | 198 ms |

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
| **nzbdavex** | **—** | — | — | — |
| **raw NNTP baseline** | **21.9** | 0.9 | 0.9 | 95% |
| **AltMount** | **4.7** | 0.5 | 0.5 | 63% |
| **StreamNZB** | **6.3** | 0.6 | 0.6 | 64% |
| **nzbdav** | **—** | — | — | — |
| **Decypharr** | **—** | — | — | — |
| **StremThru (newz)** | **13.2** | 0.6 | 0.6 | 59% |
| **InfiniDysk** | **—** | — | — | — |
| **AIOStreams** | **6.1** | 0.7 | 0.7 | 86% |

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
| **nzbdavex** | — | **—** | — | 0 entries | — | — |
| **raw NNTP baseline** | 41 MiB | **661 MiB** | 1521 MiB | 14 entries | -794 MiB | 749 MiB |
| **AltMount** | 98 MiB | **508 MiB** | 564 MiB | 14 entries | -89 MiB | 204 MiB |
| **StreamNZB** | 51 MiB | **607 MiB** | 701 MiB | 14 entries | +58 MiB | 491 MiB |
| **nzbdav** | — | **—** | — | 0 entries | — | — |
| **Decypharr** | — | **—** | — | 0 entries | — | — |
| **StremThru (newz)** | 128 MiB | **815 MiB** | 913 MiB | 14 entries | +9 MiB | 684 MiB |
| **InfiniDysk** | — | **—** | — | 0 entries | — | — |
| **AIOStreams** | 227 MiB | **447 MiB** | 667 MiB | 14 entries | -142 MiB | 219 MiB |

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
> nzbdavex, nzbdav, Decypharr, InfiniDysk are not buildable natively here, so they were run
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

| Entry | Tier | nzbdavex | raw NNTP baseline | AltMount | StreamNZB | nzbdav | Decypharr | StremThru (newz) | InfiniDysk | AIOStreams |
|---|---|---|---|---|---|---|---|---|---|---|
| `rar4-small` | smoke | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `plain-medium` | smoke | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `obfuscated-direct` | core | pass | pass | pass | pass | pass | **FAIL** | pass | pass | pass |
| `plain-large` | core | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `plain-season-pack` | core | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `rar4-rNN` | core | pass | pass | pass | **FAIL** | pass | pass | pass | pass | pass |
| `rar-hdrenc-small` | core | **FAIL** | pass | **FAIL** | **FAIL** | **FAIL** | **FAIL** | **FAIL** | **FAIL** | **FAIL** |
| `rar-hdrenc-large` | core | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| `rar-partNN-large` | failure | refused | refused | refused | refused | refused | refused | refused | refused | refused |
| `rar-partNN-maestras` | failure | refused | refused | refused | refused | refused | refused | refused | refused | refused |
| `rar-partNN-satans` | failure | refused | refused | refused | refused | refused | refused | refused | refused | refused |
| `7z-split-bugonia` | core | pass | pass | pass | pass | pass | **FAIL** | pass | pass | pass |
| `7z-split-tardes` | core | pass | pass | pass | pass | pass | **FAIL** | pass | pass | pass |
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

### nzbdavex

`nzbdavex` · C# (.NET 10) · version `312d3bc` · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 2.22 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.66 s (shared population: 1.66 s, 1.00×) · seq 92.2 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.51 s | 149 ms | 42.5 | 93 ms | 28.2 | 1.22 s | — | — | ok |
| `plain-medium` | 564 ms | 285 ms | 92.2 | 453 ms | 64.4 | 1.78 s | — | — | ok |
| `obfuscated-direct` | 557 ms | 834 ms | 102.1 | 555 ms | 8.9 | 2.67 s | — | — | ok |
| `plain-large` | 888 ms | 418 ms | 59.4 | 469 ms | 66.0 | 1.67 s | — | — | ok |
| `plain-season-pack` | 514 ms | 106 ms | 104.8 | 164 ms | 80.7 | 871 ms | — | — | ok |
| `rar4-rNN` | 1.85 s | 153 ms | 91.4 | 118 ms | 51.2 | 804 ms | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 1.87 s | 796 ms | 94.7 | 125 ms | 78.1 | 585 ms | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 2.01 s | 99 ms | 93.6 | 88 ms | 53.9 | 982 ms | — | — | ok |
| `7z-split-tardes` | 1.79 s | 197 ms | 75.1 | 161 ms | 56.0 | 920 ms | — | — | ok |
| `damaged-partial` | 795 ms | 580 ms | 76.2 | 408 ms | 102.5 | 1.37 s | — | — | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Corrupt file. Cannot find byte position 40747732.
- `rar-partNN-large` (failure): import failed: Article with message-id 0d98530cfea9409f8dffdc22a94b396a@ngPost not found.
- `rar-partNN-maestras` (failure): import failed: Article with message-id 86ea8585e28844ea85e01bd23fede30a@ngPost not found.
- `rar-partNN-satans` (failure): import failed: Article with message-id 226c3ffed91942069ed8821bc4980374@ngPost not found.

</details>

### raw NNTP baseline

`raw` · JavaScript (this harness) · version `harness-builtin` · serving: http-range · runtime: source · startup 3 ms

**Own set**: 10 entries, median post 11.3 GiB · click&rarr;byte 354 ms (shared population: 382 ms, 1.08×) · seq 64.8 MB/s · CPU 23.2 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 605 ms | 55 ms | 63.1† | 90 ms | — | — | — | 192 MiB | ok |
| `plain-medium` | 335 ms | 873 ms | 111.4 | 998 ms | 39.4 | 1.36 s | 40.0 | 1521 MiB | ok |
| `obfuscated-direct` | 615 ms | 957 ms | 76.5 | 864 ms | 18.3 | 2.27 s | 38.6 | 1397 MiB | ok |
| `plain-large` | 179 ms | 146 ms | 74.4 | 277 ms | 61.0 | 1.02 s | 16.3 | 1372 MiB | ok |
| `plain-season-pack` | 158 ms | 113 ms | 70.2 | 108 ms | 46.2 | 873 ms | 12.6 | 1050 MiB | ok |
| `rar4-rNN` | 161 ms | 109 ms | 72.9† | 111 ms | — | 468 ms | 26.3 | 794 MiB | ok |
| `rar-hdrenc-small` | 162 ms | 110 ms | 43.1 | 112 ms | 57.4 | 1.97 s | 25.5 | 716 MiB | ok |
| `rar-hdrenc-large` | 83 ms | 105 ms | 59.4 | 103 ms | 106.0 | 506 ms | 17.0 | 606 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 592 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 587 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 588 MiB | **failed** |
| `7z-split-bugonia` | 297 ms | 106 ms | 55.1 | 190 ms | 29.1 | 798 ms | 20.6 | 593 MiB | ok |
| `7z-split-tardes` | 240 ms | 143 ms | 37.8 | 214 ms | 57.2 | 785 ms | 23.2 | 464 MiB | ok |
| `damaged-partial` | 729 ms | 1.01 s | 86.3 | 909 ms | 54.4 | 1.69 s | 37.2 | 1383 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (3)</summary>

- `rar-partNN-large` (failure): first article missing
- `rar-partNN-maestras` (failure): first article missing
- `rar-partNN-satans` (failure): first article missing

</details>

### AltMount

`altmount` · Go · version `v0.3.2-68-g2f9a80d7` · serving: webdav · runtime: source · startup 778 ms

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.18 s (shared population: 1.18 s, 1.00×) · seq 43.9 MB/s · CPU 4.7 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.09 s | 92 ms | 49.2 | 3 ms | 27.6 | 253 ms | 4.2 | 296 MiB | ok |
| `plain-medium` | 328 ms | 88 ms | 46.9 | 612 ms | 51.6 | 2.51 s | 4.7 | 536 MiB | ok |
| `obfuscated-direct` | 868 ms | 61 ms | 103.7 | 169 ms | 57.8 | 2.32 s | 4.1 | 536 MiB | ok |
| `plain-large` | 1.42 s | 62 ms | 40.1 | 119 ms | 42.1 | 1.07 s | 4.5 | 554 MiB | ok |
| `plain-season-pack` | 618 ms | 81 ms | 24.6 | 246 ms | 2.6 | 613 ms | 5.3 | 564 MiB | ok |
| `rar4-rNN` | 5.21 s | 53 ms | 24.6 | 71 ms | 6.5 | 779 ms | 4.5 | 505 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 511 MiB | **failed** |
| `rar-hdrenc-large` | 2.31 s | 55 ms | 79.5 | 132 ms | 7.7 | 506 ms | 5.7 | 530 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 444 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 400 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 341 MiB | **failed** |
| `7z-split-bugonia` | 1.45 s | 73 ms | 43.3 | 99 ms | 7.0 | 508 ms | 5.5 | 444 MiB | ok |
| `7z-split-tardes` | 692 ms | 50 ms | 43.9 | 121 ms | 14.1 | 560 ms | 5.8 | 450 MiB | ok |
| `damaged-partial` | 124 ms | 91 ms | 100.2 | 302 ms | 11.2 | 2.10 s | 17.8 | 548 MiB | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: failed to process nested RAR archives: failed to process nested RAR "b-simpson": compressed media files are not supported: b-simpson.iso (uses rar2.9 compression)
- `rar-partNN-large` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)
- `rar-partNN-maestras` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)
- `rar-partNN-satans` (failure): import failed: fast-fail segment check failed: no regular files were successfully processed (all files failed validation)

</details>

### StreamNZB

`streamnzb` · Go · version `v5.17.0` · serving: http-range · runtime: source · startup 776 ms

**Own set**: 8 entries, median post 17.6 GiB · click&rarr;byte 1.48 s (shared population: 1.48 s, 1.00×) · seq 45.1 MB/s · CPU 6.3 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 6 ms | 4.11 s | 18.0 | 5 ms | 2.9 | 310 ms | 7.0 | 367 MiB | ok |
| `plain-medium` | 9 ms | 1.48 s | 34.3 | 234 ms | 14.5 | 1.23 s | 5.7 | 565 MiB | ok |
| `obfuscated-direct` | 7 ms | 1.02 s | 56.8 | 113 ms | 20.7 | 1.05 s | 5.5 | 560 MiB | ok |
| `plain-large` | 28 ms | 1.17 s | 31.2 | 257 ms | 5.6 | 1.24 s | 6.1 | 595 MiB | ok |
| `plain-season-pack` | 64 ms | 1.50 s | 23.0 | 309 ms | 2.6 | 1.05 s | 7.1 | 607 MiB | ok |
| `rar4-rNN` | 32 ms | — | — | — | — | — | — | 434 MiB | **failed** |
| `rar-hdrenc-small` | 21 ms | — | — | — | — | — | — | 281 MiB | **failed** |
| `rar-hdrenc-large` | 63 ms | 1.41 s | 88.1 | 375 ms | 24.8 | 1.00 s | 6.4 | 636 MiB | ok |
| `rar-partNN-large` | 79 ms | — | — | — | — | — | — | 621 MiB | **failed** |
| `rar-partNN-maestras` | 21 ms | — | — | — | — | — | — | 621 MiB | **failed** |
| `rar-partNN-satans` | 21 ms | — | — | — | — | — | — | 621 MiB | **failed** |
| `7z-split-bugonia` | 64 ms | 3.73 s | 56.0 | 126 ms | 97.6 | 1.07 s | 6.1 | 621 MiB | ok |
| `7z-split-tardes` | 80 ms | 1.31 s | 88.4 | 240 ms | 20.0 | 1.21 s | 6.8 | 607 MiB | ok |
| `damaged-partial` | 17 ms | 1.05 s | 27.8 | 250 ms | 4.8 | 989 ms | 7.8 | 701 MiB | ok |

<details><summary>Failures (5)</summary>

- `rar4-rNN` (core): served only 2.4 MB from a 6600 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-hdrenc-small` (core): served only 2.4 MB from a 5554 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-large` (failure): served only 2.4 MB from a 20891 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-maestras` (failure): served only 2.4 MB from a 3743 MB post. That is a placeholder, a sample, or the wrong file, not the media.
- `rar-partNN-satans` (failure): served only 2.4 MB from a 3335 MB post. That is a placeholder, a sample, or the wrong file, not the media.

</details>

### nzbdav

`nzbdav` · C# (.NET 10) · version `0c7d8e2` · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 1.64 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.78 s (shared population: 1.78 s, 1.00×) · seq 80.9 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.62 s | 154 ms | 28.6 | 112 ms | 35.8 | 815 ms | — | — | ok |
| `plain-medium` | 657 ms | 65 ms | 92.4 | 334 ms | 82.9 | 1.21 s | — | — | ok |
| `obfuscated-direct` | 890 ms | 63 ms | 89.6 | 366 ms | 14.5 | 882 ms | — | — | ok |
| `plain-large` | 876 ms | 64 ms | 109.1 | 342 ms | 2.1 | 876 ms | — | — | ok |
| `plain-season-pack` | 591 ms | 54 ms | 80.9 | 217 ms | 64.4 | 509 ms | — | — | ok |
| `rar4-rNN` | 2.13 s | 114 ms | 101.0 | 135 ms | 23.8 | 843 ms | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 4.53 s | 129 ms | 74.7 | 133 ms | 28.9 | 750 ms | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 1.75 s | 92 ms | 71.3 | 132 ms | 13.2 | 2.60 s | — | — | ok |
| `7z-split-tardes` | 1.77 s | 114 ms | 55.7 | 165 ms | 27.7 | 1.14 s | — | — | ok |
| `damaged-partial` | 547 ms | 62 ms | — | 516 ms | 66.7 | 1.09 s | — | — | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Article with message-id SbVwZiPuDtFfMaMhFpIeNrFf-1737036699496@nyuu not found.
- `rar-partNN-large` (failure): import failed: Article with message-id 0d98530cfea9409f8dffdc22a94b396a@ngPost not found.
- `rar-partNN-maestras` (failure): import failed: Article with message-id 4c30d20d229f498ca1aa5e49bafec4b2@ngPost not found.
- `rar-partNN-satans` (failure): import failed: Article with message-id 849c252510294df2af955f53d2aae08c@ngPost not found.

</details>

### Decypharr

`decypharr` · Go · version `v2.5` (`0dd1cbb`) · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 1.03 s

**Own set**: 6 entries, median post 11.3 GiB · click&rarr;byte 5.02 s (shared population: 5.02 s, 1.00×) · seq 66.4 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 2.55 s | 280 ms | 31.9 | 5 ms | 160.2 | 250 ms | — | — | ok |
| `plain-medium` | 3.09 s | 181 ms | 121.6 | 450 ms | 9.3 | 1.61 s | — | — | ok |
| `obfuscated-direct` | — | — | — | — | — | — | — | — | **failed** |
| `plain-large` | 12.20 s | 332 ms | 26.2 | 230 ms | 13.8 | 540 ms | — | — | ok |
| `plain-season-pack` | 6.20 s | 304 ms | 65.9 | 124 ms | 34.5 | 850 ms | — | — | ok |
| `rar4-rNN` | 3.34 s | 200 ms | 66.9 | 103 ms | 57.5 | 791 ms | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 7.22 s | 328 ms | 66.9 | 145 ms | 4.4 | 1.75 s | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-tardes` | — | — | — | — | — | — | — | — | **failed** |
| `damaged-partial` | 2.10 s | 508 ms | — | 780 ms | 8.8 | 1.86 s | — | — | ok |

<details><summary>Failures (7)</summary>

- `obfuscated-direct` (core): import failed: failed to process nzb: failed to process NZB archives: no valid files found in NZB
- `rar-hdrenc-small` (core): import failed: failed to process nzb: failed to process NZB archives: all files were skipped due to size or extension restrictions(error file extension not allowed)
- `rar-partNN-large` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-large.nzb: usenet parse failed: failed to stat segment XEQWzFs0v7WfZnBBzUxZ.part01.rar \u003cd3ce1d85f18444998817c8c9f5e4c3fb@ngPost\u003e: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `rar-partNN-maestras` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-maestras.nzb: usenet parse failed: failed to stat segment Su7pfShZJ0uazNFfv70sB3.part01.rar \u003cbf4a33b4f2cd47d2829c3c5688da9667@ngPost\u003e: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `rar-partNN-satans` (failure): POST <app>/sabnzbd/api?mode=addfile&output=json&category=bench&cat=bench&action=none -> 500: { "status": false, "error": "Failed to add rar-partNN-satans.nzb: usenet parse failed: failed to stat segment rEN8svtCcJjJ74Q4qpvJf1K8A.part01.rar \u003c4b2b3480fd6543efb4f7bba8e7ccc3b7@ngPost\u003e: all providers failed: NNTP ARTICLE_NOT_FOUND (code 430): No Such Article" }
- `7z-split-bugonia` (core): import failed: failed to process nzb: content verification failed: head of "7z-split-bugonia.mkv" matches no media container signature: usenet file content is corrupt
- `7z-split-tardes` (core): import failed: failed to process nzb: content verification failed: head of "7z-split-tardes.mkv" matches no media container signature: usenet file content is corrupt

</details>

### StremThru (newz)

`stremthru` · Go · version `73bf362` · serving: http-range · runtime: source · startup 559 ms

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 3.49 s (shared population: 3.49 s, 1.00×) · seq 28.5 MB/s · CPU 13.2 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 3.45 s | 43 ms | 18.8 | 32 ms | 22.8 | 305 ms | 13.2 | 585 MiB | ok |
| `plain-medium` | 835 ms | 18 ms | 115.9 | 288 ms | 22.1 | 990 ms | 6.1 | 843 MiB | ok |
| `obfuscated-direct` | 105 ms | 10 ms | 2667.0† | 13 ms | 31.8 | 250 ms | 2.2 | 913 MiB | ok |
| `plain-large` | 637 ms | 38 ms | 75.0 | 213 ms | 19.9 | 1.39 s | 6.4 | 770 MiB | ok |
| `plain-season-pack` | 1.04 s | 76 ms | 35.7 | 211 ms | 2.4 | 12.76 s | 9.4 | 653 MiB | ok |
| `rar4-rNN` | 5.33 s | 240 ms | 23.0 | 483 ms | — | — | 85.8 | 752 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 708 MiB | **failed** |
| `rar-hdrenc-large` | 27.85 s | 191 ms | 34.0 | 422 ms | 1.5 | 1.86 s | 27.6 | 860 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 858 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 829 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 830 MiB | **failed** |
| `7z-split-bugonia` | 8.22 s | 733 ms | 7.3 | 217 ms | 7.2 | 7.65 s | 60.2 | 834 MiB | ok |
| `7z-split-tardes` | 4.37 s | 59 ms | 7.1 | 297 ms | 5.3 | 4.66 s | 61.9 | 519 MiB | ok |
| `damaged-partial` | 630 ms | 21 ms | — | 530 ms | 10.5 | 1.58 s | 7.0 | 801 MiB | ok |

† transfer too short to measure sustained rate (the file fit in flight).

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): newz status failed
- `rar-partNN-large` (failure): newz status failed
- `rar-partNN-maestras` (failure): newz status failed
- `rar-partNN-satans` (failure): newz status failed

</details>

### InfiniDysk

`infinidysk` · C# (.NET 10) · version `dev` · serving: webdav · runtime: docker · CPU/RSS from container cgroups · CPU/RSS not measurable · startup 6.32 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 3.24 s (shared population: 3.24 s, 1.00×) · seq 53.6 MB/s · CPU — s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 24.50 s | 77 ms | 20.8 | 51 ms | 40.3 | 548 ms | — | — | ok |
| `plain-medium` | 1.09 s | 62 ms | 33.1 | 718 ms | 1.6 | 3.21 s | — | — | ok |
| `obfuscated-direct` | 1.21 s | 67 ms | 25.3 | 652 ms | 3.4 | 5.15 s | — | — | ok |
| `plain-large` | 1.08 s | 84 ms | 27.6 | 611 ms | 11.1 | 3.50 s | — | — | ok |
| `plain-season-pack` | 2.44 s | 50 ms | 74.2 | 125 ms | 81.1 | 800 ms | — | — | ok |
| `rar4-rNN` | 3.19 s | 53 ms | 53.6 | 102 ms | — | — | — | — | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | — | **failed** |
| `rar-hdrenc-large` | 5.45 s | 581 ms | 74.3 | 136 ms | 34.1 | 1.08 s | — | — | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | — | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | — | **failed** |
| `7z-split-bugonia` | 16.14 s | 126 ms | 83.9 | 133 ms | 43.7 | 1.05 s | — | — | ok |
| `7z-split-tardes` | 9.43 s | 133 ms | 87.6 | 86 ms | 50.7 | 907 ms | — | — | ok |
| `damaged-partial` | 1.45 s | 75 ms | 28.6 | 583 ms | 2.0 | 13.85 s | — | — | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Article with message-id SbVwZiPuDtFfMaMhFpIeNrFf-1737036699496@nyuu not found.
- `rar-partNN-large` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. XEQWzFs0v7WfZnBBzUxZ.part04.rar). NZB is likely DMCA'd or expired.
- `rar-partNN-maestras` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. Su7pfShZJ0uazNFfv70sB3.part04.rar). NZB is likely DMCA'd or expired.
- `rar-partNN-satans` (failure): import failed: Missing articles: 1 important file(s) have missing segments across all providers (e.g. rEN8svtCcJjJ74Q4qpvJf1K8A.part01.rar). NZB is likely DMCA'd or expired.

</details>

### AIOStreams

`aiostreams` · TypeScript · version `2026.09.04.2319-nightly` · serving: http-range · runtime: source · startup 2.93 s

**Own set**: 9 entries, median post 15.9 GiB · click&rarr;byte 1.12 s (shared population: 1.12 s, 1.00×) · seq 67.2 MB/s · CPU 6.1 s/GiB

Medians over the entries *this application served*, so they are not comparable
across rows. Import and click&rarr;byte scale with post size, so an application
that fails the large entries is credited with the fast medians of the small ones
it survived; the multiplier is the size of that distortion.

| Entry | Import | Cold TTFB | Seq MB/s | Seek TTFB | Playback p05 | To buffer | CPU s/GiB | Peak RSS | Status |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `rar4-small` | 1.34 s | 206 ms | 52.3 | 9 ms | — | 289 ms | 11.0 | 358 MiB | ok |
| `plain-medium` | 534 ms | 315 ms | 96.0 | 279 ms | 27.2 | 1.79 s | 5.7 | 667 MiB | ok |
| `obfuscated-direct` | 537 ms | 6 ms | 67.7 | 7 ms | 21.7 | 1.25 s | 5.3 | 642 MiB | ok |
| `plain-large` | 804 ms | 519 ms | 69.5 | 164 ms | 35.3 | 1.43 s | 4.0 | 598 MiB | ok |
| `plain-season-pack` | 318 ms | 146 ms | 62.0 | 129 ms | 64.4 | 898 ms | 5.5 | 417 MiB | ok |
| `rar4-rNN` | 1.82 s | 95 ms | 67.2 | 109 ms | 44.1 | 1.66 s | 6.1 | 464 MiB | ok |
| `rar-hdrenc-small` | — | — | — | — | — | — | — | 381 MiB | **failed** |
| `rar-hdrenc-large` | 1.10 s | 134 ms | 76.1 | 121 ms | 35.6 | 1.78 s | 7.5 | 434 MiB | ok |
| `rar-partNN-large` | — | — | — | — | — | — | — | 438 MiB | **failed** |
| `rar-partNN-maestras` | — | — | — | — | — | — | — | 326 MiB | **failed** |
| `rar-partNN-satans` | — | — | — | — | — | — | — | 329 MiB | **failed** |
| `7z-split-bugonia` | 1.09 s | 23 ms | 44.6 | 112 ms | 22.7 | 1.50 s | 8.0 | 501 MiB | ok |
| `7z-split-tardes` | 595 ms | 20 ms | 40.5 | 148 ms | 80.3 | 1.34 s | 7.0 | 456 MiB | ok |
| `damaged-partial` | 547 ms | 167 ms | 32.8 | 229 ms | 30.4 | 3.22 s | 6.2 | 593 MiB | ok |

<details><summary>Failures (4)</summary>

- `rar-hdrenc-small` (core): import failed: Archive incomplete: volumes missing from the post [incomplete_archive]
- `rar-partNN-large` (failure): import failed: Missing on providers: 8/8 sampled segments unavailable (incomplete or removed) [missing_on_providers]
- `rar-partNN-maestras` (failure): import failed: Missing on providers: 16/16 sampled segments unavailable (incomplete or removed) [missing_on_providers]
- `rar-partNN-satans` (failure): import failed: Missing on providers: 2/21 files unavailable (incomplete or removed) [missing_on_providers]

</details>

## Corpus used

See `CORPUS.md` for why each entry is in the set.

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