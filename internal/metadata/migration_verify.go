package metadata

import (
	"fmt"
	"os"
	"path/filepath"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// sameResolvedSegments reports whether two resolved segment lists are
// equivalent for streaming purposes: same ids, same decoded sizes, same byte
// ranges, in the same order. label names the list in the error so a failure
// says which part of the file disagreed.
func sameResolvedSegments(label string, want, got []*metapb.SegmentData) error {
	if len(want) != len(got) {
		return fmt.Errorf("%s segment count changed: was %d, now %d", label, len(want), len(got))
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.Id != g.Id || w.SegmentSize != g.SegmentSize ||
			w.StartOffset != g.StartOffset || w.EndOffset != g.EndOffset {
			return fmt.Errorf(
				"%s segment %d changed: was {id=%s size=%d start=%d end=%d}, now {id=%s size=%d start=%d end=%d}",
				label, i,
				w.Id, w.SegmentSize, w.StartOffset, w.EndOffset,
				g.Id, g.SegmentSize, g.StartOffset, g.EndOffset,
			)
		}
	}
	return nil
}

// verifyMigratedFile re-reads a just-migrated .meta from disk and confirms it
// resolves, through the shared store, to exactly the segments the legacy file
// carried inline — main file, every PAR2 file, and every nested source.
//
// This is the migration's own proof of correctness. Rather than relying on test
// coverage to have anticipated every metadata shape in the wild, each file is
// checked individually against itself, and MigrateGroup restores any file whose
// check fails. It is a package-level var so tests can force the failure path.
var verifyMigratedFile = func(ms *MetadataService, virtualPath string, original *metapb.FileMetadata) error {
	reread, err := ms.ReadFileMetadata(virtualPath)
	if err != nil {
		return fmt.Errorf("re-read after migration: %w", err)
	}
	if reread == nil {
		return fmt.Errorf("re-read after migration returned no metadata")
	}

	if err := sameResolvedSegments("main", original.SegmentData, reread.SegmentData); err != nil {
		return err
	}

	if len(original.Par2Files) != len(reread.Par2Files) {
		return fmt.Errorf("par2 file count changed: was %d, now %d",
			len(original.Par2Files), len(reread.Par2Files))
	}
	for i := range original.Par2Files {
		label := fmt.Sprintf("par2[%d] (%s)", i, original.Par2Files[i].Filename)
		if err := sameResolvedSegments(label, original.Par2Files[i].SegmentData, reread.Par2Files[i].SegmentData); err != nil {
			return err
		}
	}

	if len(original.NestedSources) != len(reread.NestedSources) {
		return fmt.Errorf("nested source count changed: was %d, now %d",
			len(original.NestedSources), len(reread.NestedSources))
	}
	for i := range original.NestedSources {
		o, r := original.NestedSources[i], reread.NestedSources[i]
		if err := sameResolvedSegments(fmt.Sprintf("nested[%d]", i), o.Segments, r.Segments); err != nil {
			return err
		}
		// The cipher and extent geometry matter as much as the segments: a
		// wrong inner volume size silently corrupts an encrypted read.
		if o.InnerOffset != r.InnerOffset || o.InnerLength != r.InnerLength {
			return fmt.Errorf("nested[%d] extent changed: was offset=%d len=%d, now offset=%d len=%d",
				i, o.InnerOffset, o.InnerLength, r.InnerOffset, r.InnerLength)
		}
		if o.InnerVolumeSize != r.InnerVolumeSize {
			return fmt.Errorf("nested[%d] inner volume size changed: was %d, now %d",
				i, o.InnerVolumeSize, r.InnerVolumeSize)
		}
		if string(o.AesKey) != string(r.AesKey) || string(o.AesIv) != string(r.AesIv) {
			return fmt.Errorf("nested[%d] AES key material changed", i)
		}
	}

	// File-level crypto and geometry must be untouched by a format change.
	if original.FileSize != reread.FileSize {
		return fmt.Errorf("file size changed: was %d, now %d", original.FileSize, reread.FileSize)
	}
	if original.Encryption != reread.Encryption {
		return fmt.Errorf("encryption changed: was %v, now %v", original.Encryption, reread.Encryption)
	}
	if string(original.AesKey) != string(reread.AesKey) || string(original.AesIv) != string(reread.AesIv) {
		return fmt.Errorf("AES key material changed")
	}
	if original.Password != reread.Password || original.Salt != reread.Salt {
		return fmt.Errorf("rclone password/salt changed")
	}
	if len(original.ClipBoundaries) != len(reread.ClipBoundaries) {
		return fmt.Errorf("clip boundary count changed: was %d, now %d",
			len(original.ClipBoundaries), len(reread.ClipBoundaries))
	}
	for i := range original.ClipBoundaries {
		if original.ClipBoundaries[i].ByteLen != reread.ClipBoundaries[i].ByteLen ||
			original.ClipBoundaries[i].Delta_90K != reread.ClipBoundaries[i].Delta_90K {
			return fmt.Errorf("clip boundary %d changed", i)
		}
	}
	return nil
}

// restoreLegacyMeta writes raw bytes back to a .meta path atomically, undoing a
// migration for one file. Used when verification fails: the legacy format still
// works, so leaving the file untouched is always the safe outcome.
func restoreLegacyMeta(metaPath string, original []byte) error {
	dir := filepath.Dir(metaPath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(metaPath)+".restore.*.tmp")
	if err != nil {
		return fmt.Errorf("create restore temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(original); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write restore temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close restore temp file: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename restored metadata: %w", err)
	}
	return nil
}
