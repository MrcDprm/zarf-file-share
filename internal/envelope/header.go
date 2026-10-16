// Package envelope reads and writes Zarf envelopes: a header, an encrypted metadata block and
// the encrypted content in chunks. The byte layout is defined in docs/PROTOCOL.md, section 2.
package envelope

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Mode says how the envelope's key was made (PROTOCOL.md sections 3-5).
type Mode byte

const (
	ModeLink         Mode = 0x01
	ModeLinkPassword Mode = 0x02
	ModeRequest      Mode = 0x03
	ModeLive         Mode = 0x04
)

const (
	magic   = "ZARF"
	version = 0x01

	DefaultChunkLog2 = 16 // 64 KiB chunks
	minChunkLog2     = 12
	maxChunkLog2     = 20

	saltSize       = 32
	commitmentSize = 32
	tagSize        = 16 // AES-GCM tag
	maxMetaCipher  = 64 * 1024

	// Limits on the password hash parameters a header may ask for, so a hostile
	// header cannot make a reader spend gigabytes of memory or minutes of time.
	MaxArgonMemoryKiB = 1024 * 1024 // 1024 MB
	MaxArgonTime      = 10
	MaxArgonThreads   = 4
)

var (
	ErrNotEnvelope = errors.New("envelope: not a Zarf envelope")
	ErrVersion     = errors.New("envelope: unsupported version")
	ErrHeader      = errors.New("envelope: invalid header")
	ErrWrongKey    = errors.New("envelope: wrong key or password")
	ErrAuth        = errors.New("envelope: content was changed or damaged")
	ErrTruncated   = errors.New("envelope: content is cut short")
	ErrTrailing    = errors.New("envelope: unexpected data after the end")
)

// ArgonParams are the Argon2id settings of a password-protected link (mode 0x02).
type ArgonParams struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	Salt      [16]byte
}

// DefaultArgon is the first OWASP recommendation: 46 MB, one pass, one thread.
func DefaultArgon() ArgonParams { return ArgonParams{MemoryKiB: 47104, Time: 1, Threads: 1} }

func (a ArgonParams) check() error {
	if a.MemoryKiB < 8 || a.MemoryKiB > MaxArgonMemoryKiB || a.Time < 1 || a.Time > MaxArgonTime || a.Threads < 1 || a.Threads > MaxArgonThreads {
		return fmt.Errorf("%w: password hash settings out of range", ErrHeader)
	}
	return nil
}

// Header is the plaintext start of an envelope. Every byte of it is authenticated.
type Header struct {
	Mode       Mode
	ChunkLog2  uint8
	Salt       [saltSize]byte
	Argon      ArgonParams // mode 0x02 only
	EphPub     [32]byte    // mode 0x03 only: the sender's one-time X25519 public key
	MetaLen    uint32
	Commitment [commitmentSize]byte
}

func (h *Header) chunkSize() int { return 1 << h.ChunkLog2 }

// marshal returns the header bytes; with withCommitment false, it stops right before the commitment.
func (h *Header) marshal(withCommitment bool) []byte {
	var b bytes.Buffer
	b.WriteString(magic)
	b.WriteByte(version)
	b.WriteByte(byte(h.Mode))
	b.WriteByte(h.ChunkLog2)
	b.Write(h.Salt[:])
	switch h.Mode {
	case ModeLinkPassword:
		_ = binary.Write(&b, binary.BigEndian, h.Argon.MemoryKiB)
		_ = binary.Write(&b, binary.BigEndian, h.Argon.Time)
		b.WriteByte(h.Argon.Threads)
		b.Write(h.Argon.Salt[:])
	case ModeRequest:
		b.Write(h.EphPub[:])
	}
	_ = binary.Write(&b, binary.BigEndian, h.MetaLen)
	if withCommitment {
		b.Write(h.Commitment[:])
	}
	return b.Bytes()
}

// hash is the associated data bound to every ciphertext of the envelope.
func (h *Header) hash() []byte {
	sum := sha256.Sum256(h.marshal(true))
	return sum[:]
}

func (h *Header) check() error {
	switch h.Mode {
	case ModeLink, ModeLive, ModeRequest:
	case ModeLinkPassword:
		if err := h.Argon.check(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown mode %#x", ErrHeader, byte(h.Mode))
	}
	if h.ChunkLog2 < minChunkLog2 || h.ChunkLog2 > maxChunkLog2 {
		return fmt.Errorf("%w: chunk size out of range", ErrHeader)
	}
	if h.MetaLen < tagSize || h.MetaLen > maxMetaCipher {
		return fmt.Errorf("%w: metadata size out of range", ErrHeader)
	}
	return nil
}

// ReadHeader reads and checks the header at the start of r.
func ReadHeader(r io.Reader) (*Header, error) {
	start := make([]byte, 4+1+1+1+saltSize)
	if _, err := io.ReadFull(r, start); err != nil {
		return nil, ErrNotEnvelope
	}
	if string(start[:4]) != magic {
		return nil, ErrNotEnvelope
	}
	if start[4] != version {
		return nil, ErrVersion
	}
	h := &Header{Mode: Mode(start[5]), ChunkLog2: start[6]}
	copy(h.Salt[:], start[7:])

	read := func(n int) ([]byte, error) {
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, fmt.Errorf("%w: cut short", ErrHeader)
		}
		return b, nil
	}
	switch h.Mode {
	case ModeLinkPassword:
		b, err := read(4 + 4 + 1 + 16)
		if err != nil {
			return nil, err
		}
		h.Argon.MemoryKiB = binary.BigEndian.Uint32(b[0:4])
		h.Argon.Time = binary.BigEndian.Uint32(b[4:8])
		h.Argon.Threads = b[8]
		copy(h.Argon.Salt[:], b[9:])
	case ModeRequest:
		b, err := read(32)
		if err != nil {
			return nil, err
		}
		copy(h.EphPub[:], b)
	}
	b, err := read(4 + commitmentSize)
	if err != nil {
		return nil, err
	}
	h.MetaLen = binary.BigEndian.Uint32(b[:4])
	copy(h.Commitment[:], b[4:])
	if err := h.check(); err != nil {
		return nil, err
	}
	return h, nil
}
