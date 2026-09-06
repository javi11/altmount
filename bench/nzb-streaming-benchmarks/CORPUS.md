# NZB corpus

14 NZBs selected from a pool of 14, chosen for **characteristic
coverage** rather than title. Every classification below was verified against live NNTP by
`src/corpus/probe.mjs` (archive headers actually fetched and parsed), not inferred from
filenames, which are frequently obfuscated and carry stale inline comments.

The NZBs themselves are gitignored. Regenerate with:

```
node src/corpus/analyze.mjs     # static structure     -> corpus/analysis.json
node src/corpus/probe.mjs       # live archive probing -> corpus/probe.json
node src/corpus/select.mjs      # curate + manifest    -> corpus/corpus.json + this file
```

Disc collections (BDMV/ISO packs) were deliberately excluded: they are not streaming
scenarios.

## Coverage

| Axis | Entries |
|---|---|
| `password-in-nzb` | 7 |
| `direct-video` | 5 |
| `partNN` | 4 |
| `rar` | 3 |
| `7z` | 2 |
| `encrypted-headers` | 2 |
| `no-archive` | 2 |
| `rar4` | 2 |
| `rar5` | 2 |
| `split-7z.NNN` | 2 |
| `stored` | 2 |
| `extensionless` | 1 |
| `file-selection` | 1 |
| `letter-rollover` | 1 |
| `missing-articles` | 1 |
| `named-volumes` | 1 |
| `obfuscated-names` | 1 |
| `per-file-unique-stems` | 1 |
| `rar+rNN` | 1 |
| `season-pack` | 1 |

## smoke

Small and healthy. A full pass is cheap, so use these while iterating.

### `rar4-small`

Small stored RAR4 partNN set; cheap archive control.

- **Axes**: `rar4`, `stored`, `named-volumes`, `partNN`
- **Verified**: rar4 · stored
- **Size**: 0.4 GiB posted · 17 files · 1,010 segments · NZB 0.1 MB
- **Health**: 16/16 sampled articles present
- **Source**: `FatherBrown.nzb`
- **sha256**: `55ed581204b3d83b…`

### `plain-medium`

Direct MKV at movie size, no archive layer.

- **Axes**: `direct-video`, `no-archive`, `per-file-unique-stems`
- **Verified**: matroska
- **Size**: 6.7 GiB posted · 7 files · 1,662 segments · NZB 0.2 MB
- **Health**: 16/16 sampled articles present
- **Obfuscation**: `per-file-unique-stems`
- **Source**: `ToyStory5.nzb`
- **sha256**: `9add0ee889f54187…`


## core

The main capability and performance matrix.

### `obfuscated-direct`

Same post fully obfuscated: extensionless names, opaque subjects.

- **Axes**: `direct-video`, `obfuscated-names`, `extensionless`
- **Verified**: unknown
- **Size**: 6.7 GiB posted · 7 files · 1,662 segments · NZB 0.2 MB
- **Health**: 16/16 sampled articles present
- **Obfuscation**: `extensionless-names`, `unquoted-subjects`, `opaque-subjects`, `per-file-unique-stems`
- **Source**: `Obfuscated.nzb`
- **sha256**: `6794d1464e7cb28c…`

### `plain-large`

Large direct MKV with hex stems.

- **Axes**: `direct-video`, `no-archive`
- **Verified**: matroska
- **Size**: 15.9 GiB posted · 10 files · 7,915 segments · NZB 0.7 MB
- **Health**: 16/16 sampled articles present
- **Obfuscation**: `per-file-unique-stems`
- **Source**: `El.Rio.De.La.Muerte.2024.SPANiSH.MULTi.1080p.BluRay.x264-USENETHD.nzb`
- **sha256**: `97df5ff5d59a3ff1…`

### `plain-season-pack`

Ten direct MKVs in one NZB.

- **Axes**: `direct-video`, `season-pack`, `file-selection`
- **Verified**: matroska · password in NZB
- **Size**: 22.6 GiB posted · 20 files · 32,850 segments · NZB 2.8 MB
- **Health**: 16/16 sampled articles present
- **Source**: `Bosch.S01.nzb`
- **sha256**: `c9b60371473825b0…`

### `rar4-rNN`

RAR4 .rar/.rNN volumes with letter rollover past .r99.

- **Axes**: `rar4`, `stored`, `rar+rNN`, `letter-rollover`
- **Verified**: rar4 · stored
- **Size**: 6.2 GiB posted · 125 files · 8,404 segments · NZB 0.8 MB
- **Health**: 16/16 sampled articles present
- **Source**: `It (1).nzb`
- **sha256**: `f89633155ab56480…`

