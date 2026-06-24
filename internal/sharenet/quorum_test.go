package sharenet

import "testing"

func files(raw string) *ReleaseFiles {
	return &ReleaseFiles{Metas: []SharedMeta{{VirtualPath: "M/a.mkv", Raw: []byte(raw)}}}
}

func TestQuorum_Default_TrustsFirstPeer(t *testing.T) {
	q := newQuorum(1)
	if got := q.add("1.1.1.1", files("meta")); got == nil {
		t.Fatal("min_peers=1 must accept the first peer")
	}
}

func TestQuorum_RequiresAgreement(t *testing.T) {
	q := newQuorum(2)
	if got := q.add("1.1.1.1", files("meta")); got != nil {
		t.Fatal("one peer must not satisfy a quorum of 2")
	}
	got := q.add("2.2.2.2", files("meta")) // distinct IP, identical metaset
	if got == nil {
		t.Fatal("two distinct IPs agreeing must satisfy the quorum")
	}
}

func TestQuorum_OneVotePerIP(t *testing.T) {
	q := newQuorum(2)
	q.add("1.1.1.1", files("meta"))
	if got := q.add("1.1.1.1", files("meta")); got != nil {
		t.Fatal("the same IP must not be able to vote twice toward a quorum")
	}
}

func TestQuorum_DisagreementNeverReachesQuorum(t *testing.T) {
	q := newQuorum(2)
	if got := q.add("1.1.1.1", files("good")); got != nil {
		t.Fatal("first distinct metaset must not meet quorum alone")
	}
	if got := q.add("2.2.2.2", files("poisoned")); got != nil {
		t.Fatal("two peers serving different metasets must never reach quorum")
	}
}

func TestFingerprint_PathOrderIndependent(t *testing.T) {
	a := &ReleaseFiles{Metas: []SharedMeta{
		{VirtualPath: "M/a.mkv", Raw: []byte("x")},
		{VirtualPath: "M/b.mkv", Raw: []byte("y")},
	}}
	b := &ReleaseFiles{Metas: []SharedMeta{ // same set, reversed order
		{VirtualPath: "M/b.mkv", Raw: []byte("y")},
		{VirtualPath: "M/a.mkv", Raw: []byte("x")},
	}}
	if fingerprint(a) != fingerprint(b) {
		t.Fatal("fingerprint must be independent of meta order")
	}
}
