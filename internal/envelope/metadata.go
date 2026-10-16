package envelope

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// File describes one file of the content. Name is data from the sender, never a path:
// see package safename before writing anything to disk.
type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type,omitempty"`
}

// Metadata is encrypted with its own key and padded, so even the length of names stays hidden.
type Metadata struct {
	V     int    `json:"v"`
	Files []File `json:"files"`
	Zip   bool   `json:"zip"` // several files, packed into one uncompressed ZIP archive
}

const metaBlock = 1024

// pad appends 0x80 and zeros up to a multiple of 1024 bytes (ISO/IEC 7816-4 padding).
func pad(b []byte) []byte {
	out := append(append([]byte(nil), b...), 0x80)
	for len(out)%metaBlock != 0 {
		out = append(out, 0)
	}
	return out
}

func unpad(b []byte) ([]byte, error) {
	i := bytes.LastIndexFunc(b, func(r rune) bool { return r != 0 })
	if i < 0 || b[i] != 0x80 {
		return nil, fmt.Errorf("%w: bad metadata padding", ErrAuth)
	}
	return b[:i], nil
}

// encodeMetadata writes the canonical form (PROTOCOL.md 2.5): fields in a fixed order, no spaces,
// and no HTML escaping, so the Go and TypeScript writers produce the same bytes.
func encodeMetadata(m Metadata) ([]byte, error) {
	m.V = 1
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return pad(bytes.TrimSuffix(b.Bytes(), []byte("\n"))), nil
}

func decodeMetadata(padded []byte) (Metadata, error) {
	raw, err := unpad(padded)
	if err != nil {
		return Metadata{}, err
	}
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil || m.V != 1 || len(m.Files) == 0 {
		return Metadata{}, fmt.Errorf("%w: unreadable metadata", ErrHeader)
	}
	for _, f := range m.Files {
		if f.Size < 0 {
			return Metadata{}, fmt.Errorf("%w: negative file size", ErrHeader)
		}
	}
	return m, nil
}
