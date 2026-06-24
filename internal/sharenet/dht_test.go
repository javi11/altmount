package sharenet_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/javi11/altmount/internal/sharenet"
)

// MockDHT implements sharenet.DHT for tests.
type MockDHT struct {
	announceErr error
	peers       []netip.AddrPort
	lookupErr   error
	announced   []string
}

func (m *MockDHT) Announce(_ context.Context, releaseHash string, _ int) error {
	if m.announceErr != nil {
		return m.announceErr
	}
	m.announced = append(m.announced, releaseHash)
	return nil
}

func (m *MockDHT) Lookup(_ context.Context, _ string) ([]netip.AddrPort, error) {
	return m.peers, m.lookupErr
}

func (m *MockDHT) Close() error { return nil }

func TestMockDHT_ImplementsInterface(t *testing.T) {
	var _ sharenet.DHT = &MockDHT{}
}

func TestMockDHT_Announce(t *testing.T) {
	m := &MockDHT{}
	if err := m.Announce(context.Background(), "hash-abc", 8080); err != nil {
		t.Fatal(err)
	}
	if len(m.announced) != 1 || m.announced[0] != "hash-abc" {
		t.Fatalf("expected announced hash-abc, got %v", m.announced)
	}
}

func TestMockDHT_Lookup(t *testing.T) {
	peer, _ := netip.ParseAddrPort("1.2.3.4:8080")
	m := &MockDHT{peers: []netip.AddrPort{peer}}

	got, err := m.Lookup(context.Background(), "hash-abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != peer {
		t.Fatalf("expected peer %v, got %v", peer, got)
	}
}

func TestMockDHT_AnnounceError(t *testing.T) {
	m := &MockDHT{announceErr: errors.New("network down")}
	err := m.Announce(context.Background(), "hash-abc", 8080)
	if err == nil {
		t.Fatal("expected error")
	}
}
