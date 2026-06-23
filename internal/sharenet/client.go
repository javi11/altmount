package sharenet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sync"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"google.golang.org/protobuf/proto"
)

// ErrNoPeers is returned when no peer has the requested release.
var ErrNoPeers = errors.New("sharenet: no peers have this release")

// ReleaseFiles holds the raw bytes fetched from a peer.
type ReleaseFiles struct {
	MetaBytes []byte // .meta file content (FileMetadata proto, structural part)
	SegBytes  []byte // .seg sidecar content (zstd-compressed FileSegments proto)
}

// Client coordinates DHT lookups and meta file transfers.
type Client struct {
	dht      DHT
	httpPort int
	http     *http.Client

	blMu      sync.Mutex
	blacklist map[string]time.Time // peer IP string → expiry
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

// LookupAndFetch finds peers with releaseHash and downloads the .meta+.seg files.
// Returns ErrNoPeers if no reachable, non-blacklisted peer has the release.
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
			// fetchFromPeer blacklists on bad proto; skip to next peer.
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

	metaBytes, err := c.get(ctx, base+"/meta/"+releaseHash)
	if err != nil {
		return nil, fmt.Errorf("fetch meta from %s: %w", peer, err)
	}

	// Verify the meta bytes are a parseable FileMetadata proto. v2 files carry a
	// magic prefix that fails proto.Unmarshal, so strip it before validating.
	if err := validateMeta(metaBytes); err != nil {
		c.blacklistPeer(peer)
		return nil, fmt.Errorf("peer %s sent unparseable .meta: %w", peer, err)
	}

	segBytes, err := c.get(ctx, base+"/seg/"+releaseHash)
	if err != nil {
		return nil, fmt.Errorf("fetch seg from %s: %w", peer, err)
	}

	return &ReleaseFiles{MetaBytes: metaBytes, SegBytes: segBytes}, nil
}

// metaMagicV2 is the prefix the metadata package writes on v2 ".meta" files.
// It is an invalid protobuf tag (field 0) so a v2 body must be stripped of it
// before proto.Unmarshal. Keep in sync with internal/metadata/service.go.
var metaMagicV2 = []byte{0x00, 'A', 'M', '2', 0x01}

func validateMeta(metaBytes []byte) error {
	body := metaBytes
	if len(body) >= len(metaMagicV2) && string(body[:len(metaMagicV2)]) == string(metaMagicV2) {
		body = body[len(metaMagicV2):]
	}
	var fm metapb.FileMetadata
	return proto.Unmarshal(body, &fm)
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
	c.blMu.Lock()
	defer c.blMu.Unlock()
	expiry, ok := c.blacklist[peer.Addr().String()]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(c.blacklist, peer.Addr().String())
		return false
	}
	return true
}

func (c *Client) blacklistPeer(peer netip.AddrPort) {
	c.blMu.Lock()
	defer c.blMu.Unlock()
	c.blacklist[peer.Addr().String()] = time.Now().Add(24 * time.Hour)
}
