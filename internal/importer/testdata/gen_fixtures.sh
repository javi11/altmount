#!/usr/bin/env bash
# gen_fixtures.sh — generates deterministic archive test fixtures for the
# NZB import battery tests.
#
# Requirements: rar (WinRAR CLI), 7z (p7zip / 7-Zip CLI)
# Run from the testdata/ directory, or from anywhere as long as the working
# directory is set to the testdata/ directory before running.
#
#   cd internal/importer/testdata && bash gen_fixtures.sh
#
# All archives use store mode (no compression) so that segment offsets map
# 1-to-1 onto the inner file bytes. This is required by altmount's archive
# analyzers: compressed entries are rejected.
#
# Regenerate whenever payload_a.bin or payload_b.bin content changes, or when
# new fixture variants are needed.

set -euo pipefail

cd "$(dirname "$0")"

echo "==> Generating deterministic payloads..."

# Generate payload_a.bin (200 000 bytes) and payload_b.bin (120 000 bytes)
# using a simple repeating pattern so the bytes are deterministic across runs.
python3 -c "
import sys
# payload_a: 200000 bytes with repeating 0..255 pattern
pattern = bytes(range(256))
data = (pattern * (200000 // 256 + 1))[:200000]
sys.stdout.buffer.write(data)
" > payload_a.bin

python3 -c "
import sys
# payload_b: 120000 bytes, offset to distinguish from payload_a
pattern = bytes((b + 128) % 256 for b in range(256))
data = (pattern * (120000 // 256 + 1))[:120000]
sys.stdout.buffer.write(data)
" > payload_b.bin

echo "  payload_a.bin: $(wc -c < payload_a.bin) bytes"
echo "  payload_b.bin: $(wc -c < payload_b.bin) bytes"

# ---------------------------------------------------------------------------
# RAR single-part
# ---------------------------------------------------------------------------
echo "==> rar_single/archive.rar ..."
rm -f rar_single/archive.rar
# -m0 = store (no compression), -ep = exclude path prefix
rar a -m0 -ep rar_single/archive.rar payload_a.bin
echo "  done: $(wc -c < rar_single/archive.rar) bytes"

# ---------------------------------------------------------------------------
# RAR multi-part (.part01.rar / .part02.rar style, new naming)
# -v100k = split into 100 KB volumes.
# rar auto-generates archive.part1.rar, archive.part2.rar, ...
# Rename single-digit parts to zero-padded (part01, part02) for consistency.
# ---------------------------------------------------------------------------
echo "==> rar_multi/archive.part*.rar ..."
rm -f rar_multi/archive.part*.rar
# Base name must NOT already contain "part" to avoid doubled naming.
rar a -m0 -ep -v100k rar_multi/archive.rar payload_a.bin
# Rename part1/part2 → part01/part02
for f in rar_multi/archive.part[0-9].rar; do
    [ -f "$f" ] || continue
    n="${f##*part}"
    n="${n%.rar}"
    mv "$f" "rar_multi/archive.part0${n}.rar"
done
ls -la rar_multi/
echo "  done"

# ---------------------------------------------------------------------------
# RAR old-style volumes (.rar / .r00 / .r01 ...)
# Modern rar (5+) dropped the -vn flag and always uses new-style naming.
# Skip gracefully when not supported; the test checks for the fixture files.
# ---------------------------------------------------------------------------
echo "==> rar_oldstyle/archive.rar + .r00 (best-effort) ..."
rm -f rar_oldstyle/archive.rar rar_oldstyle/archive.r[0-9][0-9]
if rar a -m0 -ep -vn -v100k rar_oldstyle/archive.rar payload_a.bin >/dev/null 2>&1 \
   && [ -f rar_oldstyle/archive.r00 ]; then
    echo "  old-style naming supported"
    ls -la rar_oldstyle/
else
    echo "  WARNING: rar does not produce .r00 old-style volumes on this system."
    echo "  TestImportBattery_RarOldStyleVolumes will be skipped automatically."
    # Clean up any partial files (new-style volumes + first volume) rar may have created.
    rm -f rar_oldstyle/archive.rar rar_oldstyle/archive.part*.rar
fi

# ---------------------------------------------------------------------------
# 7z single-part
# -mx=0 = store (no compression)
# ---------------------------------------------------------------------------
echo "==> 7z_single/archive.7z ..."
rm -f 7z_single/archive.7z
7z a -mx=0 7z_single/archive.7z payload_b.bin
echo "  done: $(wc -c < 7z_single/archive.7z) bytes"

# ---------------------------------------------------------------------------
# 7z multi-part (.7z.001 / .7z.002 ...)
# -v100k = split into 100 KB volumes
# ---------------------------------------------------------------------------
echo "==> 7z_multi/archive.7z.001 ..."
rm -f 7z_multi/archive.7z.*
7z a -mx=0 -v100k 7z_multi/archive.7z payload_b.bin
ls -la 7z_multi/
echo "  done"

# ---------------------------------------------------------------------------
# Write manifest.json listing inner file names and sizes per fixture
# ---------------------------------------------------------------------------
echo "==> Writing manifest.json ..."
python3 - <<'PYEOF'
import json, os, re, subprocess

def rar_contents(archive):
    """Return list of {name, size} from all parts of the archive."""
    try:
        out = subprocess.check_output(["rar", "vt", archive], stderr=subprocess.DEVNULL, text=True)
    except subprocess.CalledProcessError:
        return []
    results = []
    # "rar vt" technical listing: look for lines like "  Name: payload_a.bin"
    # and "  Size: 200000"
    name, size = None, None
    for line in out.splitlines():
        m = re.match(r'\s+Name:\s+(.+)', line)
        if m:
            name = m.group(1).strip()
        m = re.match(r'\s+Size:\s+(\d+)', line)
        if m:
            size = int(m.group(1))
        if name is not None and size is not None:
            results.append({"name": name, "size": size})
            name, size = None, None
    if not results:
        # Fallback: "rar l" simple listing (columns vary by version)
        try:
            out2 = subprocess.check_output(["rar", "l", archive], stderr=subprocess.DEVNULL, text=True)
        except subprocess.CalledProcessError:
            return []
        in_listing = False
        for line in out2.splitlines():
            stripped = line.strip()
            if re.match(r'^-{5,}', stripped):
                in_listing = not in_listing
                continue
            if not in_listing:
                continue
            # Format: size  packed  ratio  date  time  attr  name
            parts = stripped.split()
            if len(parts) >= 5:
                try:
                    s = int(parts[0])
                    n = parts[-1]
                    if n and not n.startswith('-'):
                        results.append({"name": n, "size": s})
                except ValueError:
                    pass
    return results

def sz_contents(archive):
    """Return list of {name, size} by parsing 7z l output.

    Only captures lines whose attribute field (col 4) looks like a file
    attribute string (e.g. '....A', 'D....'). This excludes summary lines
    like '1 files' that appear after each separator.
    """
    try:
        out = subprocess.check_output(["7z", "l", archive], stderr=subprocess.DEVNULL, text=True)
    except subprocess.CalledProcessError:
        return []
    results = []
    seen = set()
    for line in out.splitlines():
        stripped = line.strip()
        # Only care about data rows: date(10) time(8) attr(5) size compr name
        # e.g. "2026-06-10 23:59:01 ....A   120000   120000  payload_b.bin"
        m = re.match(
            r'\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\s+'
            r'([.DRASH]{5})\s+(\d+)\s+\d+\s+(.+)', stripped
        )
        if m:
            attr, size_str, name = m.group(1), m.group(2), m.group(3).strip()
            # Skip directory entries (attr starts with 'D')
            if attr.startswith('D'):
                continue
            size = int(size_str)
            key = (name, size)
            if key not in seen:
                seen.add(key)
                results.append({"name": name, "size": size})
    return results

manifest = {}

if os.path.exists("rar_single/archive.rar"):
    manifest["rar_single"] = rar_contents("rar_single/archive.rar")

# For multi-part, use the first volume.
if os.path.exists("rar_multi/archive.part01.rar"):
    manifest["rar_multi"] = rar_contents("rar_multi/archive.part01.rar")

if os.path.exists("rar_oldstyle/archive.rar"):
    manifest["rar_oldstyle"] = rar_contents("rar_oldstyle/archive.rar")

if os.path.exists("7z_single/archive.7z"):
    manifest["7z_single"] = sz_contents("7z_single/archive.7z")

if os.path.exists("7z_multi/archive.7z.001"):
    manifest["7z_multi"] = sz_contents("7z_multi/archive.7z.001")

with open("manifest.json", "w") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")

print("manifest.json:")
print(json.dumps(manifest, indent=2))
PYEOF

echo ""
echo "==> Done. Verify contents above, then commit:"
echo "    git add internal/importer/testdata/"
