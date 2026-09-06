package archive

import (
	"path"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
)

// payloadDominanceFactor is how much bigger than the runner-up the largest
// entry must be before it can be called the release. A release is one large
// file beside small ones; several files of a size are a set (a season pack in
// one archive), and renaming one of them to the release name would be a guess.
const payloadDominanceFactor = 3

// excludedFromRename are extensions a payload is never renamed under: disc
// structures and split containers, where a player finds the next piece by name.
var excludedFromRename = map[string]bool{
	".vob": true, ".rar": true, ".par2": true, ".mts": true, ".m2ts": true,
	".cpi": true, ".clpi": true, ".mpl": true, ".mpls": true, ".bdm": true, ".bdmv": true,
}

// discStructureDirs name a ripped disc, where every filename is part of an
// index the player reads.
var discStructureDirs = map[string]bool{"video_ts": true, "audio_ts": true, "bdmv": true}

// PayloadRenames decides which archive entries are presented under the release
// name, following SABnzbd's final-filename deobfuscation: only the entry that
// is clearly the release — largest by a wide margin, not part of a disc
// structure, and carrying a name that says nothing — is renamed, and files
// sharing its stem (subtitles, samples) follow it so they stay paired.
//
// Keys are the entries' InternalPath as given; values are the new leaf name.
// A nil result means nothing should be renamed.
func PayloadRenames(contents []Content, releaseName string) map[string]string {
	release := strings.TrimSpace(path.Base(strings.ReplaceAll(releaseName, `\`, "/")))
	if release == "" || release == "." || release == "/" || len(contents) == 0 {
		return nil
	}

	var biggest *Content
	var runnerUp int64
	for i := range contents {
		c := &contents[i]
		if c.IsDirectory || c.Size == 0 {
			continue
		}
		if hasDiscStructure(normalizePath(c.InternalPath)) {
			return nil
		}
		switch {
		case biggest == nil || c.Size > biggest.Size:
			if biggest != nil {
				runnerUp = biggest.Size
			}
			biggest = c
		case c.Size > runnerUp:
			runnerUp = c.Size
		}
	}
	if biggest == nil {
		return nil
	}
	if runnerUp > 0 && biggest.Size < runnerUp*payloadDominanceFactor {
		return nil
	}

	payload := normalizePath(biggest.InternalPath)
	ext := path.Ext(payload)
	if excludedFromRename[strings.ToLower(ext)] {
		return nil
	}
	leaf := path.Base(payload)
	if !fileinfo.IsProbablyObfuscated(leaf) {
		return nil
	}

	// "Movie.2019.mkv" must not become "Movie.2019.mkv.mkv".
	target := release
	if strings.EqualFold(path.Ext(target), ext) {
		target = strings.TrimSuffix(target, path.Ext(target))
	}

	stem := strings.TrimSuffix(leaf, ext)
	dir := path.Dir(payload)

	taken := make(map[string]struct{}, len(contents))
	for _, c := range contents {
		taken[strings.ToLower(normalizePath(c.InternalPath))] = struct{}{}
	}

	renames := make(map[string]string)
	for _, c := range contents {
		if c.IsDirectory {
			continue
		}
		current := normalizePath(c.InternalPath)
		if path.Dir(current) != dir || !strings.HasPrefix(path.Base(current), stem) {
			continue
		}
		newLeaf := target + strings.TrimPrefix(path.Base(current), stem)
		renamed := newLeaf
		if dir != "." && dir != "/" {
			renamed = dir + "/" + newLeaf
		}
		// Renaming onto a name the archive already uses would hide one file
		// behind another; the hash names are better than a lost entry.
		if _, clash := taken[strings.ToLower(renamed)]; clash && !strings.EqualFold(renamed, current) {
			return nil
		}
		renames[c.InternalPath] = newLeaf
	}
	return renames
}

func normalizePath(p string) string {
	return path.Clean(strings.ReplaceAll(p, `\`, "/"))
}

func hasDiscStructure(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if discStructureDirs[strings.ToLower(part)] {
			return true
		}
	}
	return false
}
