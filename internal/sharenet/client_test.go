package sharenet_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

// peerServer starts an httptest.Server that serves info, meta and seg bytes for hash.
func peerServer(t *testing.T, hash string, metaBytes, segBytes []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/share/info/"+hash, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"virtual_path":"Movies/Title/Title.mkv"}`))
	})
	mux.HandleFunc("/api/share/meta/"+hash, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(metaBytes)
	})
	mux.HandleFunc("/api/share/seg/"+hash, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(segBytes)
	})
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
	// A valid (minimal) FileMetadata proto: field 1, wire type 2, length 0 →
	// bytes {0x0a, 0x00} (empty Name). proto.Unmarshal accepts this.
	validProto := []byte{0x0a, 0x00}
	hash := "abc123"

	srv := peerServer(t, hash, validProto, []byte("seg"))
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 8080)

	files, err := client.LookupAndFetch(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(files.MetaBytes) != string(validProto) {
		t.Fatalf("meta mismatch: got %v", files.MetaBytes)
	}
	if string(files.SegBytes) != "seg" {
		t.Fatalf("seg mismatch: got %q", files.SegBytes)
	}
	if files.VirtualPath != "Movies/Title/Title.mkv" {
		t.Fatalf("virtual path mismatch: got %q", files.VirtualPath)
	}
}

func TestClient_LookupAndFetch_NoPeers(t *testing.T) {
	mock := &MockDHT{peers: nil}
	client := sharenet.NewClient(mock, 8080)

	_, err := client.LookupAndFetch(context.Background(), "hash123")
	if !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers, got %v", err)
	}
}

func TestClient_LookupAndFetch_BadProto_BlacklistsPeer(t *testing.T) {
	// Return garbage bytes that are not a valid proto. After blacklisting the
	// only peer, we should get ErrNoPeers.
	hash := "abc123"
	srv := peerServer(t, hash, []byte("not-a-proto\xff\xfe"), []byte("seg"))
	defer srv.Close()

	mock := &MockDHT{peers: []netip.AddrPort{peerAddrPort(t, srv)}}
	client := sharenet.NewClient(mock, 8080)

	_, err := client.LookupAndFetch(context.Background(), hash)
	if !errors.Is(err, sharenet.ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers after bad peer, got %v", err)
	}
}

func TestClient_Announce_CallsDHT(t *testing.T) {
	mock := &MockDHT{}
	client := sharenet.NewClient(mock, 8080)

	if err := client.Announce(context.Background(), "abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.announced) != 1 || mock.announced[0] != "abc123" {
		t.Fatalf("expected abc123 announced, got %v", mock.announced)
	}
}
