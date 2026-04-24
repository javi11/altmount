// Package nzbfile centralizes on-disk NZB I/O, hiding gzip compression so
// callers always deal with plain XML regardless of how the file is stored.
package nzbfile

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// GzExtension is the persistent storage extension for gzip-compressed NZB files.
const GzExtension = ".nzb.gz"

// IsGzipped reports whether path points to a gzip-compressed NZB by suffix.
func IsGzipped(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), GzExtension)
}

// PlainFilename returns the base filename with any .gz suffix stripped, so
// downstream consumers (download clients, external SABnzbd) see a plain .nzb.
// The case of the original name is preserved.
func PlainFilename(path string) string {
	name := filepath.Base(path)
	if strings.HasSuffix(strings.ToLower(name), GzExtension) {
		return name[:len(name)-len(".gz")]
	}
	return name
}

// Open opens an NZB file for reading, transparently decompressing .nzb.gz
// files so callers always receive plain XML. The caller must Close the result.
func Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	if !IsGzipped(path) {
		return f, nil
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &gzipReadCloser{file: f, reader: gr}, nil
}

// Compress reads the NZB at srcPath and writes a gzip-compressed copy to dstPath.
func Compress(srcPath, dstPath string) error {
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

	gw, err := gzip.NewWriterLevel(dst, gzip.BestCompression)
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

// ResolveOnDisk returns an existing path for an NZB, tolerating drift between
// the stored extension and what's actually on disk (e.g. legacy rows pointing
// at .nzb while the file was later gzipped, or vice versa). Returns the input
// path unchanged if it exists, the .gz-toggled variant if that exists, or
// (path, os.ErrNotExist) otherwise.
func ResolveOnDisk(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return path, err
	}

	var alt string
	if IsGzipped(path) {
		alt = strings.TrimSuffix(path, filepath.Ext(path))
	} else {
		alt = path + ".gz"
	}
	if _, err := os.Stat(alt); err == nil {
		return alt, nil
	}
	return path, os.ErrNotExist
}

type gzipReadCloser struct {
	file   *os.File
	reader *gzip.Reader
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.reader.Read(p) }

func (g *gzipReadCloser) Close() error {
	rerr := g.reader.Close()
	ferr := g.file.Close()
	if rerr != nil {
		return rerr
	}
	return ferr
}
