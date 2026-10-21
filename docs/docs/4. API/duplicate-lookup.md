---
title: Duplicate Lookup
sidebar_position: 3
---

# Duplicate lookup

AltMount provides a generic HTTP service for finding an existing NZB before uploading a local file. A match requires the same **original filename**, **exact byte length**, and **sampled content hash**. Clients can use the returned NZB instead of posting the file again.

The protocol is independent of any particular uploader. Use an uploader or script that implements the request format below.

## Request and authentication

Send a JSON request to `POST /api/files/duplicate-lookup` (adjust `/api` if you changed the API prefix). Set `X-API-Key` to your API key from **System → Settings**, or the configured `api.key_override`. This endpoint always requires a valid API key, including when interactive login is disabled. A JWT or `?apikey=` does not replace this header.

```http
POST /api/files/duplicate-lookup HTTP/1.1
Content-Type: application/json
X-API-Key: your-api-key

{
  "algorithm": "sample-sha256-v1",
  "name": "Movie.2026.1080p.mkv",
  "size": 4831838208,
  "hash": "<64 lowercase hexadecimal characters>"
}
```

`name` is the original file's basename, including its extension. Matching is case-sensitive, with no case folding or Unicode normalization. Do not include directory separators or control characters. Renamed local files do not match the old name, even when their content is identical. New direct imports preserve the original name independently of virtual-path disambiguation and later moves.

The request body is limited to 8192 bytes. Only the four fields shown above are accepted; `size` must be a nonnegative integer.

| HTTP status | Meaning | Client action |
| --- | --- | --- |
| `200` | Match; body is raw NZB XML, `Content-Type: application/x-nzb` | Validate and save the NZB atomically, then mark the file as reused |
| `204` | No supported, matching file in the catalog | Continue with the usual upload |
| `400` | Invalid JSON or fingerprint | Correct the request |
| `401` | Missing or invalid API key | Correct the credentials |
| `503` | Catalog, metadata, or content lookup could not finish | Retain the source and retry later |

Network errors and error responses are not misses. Clients should stop or retry the lookup instead of silently uploading. Responses use `Cache-Control: no-store`. The server bounds lookup work to 30 seconds; allow a little longer in the client and reverse proxy to receive a response.

## Fingerprint algorithm: `sample-sha256-v1`

Both sides calculate SHA-256 over this binary stream, in order:

1. The ASCII bytes `sample-sha256-v1`, followed by a single NUL byte (`00`).
2. The file size as an unsigned 64-bit **little-endian** integer.
3. For each sample: its offset as an unsigned 64-bit little-endian integer, its length in the same format, then its raw bytes.

For files **at most 196608 bytes**, use one sample containing the whole file, at offset zero. Empty files therefore have a zero-length sample.

For larger files, take three **65536-byte** samples, in this order:

- Beginning: offset `0`.
- Middle: offset `floor((size - 65536) / 2)`.
- End: offset `size - 65536`.

Encode the final SHA-256 digest as 64 lowercase hexadecimal characters. The filename is matched separately and is not part of the digest. Implementations should reject short reads and files that change during sampling.

These vectors use a file whose byte at position `i` is `i % 251`:

| Size in bytes | SHA-256 fingerprint |
| --- | --- |
| 0 | `05a8b6e0a73e1eea7da7d93b345eb2e99c9e71b6c34e0ae42fde07302a5371d9` |
| 196608 | `20f87644845ab1ca1f301d85f83a1ca9e9ed9d5c6f86e589d02a945a39f8a740` |
| 196609 | `d2c3b849b00a87e9bb79ad28d2b820c74777911e4eb682d2c5b7b20ac1febbb9` |

## Example client

This Python example uses only the standard library. Save it as `lookup.py`, then run it with the endpoint, local file and destination NZB. Set the key through an environment variable so it does not appear in the command's arguments.

```bash
export LOOKUP_API_KEY='your-api-key'
python3 lookup.py \
  http://localhost:8080/api/files/duplicate-lookup \
  /data/Movie.2026.1080p.mkv \
  /output/Movie.2026.1080p.nzb
```

