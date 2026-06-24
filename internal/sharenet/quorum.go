package sharenet

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// quorum accumulates metadata sets fetched from peers and reports when at least
// minPeers DISTINCT peer IPs have served a byte-identical set. Distinct-IP
// voting prevents a single peer (even one announcing many ports) from forming a
// quorum alone, raising the bar on metadata poisoning.
type quorum struct {
	minPeers int
	votes    map[string]int           // fingerprint → distinct-IP count
	first    map[string]*ReleaseFiles // fingerprint → first metaset seen
	votedIP  map[string]bool          // IP → already counted
}

func newQuorum(minPeers int) *quorum {
	if minPeers < 1 {
		minPeers = 1
	}
	return &quorum{
		minPeers: minPeers,
		votes:    make(map[string]int),
		first:    make(map[string]*ReleaseFiles),
		votedIP:  make(map[string]bool),
	}
}

// add records files served by ip. It returns the agreed metaset once minPeers
// distinct IPs have served an identical set, or nil if the quorum is not yet met.
// A repeat vote from an IP already counted is ignored.
func (q *quorum) add(ip string, files *ReleaseFiles) *ReleaseFiles {
	if q.votedIP[ip] {
		return nil
	}
	q.votedIP[ip] = true

	fp := fingerprint(files)
	q.votes[fp]++
	if q.first[fp] == nil {
		q.first[fp] = files
	}
	if q.votes[fp] >= q.minPeers {
		return q.first[fp]
	}
	return nil
}

// fingerprint is a deterministic digest of a metaset: for each meta, the virtual
// path and the SHA-256 of its raw on-disk bytes, sorted by path so order does
// not matter. Two peers that independently imported the same NZB produce
// byte-identical v3 metas and thus the same fingerprint; a poisoned set differs.
func fingerprint(files *ReleaseFiles) string {
	lines := make([]string, 0, len(files.Metas))
	for _, m := range files.Metas {
		sum := sha256.Sum256(m.Raw)
		lines = append(lines, m.VirtualPath+"\x00"+hex.EncodeToString(sum[:]))
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:])
}
