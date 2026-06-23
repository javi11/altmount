package sharenet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// ErrNoPeers is returned when no peer has the requested release.
var ErrNoPeers = errors.New("sharenet: no peers have this release")

// maxManifestEntries caps how many metas a single release manifest may list.
// A release is one NZB; even a large season pack is well under this. The cap
// bounds the outbound request fan-out a malicious peer can trigger per lookup.
const maxManifestEntries = 4096

// SharedMeta is one decoded per-file metadata fetched from a peer.
// Meta is the structural v3 proto (SegmentRefs/SegmentRuns intact); its StoreRef
// still points at the peer's store path and must be rewritten by the caller to
// the locally-rebuilt store before it is written to disk.
type SharedMeta struct {
	VirtualPath string
	Meta        *metapb.FileMetadata
}

// ReleaseFiles holds every per-file meta a release produced, in manifest order.
type ReleaseFiles struct {
	Metas []SharedMeta
}

// Client coordinates DHT lookups and meta transfers.
type Client struct {
	dht      DHT
	httpPort int
	http     *http.Client

	blMu      sync.RWMutex
	blacklist map[string]time.Time // peer addr:port string → expiry
}

// NewClient creates a Client. httpPort is the port altmount's HTTP server
// listens on (announced to peers so they know where to fetch from).
func NewClient(dht DHT, httpPort int) *Client {
	return &Client{
		dht:       dht,
		httpPort:  httpPort,
		http:      &http.Client{Timeout: 30 * time.Second},
		blacklist: make(map[string]time.Time),
	}
}

// LookupAndFetch finds peers with releaseHash and downloads every per-file v3
// .meta the release produced. Returns ErrNoPeers if no reachable, non-blacklisted
// peer has the release. Segment data is never transferred — the caller rebuilds
// the NzbStore locally from the same NZB.
func (c *Client) LookupAndFetch(ctx context.Context, releaseHash string) (*ReleaseFiles, error) {
	peers, err := c.dht.Lookup(ctx, releaseHash)
	if err != nil {
		return nil, fmt.Errorf("DHT lookup: %w", err)
	}

	for _, peer := range peers {
		if c.isBlacklisted(peer) {
			continue
		}
		files, err := c.fetchFromPeer(ctx, peer, releaseHash)
		if err != nil {
			// fetchFromPeer blacklists peers that serve malformed data; skip to next.
			continue
		}
		return files, nil
	}

	return nil, ErrNoPeers
}

// Announce tells the DHT network that we have releaseHash available.
// Call this after a successful local import.
func (c *Client) Announce(ctx context.Context, releaseHash string) error {
	return c.dht.Announce(ctx, releaseHash, c.httpPort)
}

func (c *Client) fetchFromPeer(ctx context.Context, peer netip.AddrPort, releaseHash string) (*ReleaseFiles, error) {
	base := fmt.Sprintf("http://%s/api/share", peer)

	manifestBytes, err := c.get(ctx, base+"/manifest/"+releaseHash)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest from %s: %w", peer, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || len(manifest.Metas) == 0 {
		c.blacklistPeer(peer)
		return nil, fmt.Errorf("peer %s sent invalid manifest: %w", peer, err)
	}
	if len(manifest.Metas) > maxManifestEntries {
		c.blacklistPeer(peer)
		return nil, fmt.Errorf("peer %s manifest has %d entries (> %d)", peer, len(manifest.Metas), maxManifestEntries)
	}

	metas := make([]SharedMeta, 0, len(manifest.Metas))
	for i, entry := range manifest.Metas {
		if entry.VirtualPath == "" {
			c.blacklistPeer(peer)
			return nil, fmt.Errorf("peer %s manifest entry %d has empty path", peer, i)
		}
		raw, err := c.get(ctx, fmt.Sprintf("%s/meta/%s/%d", base, releaseHash, i))
		if err != nil {
			return nil, fmt.Errorf("fetch meta %d from %s: %w", i, peer, err)
		}
		// Only v3 store-backed metas are shareable; decode the structural proto
		// (no store resolution). A peer serving anything else is misbehaving.
		fm, err := metadata.DecodeStructuralMeta(raw)
		if err != nil {
			c.blacklistPeer(peer)
			return nil, fmt.Errorf("peer %s sent unusable .meta %d: %w", peer, i, err)
		}
		metas = append(metas, SharedMeta{VirtualPath: entry.VirtualPath, Meta: fm})
	}

	return &ReleaseFiles{Metas: metas}, nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MB cap
}

func (c *Client) isBlacklisted(peer netip.AddrPort) bool {
	key := peer.String() // include the port: distinct peers can share a NAT IP
	c.blMu.RLock()
	expiry, ok := c.blacklist[key]
	c.blMu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		c.blMu.Lock()
		delete(c.blacklist, key)
		c.blMu.Unlock()
		return false
	}
	return true
}

func (c *Client) blacklistPeer(peer netip.AddrPort) {
	c.blMu.Lock()
	defer c.blMu.Unlock()
	c.blacklist[peer.String()] = time.Now().Add(24 * time.Hour)
}
