package sharenet

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/krpc"
)

// DHT is the interface the rest of the package uses.
// The real implementation wraps anacrolix/dht. Tests use a mock.
type DHT interface {
	// Announce tells the network we have this release available at port.
	Announce(ctx context.Context, releaseHash string, port int) error
	// Lookup finds peers that have announced this release.
	// Returns an empty slice (not an error) when no peers are found within timeout.
	Lookup(ctx context.Context, releaseHash string) ([]netip.AddrPort, error)
	Close() error
}

// NewDHT creates a DHT client that bootstraps from bootstrapNodes.
// If bootstrapNodes is empty the node runs as a coordinator (self-bootstrapped).
// bootstrapNodes format: "host:port" e.g. "node1.example.com:42042".
// listenPort is the UDP port to bind (0 = random).
func NewDHT(bootstrapNodes []string, listenPort int) (DHT, error) {
	cfg := dht.NewDefaultServerConfig()

	cfg.StartingNodes = func() ([]dht.Addr, error) {
		if len(bootstrapNodes) == 0 {
			return nil, nil // coordinator mode: self-bootstrapped
		}
		addrs := make([]dht.Addr, 0, len(bootstrapNodes))
		for _, node := range bootstrapNodes {
			udpAddr, err := net.ResolveUDPAddr("udp", node)
			if err != nil {
				return nil, fmt.Errorf("resolve bootstrap node %s: %w", node, err)
			}
			addrs = append(addrs, dht.NewAddr(udpAddr))
		}
		return addrs, nil
	}

	if listenPort > 0 {
		conn, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", listenPort))
		if err != nil {
			return nil, fmt.Errorf("listen on UDP port %d: %w", listenPort, err)
		}
		cfg.Conn = conn
	}

	server, err := dht.NewServer(cfg)
	if err != nil {
		return nil, fmt.Errorf("create DHT server: %w", err)
	}

	return &anacrolixDHT{server: server}, nil
}

type anacrolixDHT struct {
	server *dht.Server
}

func (d *anacrolixDHT) Announce(_ context.Context, releaseHash string, port int) error {
	ih := Infohash(releaseHash)
	ann, err := d.server.Announce(ih, port, false)
	if err != nil {
		return fmt.Errorf("DHT announce: %w", err)
	}
	ann.Close()
	return nil
}

func (d *anacrolixDHT) Lookup(ctx context.Context, releaseHash string) ([]netip.AddrPort, error) {
	ih := Infohash(releaseHash)
	it, err := d.server.AnnounceTraversal(ih)
	if err != nil {
		return nil, fmt.Errorf("DHT lookup: %w", err)
	}
	defer it.Close()

	var peers []netip.AddrPort
	timeout := time.After(10 * time.Second)

	for {
		select {
		case v, ok := <-it.Peers:
			if !ok {
				return peers, nil
			}
			for _, p := range v.Peers {
				if ap := nodeAddrToAddrPort(p); ap.IsValid() {
					peers = append(peers, ap)
				}
			}
		case <-timeout:
			return peers, nil
		case <-ctx.Done():
			return peers, ctx.Err()
		}
	}
}

func (d *anacrolixDHT) Close() error {
	d.server.Close()
	return nil
}

func nodeAddrToAddrPort(na krpc.NodeAddr) netip.AddrPort {
	ip, ok := netip.AddrFromSlice(na.IP)
	if !ok {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(na.Port))
}
