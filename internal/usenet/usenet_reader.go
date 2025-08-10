package usenet

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool"
	"github.com/sourcegraph/conc/pool"
)

const defaultDownloadWorkers = 15

var (
	_ io.ReadCloser = &usenetReader{}
)

type usenetReader struct {
	log                *slog.Logger
	wg                 sync.WaitGroup
	cancel             context.CancelFunc
	rg                 segmentRange
	maxDownloadWorkers int
	init               chan any
	initDownload       sync.Once
}

func NewUsenetReader(
	ctx context.Context,
	cp nntppool.UsenetConnectionPool,
	rg segmentRange,
	maxDownloadWorkers int,
) (io.ReadCloser, error) {
	log := slog.Default()
	ctx, cancel := context.WithCancel(ctx)
	ur := &usenetReader{
		log:                log,
		cancel:             cancel,
		rg:                 rg,
		init:               make(chan any, 1),
		maxDownloadWorkers: maxDownloadWorkers,
	}

	// Will start go routine pool with max download workers that will fill the cache

	ur.wg.Add(1)
	go func() {
		defer ur.wg.Done()
		ur.downloadManager(ctx, cp)
	}()

	return ur, nil
}

func (b *usenetReader) Close() error {
	b.cancel()
	close(b.init)

	go func() {
		b.wg.Wait()
		_ = b.rg.Clear()
		b.rg = segmentRange{}
	}()

	return nil
}

// Read reads len(p) byte from the Buffer starting at the current offset.
// It returns the number of bytes read and an error if any.
// Returns io.EOF error if pointer is at the end of the Buffer.
func (b *usenetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.initDownload.Do(func() {
		b.init <- struct{}{}
	})

	s, err := b.rg.Get()
	if err != nil {
		return 0, io.EOF
	}

	n := 0
	for n < len(p) {
		nn, err := s.GetReader().Read(p[n:])
		n += nn
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Segment is fully read, remove it from the cache
				s, err = b.rg.Next()
				if err != nil {
					return n, io.EOF
				}
			} else {
				return n, err
			}
		}
	}

	return n, nil
}

func (b *usenetReader) downloadManager(
	ctx context.Context,
	cp nntppool.UsenetConnectionPool,
) {
	select {
	case _, ok := <-b.init:
		if !ok {
			return
		}

		slog.DebugContext(ctx, "Download worker started")

		downloadWorkers := b.maxDownloadWorkers
		if downloadWorkers == 0 {
			downloadWorkers = defaultDownloadWorkers
		}

		pool := pool.New().
			WithMaxGoroutines(downloadWorkers).
			WithContext(ctx)

		for _, s := range b.rg.segments {
			if ctx.Err() != nil {
				break
			}

			pool.Go(func(c context.Context) error {
				w := s.writer

				// Set the item ready to read
				ctx = slogutil.With(ctx, "segment_id", s.Id)
				_, err := cp.Body(ctx, s.Id, s.writer, s.groups)
				if !errors.Is(err, context.Canceled) {
					cErr := w.CloseWithError(err)
					if cErr != nil {
						b.log.ErrorContext(ctx, "Error closing segment buffer:", "error", cErr)
					}

					w = nil
					s = nil

					if err != nil && !errors.Is(err, context.Canceled) {
						b.log.ErrorContext(ctx, "Error downloading segment:", "error", err)
						return err
					}

					return nil
				}

				err = w.Close()
				if err != nil {
					b.log.ErrorContext(ctx, "Error closing segment writer:", "error", err)
				}

				w = nil
				s = nil

				return nil
			})
		}

		if err := pool.Wait(); err != nil {
			b.log.ErrorContext(ctx, "Error downloading segments:", "error", err)

			return
		}

		slog.DebugContext(ctx, "Download worker finished")

	case <-ctx.Done():
		return
	}
}
