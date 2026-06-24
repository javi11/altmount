package sharenet_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/sharenet"
)

// v3MetaBytes produces authentic on-disk v3 .meta bytes (magic + structural
// proto) by writing through the real MetadataService, then reading the file
// back. storeRef is set so WriteFileMetadata emits the v3 format.
func v3MetaBytes(t *testing.T, virtualPath string, fm *metapb.FileMetadata) []byte {
	t.Helper()
	root := t.TempDir()
	ms := metadata.NewMetadataService(root)
	fm.StoreRef = "peer/local/path.nzbz" // any non-empty value triggers v3
	if err := ms.WriteFileMetadata(virtualPath, fm); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, virtualPath+".meta"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if !metadata.IsV3Meta(raw) {
		t.Fatal("generated meta is not v3")
	}
	return raw
}

// peerServer serves a manifest and indexed v3 metas for hash.
func peerServer(t *testing.T, hash string, paths []string, metas [][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/share/manifest/"+hash, func(w http.ResponseWriter, _ *http.Request) {
		m := sharenet.Manifest{Metas: make([]sharenet.ManifestEntry, len(paths))}
		for i, p := range paths {
			m.Metas[i] = sharenet.ManifestEntry{VirtualPath: p}
		}
		_ = json.NewEncoder(w).Encode(m)
	})
	for i := range metas {
		i := i
		mux.HandleFunc(fmt.Sprintf("/api/share/meta/%s/%d", hash, i), func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(metas[i])
		})
	}
	return httptest.NewServer(mux)
}

func peerAddrPort(t *testing.T, srv *httptest.Server) netip.AddrPort {
	t.Helper()
	ap, err := netip.ParseAddrPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse addr: %v", err)
	}
	return ap
}

func TestClient_LookupAndFetch_Success(t *testing.T) {
	hash := "abc123"
	meta0 := v3MetaBytes(t, "Movies/A/A.mkv", &metapb.FileMetadata{FileSize: 100})
	meta1 := v3MetaBytes(t, "Movies/B/B.mkv", &metapb.FileMetadata{FileSize: 200})

	srv := peerServer(t, hash, []string{"Movies/A/A.mkv", "Movies/B/B.mkv"}, [][]byte{meta0, meta1})
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 8080, sharenet.WithAllowPrivatePeers())

	files, err := client.LookupAndFetch(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files.Metas) != 2 {
		t.Fatalf("expected 2 metas, got %d", len(files.Metas))
	}
	if files.Metas[0].VirtualPath != "Movies/A/A.mkv" || files.Metas[0].Meta.FileSize != 100 {
		t.Fatalf("meta 0 mismatch: %+v", files.Metas[0])
	}
	if files.Metas[1].VirtualPath != "Movies/B/B.mkv" || files.Metas[1].Meta.FileSize != 200 {
		t.Fatalf("meta 1 mismatch: %+v", files.Metas[1])
	}
}

func TestClient_LookupAndFetch_NoPeers(t *testing.T) {
	mock := &MockDHT{peers: nil}
	client := sharenet.NewClient(mock, 8080, sharenet.WithAllowPrivatePeers())

	if _, err := client.LookupAndFetch(context.Background(), "hash123"); !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers, got %v", err)
	}
}

func TestClient_LookupAndFetch_NonV3Meta_BlacklistsPeer(t *testing.T) {
	// A v1 (no-magic) or garbage meta is not shareable: the client must reject it,
	// blacklist the peer, and return ErrNoPeers when it's the only peer.
	hash := "abc123"
	srv := peerServer(t, hash, []string{"Movies/A/A.mkv"}, [][]byte{[]byte("not-a-v3-meta")})
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 8080, sharenet.WithAllowPrivatePeers())

	if _, err := client.LookupAndFetch(context.Background(), hash); !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers after bad peer, got %v", err)
	}
}

func TestClient_LookupAndFetch_EmptyManifest_BlacklistsPeer(t *testing.T) {
	hash := "abc123"
	srv := peerServer(t, hash, nil, nil) // manifest with zero metas
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 8080, sharenet.WithAllowPrivatePeers())

	if _, err := client.LookupAndFetch(context.Background(), hash); !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers for empty manifest, got %v", err)
	}
}

func TestClient_LookupAndFetch_OversizedManifest_BlacklistsPeer(t *testing.T) {
	// A manifest far beyond a real release's file count is an amplification attempt;
	// the client must reject it before fanning out per-meta requests.
	hash := "abc123"
	paths := make([]string, 5000)
	for i := range paths {
		paths[i] = fmt.Sprintf("X/file%d.mkv", i)
	}
	srv := peerServer(t, hash, paths, nil) // metas never fetched — cap trips first
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 8080, sharenet.WithAllowPrivatePeers())

	if _, err := client.LookupAndFetch(context.Background(), hash); !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers for oversized manifest, got %v", err)
	}
}

func TestClient_SkipsPrivatePeers_SSRFGuard(t *testing.T) {
	// Without WithAllowPrivatePeers (production default), a loopback peer address
	// must be skipped — the SSRF guard against DHT entries pointing at internal
	// services. The peer would otherwise serve a valid meta.
	hash := "abc123"
	meta0 := v3MetaBytes(t, "Movies/A/A.mkv", &metapb.FileMetadata{FileSize: 100})
	srv := peerServer(t, hash, []string{"Movies/A/A.mkv"}, [][]byte{meta0})
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}} // 127.0.0.1
	client := sharenet.NewClient(mock, 8080)                        // guard ON

	if _, err := client.LookupAndFetch(context.Background(), hash); !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected loopback peer skipped (ErrNoPeers), got %v", err)
	}
}

func TestClient_Announce_CallsDHT(t *testing.T) {
	mock := &MockDHT{}
	client := sharenet.NewClient(mock, 8080, sharenet.WithAllowPrivatePeers())

	if err := client.Announce(context.Background(), "abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.announced) != 1 || mock.announced[0] != "abc123" {
		t.Fatalf("expected abc123 announced, got %v", mock.announced)
	}
}
