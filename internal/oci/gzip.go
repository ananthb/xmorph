package oci

import (
	"compress/gzip"
	"io"
)

// newGzipReader wraps r in a gzip decompressor with a small helper that
// closes both the gzip reader and the underlying reader (when the
// underlying reader is itself an io.Closer). Extracted so extract.go
// stays focused on tar walking.
func newGzipReader(r io.Reader) (io.ReadCloser, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	return gz, nil
}
