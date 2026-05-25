package par2

import (
	"crypto/md5"
	"errors"
	"hash/crc32"
)

// ErrVerificationFailed signals that a reconstructed slice did not match the
// PAR2 IFSC checksums. Serving such bytes would be strictly worse than a full
// re-download (it would silently cache and stream corrupt data), so callers
// must treat this as a hard failure and fall back to the ARR re-download path.
var ErrVerificationFailed = errors.New("par2: reconstructed data failed checksum verification")

// verifySlices checks reconstructed slices against the IFSC checksums for the
// single recovery-set file. idxs are the slice indices to check (the
// reconstructed ones), relative to fileID's slice ordering — for the only
// supported case (single-file set) the global block index equals the file
// slice index. Returns the first index that fails its CRC32 or MD5, or -1 if
// all pass. When no IFSC data is present it returns -1 (nothing to verify
// against) rather than hard-failing.
//
// PAR2 computes both the CRC32 (standard IEEE polynomial) and MD5 over the
// zero-padded slice of exactly SliceSize bytes, so verifying the full
// reconstructed block — which Reconstruct returns zero-padded — is correct.
func (rs *RecoverySet) verifySlices(fileID [16]byte, slices [][]byte, idxs []int) int {
	sums := rs.SliceCRCs[fileID]
	if sums == nil {
		return -1 // no IFSC data: nothing to verify against
	}
	for _, i := range idxs {
		if i < 0 || i >= len(sums) || i >= len(slices) || slices[i] == nil {
			continue
		}
		want := sums[i]
		if crc32.ChecksumIEEE(slices[i]) != want.CRC32 {
			return i
		}
		if md5.Sum(slices[i]) != want.MD5 {
			return i
		}
	}
	return -1
}
