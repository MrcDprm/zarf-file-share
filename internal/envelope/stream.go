package envelope

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
)

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// chunkNonce is the 11-byte chunk counter followed by 0x01 for the last chunk, else 0x00 (STREAM).
func chunkNonce(i uint64, last bool) []byte {
	n := make([]byte, 12)
	for j := 0; j < 8; j++ {
		n[10-j] = byte(i >> (8 * j)) // the top three counter bytes stay zero
	}
	if last {
		n[11] = 1
	}
	return n
}

// Params are what a writer needs besides the input key. Salt is drawn at random when left zero
// (tests and test vectors fix it).
type Params struct {
	Mode      Mode
	ChunkLog2 uint8 // 0 means DefaultChunkLog2
	Salt      [saltSize]byte
	Argon     ArgonParams // mode 0x02
	EphPub    [32]byte    // mode 0x03
}

// Writer encrypts content as it is written. Close must be called: it seals the last chunk,
// and without it the envelope reads as cut short.
type Writer struct {
	w      io.Writer
	aead   cipher.AEAD
	aad    []byte
	buf    []byte
	size   int
	n      uint64
	closed bool
}

// NewWriter writes the header and the encrypted metadata to w, then returns a Writer for the content.
func NewWriter(w io.Writer, p Params, ikm []byte, meta Metadata) (*Writer, error) {
	h := &Header{Mode: p.Mode, ChunkLog2: p.ChunkLog2, Salt: p.Salt, Argon: p.Argon, EphPub: p.EphPub}
	if h.ChunkLog2 == 0 {
		h.ChunkLog2 = DefaultChunkLog2
	}
	if h.Salt == [saltSize]byte{} {
		if _, err := rand.Read(h.Salt[:]); err != nil {
			return nil, err
		}
	}
	plainMeta, err := encodeMetadata(meta)
	if err != nil {
		return nil, err
	}
	h.MetaLen = uint32(len(plainMeta) + tagSize)
	if err := h.check(); err != nil {
		return nil, err
	}

	k, err := deriveKeys(h.Salt[:], ikm)
	if err != nil {
		return nil, err
	}
	copy(h.Commitment[:], commitment(k.commit, h))
	aad := h.hash()

	metaAEAD, err := newGCM(k.meta)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(h.marshal(true)); err != nil {
		return nil, err
	}
	// The metadata key is used exactly once, so a zero nonce is safe.
	if _, err := w.Write(metaAEAD.Seal(nil, make([]byte, 12), plainMeta, aad)); err != nil {
		return nil, err
	}
	contentAEAD, err := newGCM(k.content)
	if err != nil {
		return nil, err
	}
	return &Writer{w: w, aead: contentAEAD, aad: aad, size: h.chunkSize(), buf: make([]byte, 0, h.chunkSize())}, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("envelope: write after close")
	}
	written := 0
	for len(p) > 0 {
		// A full buffer is sealed only once more data arrives: until then it may be the last chunk.
		if len(w.buf) == w.size {
			if err := w.seal(false); err != nil {
				return written, err
			}
		}
		n := copy(w.buf[len(w.buf):w.size], p)
		w.buf = w.buf[:len(w.buf)+n]
		p = p[n:]
		written += n
	}
	return written, nil
}

func (w *Writer) seal(last bool) error {
	if _, err := w.w.Write(w.aead.Seal(nil, chunkNonce(w.n, last), w.buf, w.aad)); err != nil {
		return err
	}
	w.n++
	w.buf = w.buf[:0]
	return nil
}

// Close seals the last chunk (empty if the content was empty).
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.seal(true)
}

// KeyFunc turns a header into the input key: for a password it needs the header's
// Argon2id settings, for request mode the sender's one-time public key.
type KeyFunc func(h *Header) ([]byte, error)

// Reader decrypts content as it is read. Each chunk is checked before any of it is
// returned; io.EOF comes only after the last chunk verified.
type Reader struct {
	Header   *Header
	Metadata Metadata

	r     *bufio.Reader
	aead  cipher.AEAD
	aad   []byte
	size  int
	n     uint64
	plain []byte
	done  bool
}

// Open reads the header, checks the key commitment before touching any ciphertext,
// and decrypts the metadata.
func Open(r io.Reader, key KeyFunc) (*Reader, error) {
	h, err := ReadHeader(r)
	if err != nil {
		return nil, err
	}
	ikm, err := key(h)
	if err != nil {
		return nil, err
	}
	k, err := deriveKeys(h.Salt[:], ikm)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(commitment(k.commit, h), h.Commitment[:]) != 1 {
		return nil, ErrWrongKey
	}
	aad := h.hash()

	metaCipher := make([]byte, h.MetaLen)
	if _, err := io.ReadFull(r, metaCipher); err != nil {
		return nil, ErrTruncated
	}
	metaAEAD, err := newGCM(k.meta)
	if err != nil {
		return nil, err
	}
	plainMeta, err := metaAEAD.Open(nil, make([]byte, 12), metaCipher, aad)
	if err != nil {
		return nil, ErrAuth
	}
	meta, err := decodeMetadata(plainMeta)
	if err != nil {
		return nil, err
	}
	contentAEAD, err := newGCM(k.content)
	if err != nil {
		return nil, err
	}
	return &Reader{Header: h, Metadata: meta, r: bufio.NewReaderSize(r, h.chunkSize()+tagSize+1), aead: contentAEAD, aad: aad, size: h.chunkSize()}, nil
}

func (r *Reader) Read(p []byte) (int, error) {
	for len(r.plain) == 0 {
		if r.done {
			return 0, io.EOF
		}
		if err := r.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.plain)
	r.plain = r.plain[n:]
	return n, nil
}

// next opens one chunk. A chunk is the last one if the data ends right after it.
func (r *Reader) next() error {
	ct := make([]byte, r.size+tagSize)
	n, err := io.ReadFull(r.r, ct)
	switch {
	case err == io.EOF:
		return ErrTruncated // no chunk at all where one was due
	case err == io.ErrUnexpectedEOF:
		ct = ct[:n] // a short chunk: it can only be the last one
	case err != nil:
		return err
	default:
		if _, err := r.r.Peek(1); err == nil {
			return r.open(ct, false)
		} else if err != io.EOF {
			return err
		}
	}
	if len(ct) < tagSize {
		return ErrTruncated
	}
	if err := r.open(ct, true); err != nil {
		// Opening as a middle chunk would mean the stream was cut right after it.
		if _, middleErr := r.aead.Open(nil, chunkNonce(r.n, false), ct, r.aad); middleErr == nil {
			return ErrTruncated
		}
		return err
	}
	r.done = true
	return nil
}

func (r *Reader) open(ct []byte, last bool) error {
	plain, err := r.aead.Open(nil, chunkNonce(r.n, last), ct, r.aad)
	if err != nil {
		if !last {
			// A full chunk followed by more data that is really the last one: bytes were added.
			if _, lastErr := r.aead.Open(nil, chunkNonce(r.n, true), ct, r.aad); lastErr == nil {
				return ErrTrailing
			}
		}
		return ErrAuth
	}
	r.n++
	r.plain = plain
	return nil
}

// String is for error messages and logs: it never includes keys.
func (h *Header) String() string {
	return fmt.Sprintf("zarf v1 mode=%#x chunk=%d meta=%d", byte(h.Mode), h.chunkSize(), h.MetaLen)
}
