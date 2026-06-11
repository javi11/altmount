# Import Battery Test Fixtures

Binary archive fixtures for `TestImportBattery_*` tests in `internal/importer`.

## Contents

| Path | Description |
|---|---|
| `payload_a.bin` | 200 000-byte deterministic payload (repeating 0–255 pattern) |
| `payload_b.bin` | 120 000-byte deterministic payload (repeating 128–127 pattern) |
| `rar_single/archive.rar` | Single-part RAR containing `payload_a.bin` (store, no compression) |
| `rar_multi/archive.part01.rar` + `.part02.rar` | Two-part RAR containing `payload_a.bin`, split at 100 KB |
| `rar_oldstyle/archive.rar` + `.r00` | Old-style RAR volumes (`.r##` naming); may be absent if local `rar` lacks `-vn` support |
| `7z_single/archive.7z` | Single-part 7z containing `payload_b.bin` (store, no compression) |
| `7z_multi/archive.7z.001` + `.7z.002` | Multi-part 7z containing `payload_b.bin`, split at 100 KB |
| `manifest.json` | Inner file names and sizes for each fixture, used by tests for assertions |

## Why committed binaries?

altmount's archive analyzers stream real bytes through `rardecode` and `sevenzip` libraries. There are no pure-Go RAR/7z writers, so fixtures cannot be generated programmatically. Store mode (`-m0` / `-mx=0`) is required because altmount rejects compressed entries.

## Regenerating

Requirements: `rar` (WinRAR CLI) and `7z` (p7zip / 7-Zip) must be installed.

```bash
cd internal/importer/testdata
bash gen_fixtures.sh
git add -A
```

The generator script is idempotent — running it again produces byte-identical output because payloads use a fixed deterministic pattern.
