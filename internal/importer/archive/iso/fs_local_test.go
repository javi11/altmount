package iso

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestUDFWalk_LogsWhenFileICBHasUnknownTag drives a synthetic UDF blob with
// one directory containing one File Identifier Descriptor (BOGUS.M2TS) whose
// ICB points at a sector containing an invalid descriptor tag (id=999, not
// 261/266). The walker must:
//
//  1. drop the file from its returned listing (silent today, kept silent);
//  2. emit exactly one slog.WarnContext line naming the file and the bogus
//     tag id so operators can see why a file vanished.
//
// This locks in the diagnostic behavior added by Task 6: every silent drop
// site in udfWalkAll / collectFileExtents now logs at WARN level before
// continuing or breaking.
func TestUDFWalk_LogsWhenFileICBHasUnknownTag(t *testing.T) {
	// Capture default slog output into a buffer for assertions. NOTE: this
	// test mutates the process-wide default slog logger. Do NOT call
	// t.Parallel() here, and do not parallelise any other test in this
	// package that touches slog output, or log lines will bleed between
	// tests and the matches==1 assertion below will flake.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Build a minimal in-memory blob: 32 sectors of zeros, with custom
	// content at sector 10 (directory FE) and sector 20 (bogus tag).
	const dirSector = 10
	const bogusSector = 20
	image := make([]byte, iso9660SectorSize*32)

	// Sector 10: a UDF File Entry (tag 261) acting as a directory whose
	// allocation type is 3 (inline), so udfReadDirEntries reads the FID
	// straight out of buf[allocDescOff : allocDescOff+allocDescLen].
	dir := image[dirSector*iso9660SectorSize : (dirSector+1)*iso9660SectorSize]
	binary.LittleEndian.PutUint16(dir[0:2], 261) // tag.id = 261 (File Entry)
	dir[34] = 3                                  // icbtag.flags lower 3 bits = 3 (inline)
	// FE plain (tag 261) AD-area header at buf[168..176].
	binary.LittleEndian.PutUint32(dir[168:172], 0)  // L_EA (extended attrs length)
	binary.LittleEndian.PutUint32(dir[172:176], 52) // L_AD (alloc-desc length, == one padded FID)

	// FID at dir[176..]: file identifier descriptor for BOGUS.M2TS
	// pointing its ICB long_ad at sector `bogusSector`.
	fid := dir[176:]
	name := "BOGUS.M2TS"                            // 10 ASCII bytes
	binary.LittleEndian.PutUint16(fid[0:2], 257)    // FID tag id
	fid[18] = 0                                     // file characteristics: regular file, neither parent nor deleted
	fid[19] = byte(1 + len(name))                   // L_FI (comp byte + ASCII chars)
	binary.LittleEndian.PutUint32(fid[20:24], 2048) // long_ad.length
	binary.LittleEndian.PutUint32(fid[24:28], bogusSector)
	binary.LittleEndian.PutUint16(fid[28:30], 0)   // long_ad.partition (0 → partStart-relative)
	binary.LittleEndian.PutUint16(fid[36:38], 0)   // L_IU (impl-use length)
	fid[38] = 8                                    // CS0 compression code (8 = ASCII)
	copy(fid[39:39+len(name)], name)
	// Padded record length (38 header + 11 name = 49, padded to 52). We
	// leave the trailing 3 bytes as zeros from the make().

	// Sector 20: descriptor tag with the deliberately-bogus id 999.
	bogus := image[bogusSector*iso9660SectorSize : (bogusSector+1)*iso9660SectorSize]
	binary.LittleEndian.PutUint16(bogus[0:2], 999)

	dirICB := udfLongAD{length: iso9660SectorSize, loc: udfLBA{block: dirSector, part: 0}}
	entries, err := udfWalkAll(context.Background(), bytes.NewReader(image), dirICB, nil, 0, "")
	if err != nil {
		t.Fatalf("udfWalkAll: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty listing (bogus file should be dropped); got %d entries: %+v", len(entries), entries)
	}

	// Inspect captured slog output. Parse line by line as JSON and count
	// matches; the test fails if not exactly one matching WARN was emitted.
	var matches int
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("non-JSON log line %q: %v", line, err)
		}
		if rec["level"] != "WARN" {
			continue
		}
		// Both path and tag_id must be set to disambiguate from any
		// other (future) WARN site in the walk.
		if rec["path"] != "BOGUS.M2TS" {
			continue
		}
		// JSON-decoded numbers come back as float64; compare via that.
		if v, ok := rec["tag_id"].(float64); !ok || int(v) != 999 {
			continue
		}
		matches++
	}
	if matches != 1 {
		t.Fatalf("want exactly 1 matching WARN line (path=BOGUS.M2TS tag_id=999), got %d. Full log:\n%s",
			matches, buf.String())
	}
}