### `rar-hdrenc-small`

Header-encrypted RAR5 with password meta.

- **Axes**: `rar5`, `encrypted-headers`, `password-in-nzb`
- **Verified**: rar5 · encrypted headers · password in NZB
- **Size**: 5.2 GiB posted · 57 files · 7,537 segments · NZB 0.7 MB
- **Health**: 16/16 sampled articles present
- **Obfuscation**: `opaque-subjects`
- **Source**: `An_Awesome_Piece_Of_Media.nzb`
- **sha256**: `d8e5a0d0cd14e2d1…`

### `rar-hdrenc-large`

Header-encrypted RAR5 at 24 GiB.

- **Axes**: `rar5`, `encrypted-headers`, `password-in-nzb`
- **Verified**: rar5 · encrypted headers · password in NZB
- **Size**: 24.0 GiB posted · 128 files · 36,001 segments · NZB 3.2 MB
- **Health**: 16/16 sampled articles present
- **Obfuscation**: `opaque-subjects`
- **Source**: `Eden (2025) [BDRip x264 1080p AC3 5.1 Castellano].nzb`
- **sha256**: `01431cbac254d552…`

### `7z-split-bugonia`

Split 7z .001 volumes, 21 GiB.

- **Axes**: `7z`, `split-7z.NNN`, `password-in-nzb`
- **Verified**: 7z · AES-encrypted header · header: encrypted · password in NZB
- **Size**: 20.8 GiB posted · 117 files · 31,275 segments · NZB 2.8 MB
- **Health**: 16/16 sampled articles present
- **Source**: `Bugonia.2025.SPANiSH.MULTi.1080p.BluRay.x264-USENETHD.nzb`
- **sha256**: `3d3aa512f73872c8…`

### `7z-split-tardes`

Split 7z .001 volumes, 19 GiB.

- **Axes**: `7z`, `split-7z.NNN`, `password-in-nzb`
- **Verified**: 7z · AES-encrypted header · header: encrypted · password in NZB
- **Size**: 19.3 GiB posted · 112 files · 29,068 segments · NZB 2.6 MB
- **Health**: 16/16 sampled articles present
- **Source**: `Tardes.De.Soledad.2024.SPANiSH.1080p.BluRay.x264-USENETHD.nzb`
- **sha256**: `87a5066e0aeccf85…`


## failure

Damaged or dead posts. Robustness only, never performance.

### `rar-partNN-large`

Dead post: 94% of articles gone from the provider (STAT sweep 2026-09-05). Must be refused.

- **Axes**: `rar`, `partNN`, `password-in-nzb`
- **Verified**: password in NZB
- **Size**: 19.5 GiB posted · 72 files · 29,160 segments · NZB 2.6 MB
- **Health**: availability not sampled
- **Source**: `2012 (2009) [BDRip x264 1080p DTS-HD MA 5.1 Castellano].nzb`
- **sha256**: `bc3e1ebe3c25efca…`

### `rar-partNN-maestras`

Dead post: 76% of articles gone. Must be refused.

- **Axes**: `rar`, `partNN`, `password-in-nzb`
- **Verified**: password in NZB
- **Size**: 3.5 GiB posted · 26 files · 5,234 segments · NZB 0.5 MB
- **Health**: availability not sampled
- **Obfuscation**: `opaque-subjects`
- **Source**: `Las maestras de la republica.nzb`
- **sha256**: `8897062c4e8778de…`

### `rar-partNN-satans`

Mid-size partNN RAR, opaque subjects.

- **Axes**: `rar`, `partNN`, `password-in-nzb`
- **Verified**: password in NZB
- **Size**: 3.1 GiB posted · 21 files · 4,660 segments · NZB 0.4 MB
- **Health**: availability not sampled
- **Obfuscation**: `opaque-subjects`
- **Source**: `Satans.Blood.Recuerdos.de.Escalofrio.2016.1080P.BLURAY.X264-WATCHABLE.nzb`
- **sha256**: `072bf2a684bdfca9…`

### `damaged-partial`

Lightly damaged direct MKV; should still serve.

- **Axes**: `direct-video`, `missing-articles`
- **Verified**: matroska
- **Size**: 6.7 GiB posted · 7 files · 1,662 segments · NZB 0.2 MB
- **Health**: 16/16 sampled articles present
- **Obfuscation**: `per-file-unique-stems`
- **Source**: `ToyStory5Damaged.nzb`
- **sha256**: `90b9bdccb56a8f91…`

