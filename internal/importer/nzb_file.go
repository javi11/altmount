package importer

import (
	"compress/gzip"
	"io"
	"os"
	"strings"
)

const nzbGzExtension = ".nzb.gz"

// openNzbFile opens an NZB file for reading, transparently decompressing .nzb.gz files.
// The caller is responsible for closing the returned ReadCloser.
func openNzbFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	if !strings.HasSuffix(strings.ToLower(path), nzbGzExtension) {
		return f, nil
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	return &gzipReadCloser{file: f, reader: gr}, nil
}

// gzipReadCloser wraps a gzip.Reader and its underlying file so both are closed together.
type gzipReadCloser struct {
	file   *os.File
	reader *gzip.Reader
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.reader.Read(p)
}

func (g *gzipReadCloser) Close() error {
	rerr := g.reader.Close()
	ferr := g.file.Close()
	if rerr != nil {
		return rerr
	}
	return ferr
}

// compressNzbToGz reads the NZB at srcPath and writes a gzip-compressed copy to dstPath.
func compressNzbToGz(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	gw, err := gzip.NewWriterLevel(dst, gzip.BestSpeed)
	if err != nil {
		_ = os.Remove(dstPath)
		return err
	}

	if _, err := io.Copy(gw, src); err != nil {
		_ = gw.Close()
		_ = os.Remove(dstPath)
		return err
	}

	if err := gw.Close(); err != nil {
		_ = os.Remove(dstPath)
		return err
	}

	return dst.Close()
}
