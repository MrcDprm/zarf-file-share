package envelope

import (
	"bytes"
	"crypto/ecdh"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// The shared test vectors (PROTOCOL.md section 9). The Go and TypeScript implementations
// must both rebuild every "accept" envelope byte for byte and refuse every "reject" one.
// Regenerate after a deliberate change with: go test ./internal/envelope -run Vectors -update

var update = flag.Bool("update", false, "rewrite testdata/vectors/envelope.json")

var vectorFile = filepath.Join("..", "..", "testdata", "vectors", "envelope.json")

type argonVector struct {
	MemoryKiB uint32 `json:"m_kib"`
	Time      uint32 `json:"t"`
	Threads   uint8  `json:"p"`
	Salt      string `json:"salt"`
}

type acceptVector struct {
	Name      string       `json:"name"`
	Mode      Mode         `json:"mode"`
	ChunkLog2 uint8        `json:"chunk_log2"`
	Salt      string       `json:"salt"`
	FileKey   string       `json:"file_key,omitempty"`
	Password  string       `json:"password,omitempty"`
	Argon     *argonVector `json:"argon,omitempty"`
	EphPriv   string       `json:"eph_priv,omitempty"`
	ReqPriv   string       `json:"req_priv,omitempty"`
	Metadata  Metadata     `json:"metadata"`
	Content   string       `json:"content"`
	Envelope  string       `json:"envelope"`
}

type rejectVector struct {
	Name     string `json:"name"`
	Base     string `json:"base"`               // the accept vector whose keys to use
	FileKey  string `json:"file_key,omitempty"` // a different key, for "wrong key"
	Envelope string `json:"envelope"`
	Error    string `json:"error"` // header, wrong_key, auth, truncated, trailing
}

type vectorSet struct {
	Note   string         `json:"note"`
	Accept []acceptVector `json:"accept"`
	Reject []rejectVector `json:"reject"`
}

func filled(b byte, n int) []byte { return bytes.Repeat([]byte{b}, n) }

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// inputs turns a vector into writer parameters and the reader's key function.
func inputs(t *testing.T, v acceptVector) (Params, []byte, KeyFunc) {
	t.Helper()
	p := Params{Mode: v.Mode, ChunkLog2: v.ChunkLog2}
	copy(p.Salt[:], unhex(t, v.Salt))
	switch v.Mode {
	case ModeLink:
		k := unhex(t, v.FileKey)
		return p, LinkKey(k), func(*Header) ([]byte, error) { return LinkKey(k), nil }
	case ModeLinkPassword:
		p.Argon = ArgonParams{MemoryKiB: v.Argon.MemoryKiB, Time: v.Argon.Time, Threads: v.Argon.Threads}
		copy(p.Argon.Salt[:], unhex(t, v.Argon.Salt))
		k := unhex(t, v.FileKey)
		return p, PasswordKey(k, v.Password, p.Argon), func(h *Header) ([]byte, error) { return PasswordKey(k, v.Password, h.Argon), nil }
	case ModeRequest:
		eph, err := ecdh.X25519().NewPrivateKey(unhex(t, v.EphPriv))
		if err != nil {
			t.Fatal(err)
		}
		req, err := ecdh.X25519().NewPrivateKey(unhex(t, v.ReqPriv))
		if err != nil {
			t.Fatal(err)
		}
		copy(p.EphPub[:], eph.PublicKey().Bytes())
		ikm, err := RequestKey(eph, req.PublicKey(), eph.PublicKey().Bytes(), req.PublicKey().Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return p, ikm, func(h *Header) ([]byte, error) {
			pub, err := ecdh.X25519().NewPublicKey(h.EphPub[:])
			if err != nil {
				return nil, err
			}
			return RequestKey(req, pub, h.EphPub[:], req.PublicKey().Bytes())
		}
	}
	t.Fatalf("no inputs for mode %v", v.Mode)
	return Params{}, nil, nil
}

func build(t *testing.T, v acceptVector) []byte {
	t.Helper()
	p, ikm, _ := inputs(t, v)
	var out bytes.Buffer
	w, err := NewWriter(&out, p, ikm, v.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(unhex(t, v.Content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func file(name string, size int) Metadata {
	return Metadata{V: 1, Files: []File{{Name: name, Size: int64(size), Type: "application/octet-stream"}}}
}

func makeVectors(t *testing.T) vectorSet {
	hx := hex.EncodeToString
	pat := func(n int) string { return hx(pattern(n)) }
	accept := []acceptVector{
		{Name: "link, empty file", Mode: ModeLink, ChunkLog2: 12, Salt: hx(filled(0x01, 32)), FileKey: hx(filled(0x02, 32)), Metadata: file("bos.txt", 0), Content: ""},
		{Name: "link, one short chunk", Mode: ModeLink, ChunkLog2: 16, Salt: hx(filled(0x03, 32)), FileKey: hx(filled(0x04, 32)),
			Metadata: Metadata{V: 1, Files: []File{{Name: "Merhaba <Zarf> & ünlü.txt", Size: 15, Type: "text/plain"}}}, Content: hx([]byte("Merhaba, Zarf!\n"))},
		{Name: "link, three chunks", Mode: ModeLink, ChunkLog2: 12, Salt: hx(filled(0x05, 32)), FileKey: hx(filled(0x06, 32)), Metadata: file("uc-parca.bin", 9000), Content: pat(9000)},
		{Name: "link, last chunk exactly full", Mode: ModeLink, ChunkLog2: 12, Salt: hx(filled(0x07, 32)), FileKey: hx(filled(0x08, 32)), Metadata: file("tam.bin", 8192), Content: pat(8192)},
		{Name: "link with password", Mode: ModeLinkPassword, ChunkLog2: 16, Salt: hx(filled(0x09, 32)), FileKey: hx(filled(0x0A, 32)), Password: "doğru şifre",
			Argon: &argonVector{MemoryKiB: 256, Time: 1, Threads: 1, Salt: hx([]byte("zarf-test-salt-1"))}, Metadata: file("sifreli.txt", 5), Content: hx([]byte("gizli"))},
		{Name: "request", Mode: ModeRequest, ChunkLog2: 16, Salt: hx(filled(0x0B, 32)), EphPriv: hx(filled(0x11, 32)), ReqPriv: hx(filled(0x22, 32)),
			Metadata: file("kimlik.jpg", 6), Content: hx([]byte("kimlik"))},
	}
	for i := range accept {
		accept[i].Envelope = hx(build(t, accept[i]))
	}

	three, full := unhex(t, accept[2].Envelope), unhex(t, accept[3].Envelope)
	start, chunk := layout(t, three)
	mutate := func(b []byte, f func([]byte) []byte) string { return hx(f(bytes.Clone(b))) }
	pw := unhex(t, accept[4].Envelope)
	reject := []rejectVector{
		{Name: "wrong key", Base: accept[2].Name, FileKey: hx(filled(0x66, 32)), Envelope: accept[2].Envelope, Error: "wrong_key"},
		{Name: "changed salt", Base: accept[2].Name, Envelope: mutate(three, func(b []byte) []byte { b[10] ^= 1; return b }), Error: "wrong_key"},
		{Name: "changed commitment", Base: accept[2].Name, Envelope: mutate(three, func(b []byte) []byte { b[start-int(binary.BigEndian.Uint32(b[39:43]))-1] ^= 1; return b }), Error: "wrong_key"},
		{Name: "changed content byte", Base: accept[2].Name, Envelope: mutate(three, func(b []byte) []byte { b[start+chunk+5] ^= 1; return b }), Error: "auth"},
		{Name: "swapped chunks", Base: accept[2].Name, Envelope: mutate(three, func(b []byte) []byte {
			first := bytes.Clone(b[start : start+chunk])
			copy(b[start:], b[start+chunk:start+2*chunk])
			copy(b[start+chunk:], first)
			return b
		}), Error: "auth"},
		{Name: "cut after a chunk", Base: accept[2].Name, Envelope: hx(three[:start+2*chunk]), Error: "truncated"},
		{Name: "bytes after the end", Base: accept[3].Name, Envelope: hx(append(bytes.Clone(full), 0)), Error: "trailing"},
		{Name: "hostile password settings", Base: accept[4].Name, Envelope: mutate(pw, func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[39:], 64*1024*1024) // 64 GB
			return b
		}), Error: "header"},
		{Name: "unknown version", Base: accept[2].Name, Envelope: mutate(three, func(b []byte) []byte { b[4] = 2; return b }), Error: "header"},
	}
	return vectorSet{Note: "Zarf envelope test vectors, PROTOCOL.md section 9. Content and keys are hex.", Accept: accept, Reject: reject}
}

func errorName(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrWrongKey):
		return "wrong_key"
	case errors.Is(err, ErrTruncated):
		return "truncated"
	case errors.Is(err, ErrTrailing):
		return "trailing"
	case errors.Is(err, ErrAuth):
		return "auth"
	case errors.Is(err, ErrHeader), errors.Is(err, ErrVersion), errors.Is(err, ErrNotEnvelope):
		return "header"
	}
	return "other: " + err.Error()
}

func TestVectors(t *testing.T) {
	if *update {
		b, err := json.MarshalIndent(makeVectors(t), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(vectorFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorFile, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(vectorFile)
	if err != nil {
		t.Fatal(err)
	}
	var set vectorSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	base := map[string]acceptVector{}
	for _, v := range set.Accept {
		base[v.Name] = v
		if got := hex.EncodeToString(build(t, v)); got != v.Envelope {
			t.Errorf("%s: the writer no longer produces the recorded bytes", v.Name)
		}
		_, _, key := inputs(t, v)
		got, _, err := openWith(unhex(t, v.Envelope), key)
		if err != nil || hex.EncodeToString(got) != v.Content {
			t.Errorf("%s: could not open the recorded envelope: %v", v.Name, err)
		}
	}
	for _, v := range set.Reject {
		b := base[v.Base]
		if v.FileKey != "" {
			b.FileKey = v.FileKey
		}
		_, _, key := inputs(t, b)
		_, _, err := openWith(unhex(t, v.Envelope), key)
		if got := errorName(err); got != v.Error {
			t.Errorf("%s: got %q, want %q", v.Name, got, v.Error)
		}
	}
}

func openWith(env []byte, key KeyFunc) ([]byte, *Reader, error) {
	r, err := Open(bytes.NewReader(env), key)
	if err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	_, err = out.ReadFrom(r)
	return out.Bytes(), r, err
}
