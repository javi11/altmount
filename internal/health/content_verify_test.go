package health

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/usenet"
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
		return 0, os.ErrClosed
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *fakeContentFile) Close() error { return nil }

type fakeContentOpener struct {
	data []byte
	err  error
}

func (o *fakeContentOpener) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error) {
	if o.err != nil {
		return nil, o.err
	}
	return &fakeContentFile{data: o.data}, nil
}

func newTestHealthCheckerWithVerifyContentEnabled(opener *fakeContentOpener) *HealthChecker {
	enabled := true
	cfg := &config.Config{Health: config.HealthConfig{VerifyContent: &enabled}}
	return &HealthChecker{
		configGetter:    func() *config.Config { return cfg },
		contentVerifyFS: opener,
	}
}

func allPresentResult() usenet.ValidationResult {
	return usenet.ValidationResult{TotalChecked: 1, MissingCount: 0}
}

func TestJudgeValidation_ContentInvalid(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(&fakeContentOpener{data: make([]byte, 512)}) // no recognized signature
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusPending}

	event := hc.judgeValidation(context.Background(), prep, allPresentResult(), nil)

	if event.Status != database.HealthStatusCorrupted {
		t.Errorf("got status %v, want Corrupted", event.Status)
	}
	if event.Details == nil || !strings.Contains(*event.Details, "content_invalid") {
		t.Errorf("expected content_invalid in details, got %v", event.Details)
	}
}

func TestJudgeValidation_ContentProbeErrorLeavesStatusUnchanged(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(&fakeContentOpener{err: errors.New("connection reset")})
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusPending}

	event := hc.judgeValidation(context.Background(), prep, allPresentResult(), nil)

	if event.Type != EventTypeFileHealthy {
		t.Errorf("got event type %v, want EventTypeFileHealthy (transient probe error must not corrupt)", event.Type)
	}
}

func TestJudgeValidation_SkipsProbeWhenNotPending(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(&fakeContentOpener{data: make([]byte, 512)}) // would fail if probed
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusDegraded}

	event := hc.judgeValidation(context.Background(), prep, allPresentResult(), nil)

	if event.Type != EventTypeFileHealthy {
		t.Errorf("expected a Degraded-status recheck to skip content probing entirely, got %v", event.Type)
	}
}

func TestJudgeValidation_ValidSignaturePasses(t *testing.T) {
	data := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x82, 0x88}, []byte("matroska")...)
	data = append(data, make([]byte, 512-len(data))...)
	hc := newTestHealthCheckerWithVerifyContentEnabled(&fakeContentOpener{data: data})
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusPending}

	event := hc.judgeValidation(context.Background(), prep, allPresentResult(), nil)

	if event.Type != EventTypeFileHealthy {
		t.Errorf("expected a valid signature to pass, got %v (%v)", event.Type, event.Error)
	}
}

func TestJudgeValidation_OverrideForcesProbeOnNonPending(t *testing.T) {
	force := true
	hc := newTestHealthCheckerWithVerifyContentEnabled(&fakeContentOpener{data: make([]byte, 512)})
	prep := preparedCheck{filePath: "/movie.mkv", currentStatus: database.HealthStatusDegraded, verifyContentOverride: &force}

	event := hc.judgeValidation(context.Background(), prep, allPresentResult(), nil)

	if event.Status != database.HealthStatusCorrupted {
		t.Errorf("expected override to force the probe and mark corrupted, got %v", event.Type)
	}
}

func TestJudgeValidation_SkipsNonMediaFiles(t *testing.T) {
	hc := newTestHealthCheckerWithVerifyContentEnabled(&fakeContentOpener{data: make([]byte, 512)}) // would fail if probed
	prep := preparedCheck{filePath: "/release.nfo", currentStatus: database.HealthStatusPending}

	event := hc.judgeValidation(context.Background(), prep, allPresentResult(), nil)

	if event.Type != EventTypeFileHealthy {
		t.Errorf("expected non-media files to skip content verification, got %v", event.Type)
	}
}
