package nzbfilesystem

import "github.com/javi11/altmount/internal/usenet"

// benchReplayStore is the segment store the replay scenario opens files
// with. Nil until a shared cache tier exists.
func benchReplayStore() usenet.SegmentStore { return nil }
