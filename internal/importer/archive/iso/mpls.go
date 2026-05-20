package iso

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MPLS (Blu-ray PlayList) is a fixed binary format defined by the BDA spec.
// We only parse the fields needed to identify the main feature playlist and
// its ordered list of M2TS clips: the clip_information_file_name for each
// PlayItem and the IN/OUT presentation times used to estimate duration.

// mplsHeaderSize is the fixed prefix length: 4 magic + 4 version +
// 4 PlayList offset + 4 PlayListMark offset + 4 ExtensionData offset.
const mplsHeaderSize = 20

// MPLSPlayItem describes one entry in a PlayList.
type MPLSPlayItem struct {
	// ClipName is the 5-character clip_information_file_name (e.g. "00001").
	// The corresponding stream lives at BDMV/STREAM/<ClipName>.M2TS.
	ClipName string
	// InTime and OutTime are 45 kHz presentation timestamps. Duration in
	// ticks is OutTime - InTime; convert to seconds by dividing by 45000.
	InTime  uint32
	OutTime uint32
}

// MPLSPlayList is the parsed view of a single .mpls file.
type MPLSPlayList struct {
	Version   string // e.g. "0100", "0200", "0300"
	PlayItems []MPLSPlayItem
}

// DurationTicks returns the sum of (OutTime-InTime) across PlayItems in
// 45 kHz ticks. This is the standard proxy for "longest playlist =
// main feature" used by every Blu-ray player.
func (p *MPLSPlayList) DurationTicks() int64 {
	var total int64
	for _, it := range p.PlayItems {
		if it.OutTime > it.InTime {
			total += int64(it.OutTime - it.InTime)
		}
	}
	return total
}

// ParseMPLS decodes a .mpls file. All multi-byte integers are big-endian
// per the BDA spec. Sub-paths, the STN table, and per-angle alternates
// are skipped — we use each PlayItem's leading length field to advance
// past everything we don't need.
func ParseMPLS(data []byte) (*MPLSPlayList, error) {
	if len(data) < mplsHeaderSize {
		return nil, errors.New("mpls: truncated header")
	}
	if string(data[0:4]) != "MPLS" {
		return nil, fmt.Errorf("mpls: bad magic %q", data[0:4])
	}
	version := string(data[4:8])
	playListOff := binary.BigEndian.Uint32(data[8:12])
	if int(playListOff) < mplsHeaderSize || int(playListOff)+10 > len(data) {
		return nil, fmt.Errorf("mpls: PlayList offset %d out of range (file size %d)", playListOff, len(data))
	}

	// PlayList header: length(4) + reserved(2) + numPlayItems(2) + numSubPaths(2)
	pl := data[playListOff:]
	playListLen := binary.BigEndian.Uint32(pl[0:4])
	if int(playListOff)+4+int(playListLen) > len(data) {
		return nil, fmt.Errorf("mpls: PlayList length %d exceeds file size", playListLen)
	}
	numPlayItems := binary.BigEndian.Uint16(pl[6:8])

	items := make([]MPLSPlayItem, 0, numPlayItems)
	// PlayItems start after the 10-byte PlayList header.
	cursor := 10
	plBody := pl[:4+int(playListLen)]
	for i := range int(numPlayItems) {
		if cursor+2 > len(plBody) {
			return nil, fmt.Errorf("mpls: PlayItem %d header out of range", i)
		}
		// PlayItem length excludes the 2-byte length field itself.
		itemLen := int(binary.BigEndian.Uint16(plBody[cursor : cursor+2]))
		itemStart := cursor + 2
		itemEnd := itemStart + itemLen
		if itemEnd > len(plBody) {
			return nil, fmt.Errorf("mpls: PlayItem %d length %d overruns PlayList", i, itemLen)
		}
		// Fixed PlayItem layout we care about:
		//   +0  5  clip_information_file_name (e.g. "00001")
		//   +5  4  clip_codec_identifier ("M2TS")
		//   +9  2  flags incl. is_multi_angle / connection_condition
		//   +11 1  ref_to_STC_id
		//   +12 4  IN_time   (45 kHz)
		//   +16 4  OUT_time  (45 kHz)
		if itemLen < 20 {
			return nil, fmt.Errorf("mpls: PlayItem %d too short (len=%d)", i, itemLen)
		}
		body := plBody[itemStart:itemEnd]
		items = append(items, MPLSPlayItem{
			ClipName: string(body[0:5]),
			InTime:   binary.BigEndian.Uint32(body[12:16]),
			OutTime:  binary.BigEndian.Uint32(body[16:20]),
		})
		cursor = itemEnd
	}

	return &MPLSPlayList{Version: version, PlayItems: items}, nil
}
