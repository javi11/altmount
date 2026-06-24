# P2P Metadata Sharing — Security Model

P2P sharing (`share.enabled`) lets altmount nodes discover each other over a
private BitTorrent DHT and exchange per-file metadata, skipping the NNTP
download when a peer already imported the same release.

## What crosses the wire

Only structural `.meta` protobufs (segment *references*, file size, paths). The
actual segment data is **rebuilt locally** by each node from its own copy of the
NZB. A peer can therefore never make you fetch or serve content that is not
already derivable from your own NZB.

## Trust boundaries and built-in guardrails

- **Release binding:** the DHT key is `SHA256(NZB)`, so peers only match on
  byte-identical NZBs.
- **SSRF protection:** peer addresses from the DHT are dialled only if globally
  routable; loopback/private/link-local (incl. `169.254.169.254`) are skipped and
  HTTP redirects are disabled.
- **Bounds + path checks:** peer segment refs must resolve within the locally
  rebuilt store; peer virtual paths cannot escape the metadata root.
- **Encryption:** peer-supplied decryption parameters are rejected by default
  (`share.allow_encrypted: false`); encrypted releases import normally.
- **Quorum:** `share.min_peers` (default 1) sets how many distinct peer IPs must
  agree on a metadata set before it is trusted. Raise to 2+ on untrusted networks.
- **Rate limit:** `share.rate_limit_per_minute` (default 120) bounds anonymous
  requests to the public endpoints, which are also GET-only.
- **Response caps:** manifests and per-file metas are size-capped to bound memory.

## Residual risks — operator responsibilities

1. **The `/api/share/*` endpoints are UNAUTHENTICATED.** The release hash is the
   capability. Anyone who can reach altmount's HTTP port and possesses the same
   NZB can confirm you hold that release. **Do not expose the HTTP port to the
   public internet** unless the node is intended as a coordinator; firewall it to
   trusted networks/VPN.
2. **DHT announce is a disclosure.** Announcing publishes (in a namespaced but
   public DHT) that your node holds a release, confirmable by anyone with the same
   NZB. The feature is opt-in for this reason.
3. **Single-peer poisoning (low blast radius).** With `min_peers: 1`, a malicious
   peer can at worst garble *that release's own* files (a local DoS), not inject
   foreign content. Raise `min_peers` to require independent agreement.

## Non-goal: content verification

altmount does **not** download a sample segment to verify peer metadata. That
would re-introduce the NNTP cost the feature exists to avoid and only
probabilistically catches poisoning. The quorum plus local store rebuild and
ref-bounds checks are the chosen mitigations.
