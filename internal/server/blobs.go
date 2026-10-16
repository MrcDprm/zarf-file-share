package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// blobs keeps envelopes on disk: parts while an upload is under way, then one file.
// Ids are checked by the caller (16 random bytes in base64url), so they are safe as names.
//
//	<root>/<id>/part-<n>   during the upload
//	<root>/<id>/envelope   once complete
type blobs struct{ root string }

func (b *blobs) dir(id string) string { return filepath.Join(b.root, id) }

// writePart stores part n from r, which must deliver exactly want bytes. It writes to a
// temporary file first, so a broken connection never leaves a half part in place.
func (b *blobs) writePart(id string, n, want int64, r io.Reader) error {
	if err := os.MkdirAll(b.dir(id), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(b.dir(id), "incoming-*")
	if err != nil {
		return err
	}
	got, copyErr := io.Copy(tmp, io.LimitReader(r, want+1))
	closeErr := tmp.Close()
	if copyErr == nil && closeErr == nil && got != want {
		copyErr = errWrongPartSize
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), b.partPath(id, n))
}

var errWrongPartSize = errors.New("part has the wrong size")

func (b *blobs) partPath(id string, n int64) string {
	return filepath.Join(b.dir(id), fmt.Sprintf("part-%d", n))
}

func (b *blobs) envelopePath(id string) string { return filepath.Join(b.dir(id), "envelope") }

// assemble joins the parts in order into the envelope and removes them.
func (b *blobs) assemble(id string, parts int64) error {
	tmp, err := os.CreateTemp(b.dir(id), "assembling-*")
	if err != nil {
		return err
	}
	for n := int64(0); n < parts; n++ {
		if err := appendFile(tmp, b.partPath(id, n)); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), b.envelopePath(id)); err != nil {
		return err
	}
	for n := int64(0); n < parts; n++ {
		os.Remove(b.partPath(id, n))
	}
	return nil
}

func appendFile(dst io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(dst, f)
	return err
}

func (b *blobs) remove(id string) error { return os.RemoveAll(b.dir(id)) }
