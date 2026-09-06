package importer

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/spf13/afero"
)

// fakeContentFile implements afero.File with only Read/Close exercised.
type fakeContentFile struct {
	afero.File
	data    []byte
	readErr error
	pos     int
}

func (f *fakeContentFile) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	if f.pos >= len(f.data) {
		return 0, os.ErrClosed // any non-EOF-like signal; unused in these tests
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	if f.pos >= len(f.data) {
		return n, nil
	}
	return n, nil
}

func (f *fakeContentFile) Close() error { return nil }

type fakeContentOpener struct {
	data []byte
}

func (o *fakeContentOpener) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error) {
	return &fakeContentFile{data: o.data}, nil
}

func TestVerifyWrittenContent_DisabledByDefault(t *testing.T) {
	s := &Service{
		configGetter: func() *config.Config { return &config.Config{} },
	}
	err := s.verifyWrittenContent(context.Background(), []string{"movie.mkv"})
	if err != nil {
		t.Fatalf("expected no error when verify_content is disabled, got %v", err)
	}
}

func TestVerifyWrittenContent_InvalidSignatureFailsImport(t *testing.T) {
	enabled := true
	s := &Service{
		configGetter:    func() *config.Config { return &config.Config{Import: config.ImportConfig{VerifyContent: &enabled}} },
		contentVerifyFS: &fakeContentOpener{data: make([]byte, 512)}, // no recognized signature
	}
	err := s.verifyWrittenContent(context.Background(), []string{"movie.mkv"})
	if err == nil {
		t.Fatal("expected an error when content verification fails")
	}
}

func TestVerifyWrittenContent_ValidSignaturePasses(t *testing.T) {
	enabled := true
	data := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x82, 0x88}, []byte("matroska")...)
	data = append(data, make([]byte, 512-len(data))...)
	s := &Service{
		configGetter:    func() *config.Config { return &config.Config{Import: config.ImportConfig{VerifyContent: &enabled}} },
		contentVerifyFS: &fakeContentOpener{data: data},
	}
	err := s.verifyWrittenContent(context.Background(), []string{"movie.mkv"})
	if err != nil {
		t.Fatalf("expected no error for a valid signature, got %v", err)
	}
}

func TestVerifyWrittenContent_SkipsNonMediaFiles(t *testing.T) {
	enabled := true
	s := &Service{
		configGetter:    func() *config.Config { return &config.Config{Import: config.ImportConfig{VerifyContent: &enabled}} },
		contentVerifyFS: &fakeContentOpener{data: make([]byte, 512)}, // would fail if probed
	}
	err := s.verifyWrittenContent(context.Background(), []string{"archive.par2", "release.nfo"})
	if err != nil {
		t.Fatalf("expected non-media files to be skipped, got %v", err)
	}
}

func TestVerifyWrittenContent_TransientErrorFailsAsRetryable(t *testing.T) {
	enabled := true
	s := &Service{
		configGetter: func() *config.Config { return &config.Config{Import: config.ImportConfig{VerifyContent: &enabled}} },
		contentVerifyFS: &fakeContentOpener{data: nil}, // Read returns os.ErrClosed immediately (transient, not a recognized-missing sentinel)
	}
	_ = time.Second // keep import used if timeout const changes
	err := s.verifyWrittenContent(context.Background(), []string{"movie.mkv"})
	if err == nil {
		t.Fatal("expected a transient probe error to still return a non-nil error (routed through the existing retry path)")
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("expected wrapped os.ErrClosed, got %v", err)
	}
}
