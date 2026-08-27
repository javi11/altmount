package contentverify

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/spf13/afero"
)

// fakeFile implements afero.File with only Read/Close exercised by Probe.
type fakeFile struct {
	afero.File // embed to satisfy the interface; only Read/Close are called
	data       []byte
	readErr    error
	pos        int
}

func (f *fakeFile) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *fakeFile) Close() error { return nil }

type fakeOpener struct {
	file *fakeFile
	err  error
}

func (o *fakeOpener) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error) {
	if o.err != nil {
		return nil, o.err
	}
	return o.file, nil
}

func TestProbe_ValidSignature(t *testing.T) {
	data := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x82, 0x88}, []byte("matroska")...)
	data = append(data, make([]byte, 512-len(data))...)
	res := Probe(context.Background(), &fakeOpener{file: &fakeFile{data: data}}, "movie.mkv", time.Second)
	if res.Result != ContentValid {
		t.Errorf("got %v, want ContentValid", res.Result)
	}
}

func TestProbe_InvalidSignature(t *testing.T) {
	data := make([]byte, 512) // all zero bytes, no known signature
	res := Probe(context.Background(), &fakeOpener{file: &fakeFile{data: data}}, "movie.mkv", time.Second)
	if res.Result != ContentInvalid {
		t.Errorf("got %v, want ContentInvalid", res.Result)
	}
}

func TestProbe_ShortFileTolerated(t *testing.T) {
	// Shorter than 512 bytes but a valid, complete signature.
	data := []byte("\x66\x4C\x61\x43\x00\x00\x00\x22")
	res := Probe(context.Background(), &fakeOpener{file: &fakeFile{data: data}}, "song.flac", time.Second)
	if res.Result != ContentValid {
		t.Errorf("got %v, want ContentValid for short-but-valid file", res.Result)
	}
}

func TestProbe_MissingSegment(t *testing.T) {
	opener := &fakeOpener{file: &fakeFile{readErr: nntppool.ErrArticleNotFound}}
	res := Probe(context.Background(), opener, "movie.mkv", time.Second)
	if res.Result != ContentSegmentMissing {
		t.Errorf("got %v, want ContentSegmentMissing", res.Result)
	}
	if !errors.Is(res.Err, nntppool.ErrArticleNotFound) {
		t.Errorf("expected wrapped ErrArticleNotFound, got %v", res.Err)
	}
}

func TestProbe_TransientError(t *testing.T) {
	opener := &fakeOpener{file: &fakeFile{readErr: errors.New("connection reset by peer")}}
	res := Probe(context.Background(), opener, "movie.mkv", time.Second)
	if res.Result != ContentProbeError {
		t.Errorf("got %v, want ContentProbeError", res.Result)
	}
}

func TestProbe_OpenFileError(t *testing.T) {
	opener := &fakeOpener{err: errors.New("boom")}
	res := Probe(context.Background(), opener, "movie.mkv", time.Second)
	if res.Result != ContentProbeError {
		t.Errorf("got %v, want ContentProbeError", res.Result)
	}
}

func TestProbe_Timeout(t *testing.T) {
	opener := &fakeOpener{file: &fakeFile{readErr: context.DeadlineExceeded}}
	res := Probe(context.Background(), opener, "movie.mkv", time.Millisecond)
	if res.Result != ContentProbeError {
		t.Errorf("got %v, want ContentProbeError", res.Result)
	}
}