```python
import hashlib
import json
import os
import pathlib
import struct
import sys
import tempfile
import urllib.request
import xml.etree.ElementTree as ET

endpoint, source, destination = sys.argv[1:]
source = pathlib.Path(source)
with source.open("rb") as file:
    before = os.fstat(file.fileno())
    size = before.st_size
    samples = [(0, size)] if size <= 196608 else [
        (0, 65536), ((size - 65536) // 2, 65536),
        (size - 65536, 65536),
    ]
    digest = hashlib.sha256(b"sample-sha256-v1\0")
    digest.update(struct.pack("<Q", size))
    for offset, length in samples:
        file.seek(offset)
        data = file.read(length)
        if len(data) != length:
            raise RuntimeError("Source changed or could not be read")
        digest.update(struct.pack("<QQ", offset, length))
        digest.update(data)
    after = os.fstat(file.fileno())
    if (before.st_size, before.st_mtime_ns) != (
        after.st_size, after.st_mtime_ns
    ):
        raise RuntimeError("Source changed while sampling")

payload = json.dumps({
    "algorithm": "sample-sha256-v1", "name": source.name,
    "size": size, "hash": digest.hexdigest(),
}).encode()
request = urllib.request.Request(endpoint, data=payload, headers={
    "Content-Type": "application/json",
    "X-API-Key": os.environ["LOOKUP_API_KEY"],
})
# Reject redirects so credentials are sent only to the configured endpoint.
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

with urllib.request.build_opener(NoRedirect).open(request, timeout=40) as response:
    if response.status == 204:
        print("No match; normal upload can proceed")
        sys.exit(0)
    if response.status != 200:
        raise RuntimeError(f"Unexpected status: {response.status}")
    nzb = response.read(32 * 1024 * 1024 + 1)
    if len(nzb) > 32 * 1024 * 1024:
        raise RuntimeError("NZB exceeds the client size limit")
    root = ET.fromstring(nzb)
    if root.tag.rsplit("}", 1)[-1] != "nzb" or not any(
        element.tag.rsplit("}", 1)[-1] == "segment"
        and (element.text or "").strip() for element in root.iter()
    ):
        raise RuntimeError("Response is not a usable NZB")

destination = pathlib.Path(destination)
destination.parent.mkdir(parents=True, exist_ok=True)
with tempfile.NamedTemporaryFile(dir=destination.parent, delete=False) as output:
    temporary = pathlib.Path(output.name)
    try:
        output.write(nzb)
        output.flush()
        os.fsync(output.fileno())
    except BaseException:
        temporary.unlink(missing_ok=True)
        raise
try:
    os.replace(temporary, destination)
finally:
    temporary.unlink(missing_ok=True)
print(f"Reused existing NZB: {destination}; source retained")
```

## Catalog and supported files

The catalog stores searchable names and sizes plus cached fingerprints in the configured database. It discovers files from local metadata; it does not eagerly download every file to fingerprint the entire library. Content samples are read through the virtual filesystem only for candidates with a matching name and size. Reading a sample may download its containing Usenet article and additional bytes required by the reader.

The first lookup builds the local catalog. Later lookups refresh discovery at most once per minute, so newly imported or newly eligible files can take up to a minute to appear. New uploads enter the catalog after their NZB has been imported. Existing candidates are revalidated on every lookup, even between discovery refreshes.

Reuse is restricted to healthy, directly posted **single data-file releases**, optionally including their original PAR2 files. Store-backed metadata must describe the entire original posted file. Archive members, encrypted or transformed streams, nested files, damaged files, and releases containing several data files are not offered. Older metadata without a reliable original NZB store is excluded. Supported older store-backed imports recover their original name from the original NZB subject where possible.

Cached fingerprints are tied to the metadata and original store. Candidates are checked again before reuse so changed, missing or unhealthy metadata cannot reuse a stale fingerprint. This lookup is not a complete availability check of every article on every provider.

**Partial hashes are probabilistic.** Files can share a name, size and sampled bytes while differing elsewhere. Reuse should retain the local source; a sampled match alone should not authorize automatic source deletion.
