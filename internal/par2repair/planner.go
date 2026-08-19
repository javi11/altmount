package par2repair

import (
	"errors"
	"fmt"
	"sort"

	"github.com/javi11/altmount/internal/importer/parser/par2"
)

// ErrUnrepairable marks damage that PAR2 repair cannot or may not fix:
// insufficient recovery slices, policy caps exceeded, or failed verification.
// The caller routes such files to the existing ARR/corruption path.
var ErrUnrepairable = errors.New("par2repair: unrepairable")

// ErrNothingToRepair is returned when no article in the recovery set is dead.
var ErrNothingToRepair = errors.New("par2repair: nothing to repair")

// Article is one usenet article of a recovery-set file.
type Article struct {
	MessageID string
	Size      int64 // decoded payload size
	Dead      bool  // confirmed missing on all providers
}

// SetFile is one recovery-set member resolved to its usenet articles, whose
// concatenated payloads are exactly the file's bytes.
type SetFile struct {
	FileID   [16]byte
	Length   uint64
	Articles []Article
}

// DeadArticle locates one dead article within the recovery set.
type DeadArticle struct {
	FileIdx   int // index into Plan.Files
	ArtIdx    int
	MessageID string
	FileStart int64 // byte offset of the article within its file
	Size      int64
}

// Caps is the repair policy: how much damage a job may fix.
type Caps struct {
	// MaxRepairRatio caps missing bytes over the damaged files' total bytes.
	MaxRepairRatio float64
	// MaxMemoryBytes bounds the solver's in-heap accumulator memory (k × slice
	// size). Jobs over the bound still run, backed by a disk arena.
	MaxMemoryBytes int64
}

// Plan is everything a repair job needs to run: which slices to reconstruct,
// which recovery slices to use, and the article layout of the recovery set.
type Plan struct {
	SliceSize     int
	GlobalSlices  int
	Missing       []int // global slice indices, ascending
	Recovery      []par2.RecoverySliceRef
	SpareRecovery []par2.RecoverySliceRef
	Files         []SetFile // recovery-set order (FileID ascending)
	DeadArticles  []DeadArticle
	// SpillToDisk marks a plan whose solver buffers exceed the memory budget:
	// the job backs accumulators, recovery payloads and recovered slices with
	// a memory-mapped scratch file instead of the heap.
	SpillToDisk bool
}

// BuildPlan maps dead articles to global recovery-set slices and selects
// recovery slices, enforcing the caps. files may arrive in any order but must
// contain every recovery-set member of idx.
func BuildPlan(idx *par2.Index, files []SetFile, caps Caps) (*Plan, error) {
	if idx.SliceSize == 0 || idx.SliceSize%4 != 0 {
		return nil, fmt.Errorf("par2repair: invalid slice size %d", idx.SliceSize)
	}
	sliceSize := int64(idx.SliceSize)

	byID := make(map[[16]byte]SetFile, len(files))
	for _, f := range files {
		byID[f.FileID] = f
	}

	plan := &Plan{SliceSize: int(sliceSize)}
	var startSlice []int64
	var total int64
	for _, id := range idx.RecoveryIDs {
		f, ok := byID[id]
		if !ok {
			fd := idx.Files[id]
			return nil, fmt.Errorf("par2repair: recovery-set member %q (%x) not resolved to usenet articles", fd.Name, id)
		}
		if sum := articleSizeSum(f.Articles); sum != int64(f.Length) {
			return nil, fmt.Errorf("par2repair: file %x: article sizes sum to %d, want %d", id, sum, f.Length)
		}
		plan.Files = append(plan.Files, f)
		startSlice = append(startSlice, total)
		total += (int64(f.Length) + sliceSize - 1) / sliceSize
	}
	plan.GlobalSlices = int(total)

	// Map dead articles to global slice indices.
	missingSet := map[int]bool{}
	var damagedFileBytes int64
	damagedFiles := map[int]bool{}
	for fi, f := range plan.Files {
		var off int64
		for ai, a := range f.Articles {
			if a.Dead {
				first := startSlice[fi] + off/sliceSize
				last := startSlice[fi] + (off+a.Size-1)/sliceSize
				for s := first; s <= last; s++ {
					missingSet[int(s)] = true
				}
				plan.DeadArticles = append(plan.DeadArticles, DeadArticle{
					FileIdx: fi, ArtIdx: ai, MessageID: a.MessageID,
					FileStart: off, Size: a.Size,
				})
				if !damagedFiles[fi] {
					damagedFiles[fi] = true
					damagedFileBytes += int64(f.Length)
				}
			}
			off += a.Size
		}
	}
	if len(missingSet) == 0 {
		return nil, ErrNothingToRepair
	}
	for s := range missingSet {
		plan.Missing = append(plan.Missing, s)
	}
	sort.Ints(plan.Missing)

	// Caps.
	k := len(plan.Missing)
	missingBytes := int64(k) * sliceSize
	plan.SpillToDisk = caps.MaxMemoryBytes > 0 && missingBytes > caps.MaxMemoryBytes
	if caps.MaxRepairRatio > 0 && damagedFileBytes > 0 {
		if ratio := float64(missingBytes) / float64(damagedFileBytes); ratio > caps.MaxRepairRatio {
			return nil, fmt.Errorf("%w: damage ratio %.4f exceeds max_repair_ratio %.4f",
				ErrUnrepairable, ratio, caps.MaxRepairRatio)
		}
	}
	if len(idx.Recovery) < k {
		return nil, fmt.Errorf("%w: needs %d recovery slices, set has %d",
			ErrUnrepairable, k, len(idx.Recovery))
	}

	// Choose the k lowest-exponent recovery slices; the rest are spares for
	// singular-matrix or dead-recovery-article retries.
	recs := make([]par2.RecoverySliceRef, len(idx.Recovery))
	copy(recs, idx.Recovery)
	sort.Slice(recs, func(i, j int) bool { return recs[i].Exponent < recs[j].Exponent })
	plan.Recovery = recs[:k]
	plan.SpareRecovery = recs[k:]

	return plan, nil
}

func articleSizeSum(arts []Article) int64 {
	var sum int64
	for _, a := range arts {
		sum += a.Size
	}
	return sum
}
