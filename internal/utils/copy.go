package utils

import (
	"context"
	"io"
)

func CopyWithCtx(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 1024)
	defer func() {
		buf = nil
	}()

	var totalBytes int64

	for {
		select {
		case <-ctx.Done():
			return totalBytes, ctx.Err()
		default:
			n, err := src.Read(buf)
			if err != nil {
				return totalBytes, err
			}

			n, err = dst.Write(buf[:n])
			if err != nil {
				return totalBytes, err
			}

			totalBytes += int64(n)
		}
	}
}