// TestLocalISO_DiscoverBigFiles is a manual integration test: it walks a
// real Blu-ray ISO from local disk and dumps a size-sorted summary. Skipped
// unless ALTMOUNT_LOCAL_ISO is set, so CI stays unaffected.
//
// Set ALTMOUNT_LOCAL_ISO=/abs/path/to.iso to run, e.g.:
//
//	ALTMOUNT_LOCAL_ISO=/Volumes/.../DISC_1.iso go test \
//	  ./internal/importer/archive/iso/... -run TestLocalISO -v
func TestLocalISO_DiscoverBigFiles(t *testing.T) {
	path := os.Getenv("ALTMOUNT_LOCAL_ISO")
	if path == "" {
		t.Skip("ALTMOUNT_LOCAL_ISO not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	t.Logf("ISO: %s  size=%d (%.2f GiB)", path, stat.Size(), float64(stat.Size())/(1<<30))

	entries, err := ListISOFiles(context.Background(), f)
	if err != nil {
		t.Fatalf("ListISOFiles: %v", err)
	}

	var sum int64
	for _, e := range entries {
		sum += int64(e.size)
	}
	t.Logf("listed_files=%d  listed_sum=%d (%.2f GiB)  coverage=%.1f%%",
		len(entries), sum, float64(sum)/(1<<30), 100*float64(sum)/float64(stat.Size()))

	// Top 25 by size — should match `ls -laS BDMV/STREAM/` if walker is sane.
	sort.Slice(entries, func(i, j int) bool { return entries[i].size > entries[j].size })
	t.Logf("top 25 by size:")
	for i, e := range entries {
		if i >= 25 {
			break
		}
		t.Logf("  %s  size=%d (%.2f MiB)  extents=%d  first_lba=%d",
			e.path, e.size, float64(e.size)/(1<<20), len(e.extents), e.firstLBA())
	}

	// Sanity sentinels for the Avatar disc 1 main-feature clips. Each is
	// >1 GiB and uses many on-disc extents (00022.m2ts has ~945). Assert
	// the file is present, the size is right, AND the extents slice fully
	// covers it — otherwise downstream concat reads wrong bytes past the
	// first extent.
	want := []string{"BDMV/STREAM/00016.m2ts", "BDMV/STREAM/00022.m2ts", "BDMV/STREAM/00028.m2ts"}
	have := make(map[string]isoFileEntry, len(entries))
	for _, e := range entries {
		have[e.path] = e
	}
	for _, w := range want {
		e, ok := have[w]
		if !ok {
			t.Errorf("missing %s — walker dropped this file", w)
			continue
		}
		if e.size < 1<<30 {
			t.Errorf("%s reported size=%d (%.2f MiB), want >1 GiB",
				w, e.size, float64(e.size)/(1<<20))
		}
		if len(e.extents) < 2 {
			t.Errorf("%s has only %d extents — expected multi-extent (BD main-feature clips fragment heavily)",
				w, len(e.extents))
		}
		var covered uint64
		for _, ext := range e.extents {
			covered += ext.length
		}
		if covered != e.size {
			t.Errorf("%s: sum of extent lengths = %d but file size = %d (delta %d)",
				w, covered, e.size, int64(e.size)-int64(covered))
		}
	}

	if t.Failed() {
		fmt.Println(">>> walker is dropping big files; this is the bug")
	}
}

// TestLocalISO_CountExtentsForBigFiles probes each entry's File Entry on the
// real ISO and reports how many allocation descriptors a file's data uses.
// The walker today reads only the first AD — if any of the multi-GiB main-
// feature clips reports >1 AD, downstream byte reads past the first extent
// will hit wrong sectors. Gated on ALTMOUNT_LOCAL_ISO same as the discovery
// test.
func TestLocalISO_CountExtentsForBigFiles(t *testing.T) {
	path := os.Getenv("ALTMOUNT_LOCAL_ISO")
	if path == "" {
		t.Skip("ALTMOUNT_LOCAL_ISO not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	partStart, metaMap, rootICB, err := udfSetup(f)
	if err != nil {
		t.Fatalf("udfSetup: %v", err)
	}

	// Re-walk to get entries plus their ICB so we can re-read each FE and
	// count its allocation descriptors. We can't reuse ListISOFiles output
	// directly because isoFileEntry discards the ICB.
	type probed struct {
		path string
		size uint64
		ads  int // allocation descriptors observed (= number of on-disc extents)
		alloc byte
	}

	var probedAll []probed
	var walk func(dirICB udfLongAD, prefix string)
	walk = func(dirICB udfLongAD, prefix string) {
		physSect, e := udfResolveICB(dirICB.loc, metaMap, partStart)
		if e != nil {
			return
		}
		entries, e := udfReadDirEntries(context.Background(), f, physSect, metaMap, partStart)
		if e != nil {
			return
		}
		for _, ent := range entries {
			p := ent.name
			if prefix != "" {
				p = prefix + "/" + ent.name
			}
			if ent.isDir {
				walk(ent.icb, p)
				continue
			}
			fePhys, rerr := udfResolveICB(ent.icb.loc, metaMap, partStart)
			if rerr != nil {
				continue
			}
			feTag, feBuf, rerr := udfReadTag(f, fePhys)
			if rerr != nil || (feTag.id != 261 && feTag.id != 266) {
				continue
			}
			alloc := feBuf[34] & 0x07
			var adOff, adLen int
			if feTag.id == 266 {
				eaLen := int(binary.LittleEndian.Uint32(feBuf[208:212]))
				adLen = int(binary.LittleEndian.Uint32(feBuf[212:216]))
				adOff = 216 + eaLen
			} else {
				eaLen := int(binary.LittleEndian.Uint32(feBuf[168:172]))
				adLen = int(binary.LittleEndian.Uint32(feBuf[172:176]))
				adOff = 176 + eaLen
			}
			if adOff+adLen > len(feBuf) {
				adLen = len(feBuf) - adOff
			}
			// Count extents using the UDF rules: high 2 bits of the
			// length field encode the AD "type":
			//   0 = recorded and allocated (real extent)
			//   1 = not recorded, allocated (sparse / zero-fill)
			//   2 = not recorded, not allocated (sparse hole)
			//   3 = next AD points at a continuation AED sector, follow it
			// We count types 0,1,2 as logical extents (each contributes
			// length bytes to the file) and chase type 3 into AED chains.
			n := 0
			step := 0
			switch alloc {
			case 0:
				step = 8
			case 1:
				step = 16
			case 2:
				step = 20
			case 3:
				n = 1 // embedded
			}
			if step > 0 {
				countADs := func(buf []byte) (extents int, chain *udfLongAD) {
					for off := 0; off+step <= len(buf); off += step {
						lenField := binary.LittleEndian.Uint32(buf[off:])
						adType := lenField >> 30
						adLen := lenField & 0x3FFFFFFF
						if adLen == 0 && adType != 3 {
							break
						}
						if adType == 3 {
							var loc udfLongAD
							switch step {
							case 8:
								loc = udfLongAD{length: adLen, loc: udfLBA{block: binary.LittleEndian.Uint32(buf[off+4:])}}
							case 16:
								loc = udfParseLongAD(buf, off)
							}
							return extents, &loc
						}
						extents++
					}
					return extents, nil
				}
				cnt, chain := countADs(feBuf[adOff : adOff+adLen])
				n = cnt
				safety := 0
				for chain != nil && safety < 100 {
					safety++
					ps, e := udfResolveICB(chain.loc, metaMap, partStart)
					if e != nil {
						break
					}
					_, aedBuf, e := udfReadTag(f, ps)
					if e != nil {
						break
					}
					// AED layout: 16-byte tag + 4-byte previous-AED pointer
					// + 4-byte length-of-allocation-descriptors + ADs.
					if len(aedBuf) < 24 {
						break
					}
					aedLen := int(binary.LittleEndian.Uint32(aedBuf[20:24]))
					if aedLen <= 0 || 24+aedLen > len(aedBuf) {
						break
					}
					more, nextChain := countADs(aedBuf[24 : 24+aedLen])
					n += more
					chain = nextChain
				}
			}
			probedAll = append(probedAll, probed{
				path:  p,
				size:  binary.LittleEndian.Uint64(feBuf[56:64]),
				ads:   n,
				alloc: alloc,
			})
		}
	}
	walk(rootICB, "")

	// Report the big files specifically + any file with >1 AD.
	sort.Slice(probedAll, func(i, j int) bool { return probedAll[i].size > probedAll[j].size })
	t.Logf("top 15 by size (with extent count):")
	for i, p := range probedAll {
		if i >= 15 {
			break
		}
		t.Logf("  %s  size=%d (%.2f MiB)  alloc_type=%d  extents=%d",
			p.path, p.size, float64(p.size)/(1<<20), p.alloc, p.ads)
	}

	multi := 0
	for _, p := range probedAll {
		if p.ads > 1 {
			multi++
		}
	}
	t.Logf("files with >1 extent: %d / %d", multi, len(probedAll))
	if multi == 0 {
		t.Logf("CONCLUSION: all files are contiguous — single-LBA model is sufficient for this ISO")
	} else {
		t.Logf("CONCLUSION: fragmentation present — single-LBA walker yields WRONG bytes past extent 1")
	}
}

// TestLocalISO_CountAdjacentExtents checks whether multi-extent files have
// physically contiguous extents that could be coalesced. If yes, segment
// count downstream can be reduced dramatically — the importer hit
// total_segments_to_validate=888,903 on this NZB precisely because every
// AD became its own NestedSource even when adjacent ADs sat next to each
// other on disc.
func TestLocalISO_CountAdjacentExtents(t *testing.T) {
	path := os.Getenv("ALTMOUNT_LOCAL_ISO")
	if path == "" {
		t.Skip("ALTMOUNT_LOCAL_ISO not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	entries, err := ListISOFiles(context.Background(), f)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].size > entries[j].size })

	const lookAt = 15
	for i, e := range entries {
		if i >= lookAt {
			break
		}
		if len(e.extents) <= 1 {
			continue
		}
		// Count adjacent runs (where next.lba == this.lba + this.length/sector).
		adjacent := 0
		distinctRuns := 1
		for j := 1; j < len(e.extents); j++ {
			prev := e.extents[j-1]
			next := e.extents[j]
			expectedNextLBA := prev.lba + uint32(prev.length/iso9660SectorSize)
			if next.lba == expectedNextLBA {
				adjacent++
			} else {
				distinctRuns++
			}
		}
		t.Logf("  %s: extents=%d adjacent_pairs=%d distinct_runs=%d coalesce_ratio=%.1fx",
			e.path, len(e.extents), adjacent, distinctRuns,
			float64(len(e.extents))/float64(distinctRuns))
	}
}
