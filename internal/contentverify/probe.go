package contentverify

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool/v4"
	"github.com/spf13/afero"
)

// Result classifies the outcome of a content-signature probe.
type Result int

const (
	// ContentValid means a recognized video/audio container signature was found.
	ContentValid Result = iota
	// ContentInvalid means bytes were read but no supported container
	// signature was found — a definitive failure.
	ContentInvalid
	// ContentSegmentMissing means the head article required to read the
	// file's first bytes is confirmed missing — a definitive failure.
	ContentSegmentMissing
	// ContentProbeError means the probe could not complete due to a
	// transient provider, timeout, or connection error — never definitive.
	ContentProbeError
)

// ProbeResult is the outcome of Probe.
type ProbeResult struct {
	Result Result
	Err    error
}

// probeReadSize is the number of bytes read from the start of the file —
// enough to identify every supported container signature.
const probeReadSize = 512

// Opener abstracts *nzbfilesystem.NzbFilesystem.OpenFile for testability.
// nzbfilesystem.NzbFilesystem satisfies this interface structurally.
type Opener interface {
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error)
}

// Probe opens path through opener, reads up to the first 512 bytes with
// read-ahead capped to one backing article, and classifies the result.
// Definitive failures (ContentInvalid, ContentSegmentMissing) mean the
// content is confirmed bad; ContentProbeError means the check itself
// failed and must not be treated as a content failure.
func Probe(ctx context.Context, opener Opener, path string, timeout time.Duration) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = context.WithValue(ctx, utils.MaxPrefetchKey, 1)

	f, err := opener.OpenFile(ctx, path, os.O_RDONLY, 0)
	if err != nil {
		return classifyError(err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on a read-only probe

	buf := make([]byte, probeReadSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return classifyError(err)
	}

	if fileinfo.IsRecognizedMediaContainer(buf[:n]) {
		return ProbeResult{Result: ContentValid}
	}
	return ProbeResult{Result: ContentInvalid, Err: errors.New("no recognized media container signature")}
}

// classifyError maps an Open/Read error to a definitive missing-segment
// result or a transient probe error. A confirmed missing article
// (nntppool.ErrArticleNotFound) is the only definitive case here — anything
// else, including timeouts and unrecognized errors, is conservatively
// treated as transient so it never blocks an import or corrupts a file on
// a flaky connection.
func classifyError(err error) ProbeResult {
	if errors.Is(err, nntppool.ErrArticleNotFound) {
		return ProbeResult{Result: ContentSegmentMissing, Err: err}
	}
	return ProbeResult{Result: ContentProbeError, Err: err}
}
