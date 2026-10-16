package envelope

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

const smallChunk = 12 // 4 KiB chunks keep the tests fast and multi-chunk

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

var oneFile = Metadata{Files: []File{{Name: "rapor.pdf", Size: 0, Type: "application/pdf"}}}

func seal(t *testing.T, p Params, ikm, content []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := NewWriter(&out, p, ikm, oneFile)
	if err != nil {
		t.Fatal(err)
	}
	// Uneven writes, so chunk boundaries never line up with Write calls.
	for len(content) > 0 {
		n := min(len(content), 1000)
		if _, err := w.Write(content[:n]); err != nil {
			t.Fatal(err)
		}
		content = content[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func openAll(env []byte, ikm []byte) ([]byte, *Reader, error) {
	r, err := Open(bytes.NewReader(env), func(*Header) ([]byte, error) { return ikm, nil })
	if err != nil {
		return nil, nil, err
	}
	b, err := io.ReadAll(r)
	return b, r, err
}

func TestRoundTripAtEveryChunkBoundary(t *testing.T) {
	key := randomKey(t)
	size := 1 << smallChunk
	for _, n := range []int{0, 1, size - 1, size, size + 1, 3 * size, 3*size + 7} {
		content := pattern(n)
		env := seal(t, Params{Mode: ModeLink, ChunkLog2: smallChunk}, LinkKey(key), content)
		got, r, err := openAll(env, LinkKey(key))
		if err != nil {
			t.Fatalf("%d bytes: %v", n, err)
		}
		if !bytes.Equal(got, content) {
			t.Fatalf("%d bytes: content differs", n)
		}
		if r.Metadata.Files[0].Name != "rapor.pdf" {
			t.Fatalf("metadata not restored: %+v", r.Metadata)
		}
	}
}

func TestEverySealIsDifferent(t *testing.T) {
	key := randomKey(t)
	a := seal(t, Params{Mode: ModeLink}, LinkKey(key), []byte("same"))
	b := seal(t, Params{Mode: ModeLink}, LinkKey(key), []byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two envelopes of the same content under the same key are identical: the salt is not random")
	}
}

func TestWrongKeyStopsAtTheCommitment(t *testing.T) {
	env := seal(t, Params{Mode: ModeLink}, LinkKey(randomKey(t)), []byte("gizli"))
	if _, _, err := openAll(env, LinkKey(randomKey(t))); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestPasswordNeedsBothTheLinkAndThePassword(t *testing.T) {
	a := ArgonParams{MemoryKiB: 256, Time: 1, Threads: 1}
	copy(a.Salt[:], "0123456789abcdef")
	link := randomKey(t)
	env := seal(t, Params{Mode: ModeLinkPassword, Argon: a}, PasswordKey(link, "doğru şifre", a), []byte("gizli"))

	if got, _, err := openAll(env, PasswordKey(link, "doğru şifre", a)); err != nil || string(got) != "gizli" {
		t.Fatalf("right link and password: %v", err)
	}
	if _, _, err := openAll(env, PasswordKey(link, "yanlış şifre", a)); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, _, err := openAll(env, LinkKey(link)); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("link without password: got %v", err)
	}
	h, err := ReadHeader(bytes.NewReader(env))
	if err != nil || h.KeyMatches(PasswordKey(link, "yanlış şifre", a)) || !h.KeyMatches(PasswordKey(link, "doğru şifre", a)) {
		t.Fatalf("KeyMatches: %v", err)
	}
	// The reader takes the settings from the header.
	r, err := Open(bytes.NewReader(env), func(h *Header) ([]byte, error) { return PasswordKey(link, "doğru şifre", h.Argon), nil })
	if err != nil || r.Header.Argon.MemoryKiB != 256 {
		t.Fatalf("settings from the header: %v", err)
	}
}

func TestRequestModeOpensOnlyForTheRequester(t *testing.T) {
	requester, _ := ecdh.X25519().GenerateKey(rand.Reader)
	stranger, _ := ecdh.X25519().GenerateKey(rand.Reader)
	eph, _ := ecdh.X25519().GenerateKey(rand.Reader)
	reqPub, ephPub := requester.PublicKey().Bytes(), eph.PublicKey().Bytes()

	ikm, err := RequestKey(eph, requester.PublicKey(), ephPub, reqPub)
	if err != nil {
		t.Fatal(err)
	}
	p := Params{Mode: ModeRequest}
	copy(p.EphPub[:], ephPub)
	env := seal(t, p, ikm, []byte("kimlik fotokopisi"))

	open := func(priv *ecdh.PrivateKey) error {
		_, err := Open(bytes.NewReader(env), func(h *Header) ([]byte, error) {
			pub, err := ecdh.X25519().NewPublicKey(h.EphPub[:])
			if err != nil {
				return nil, err
			}
			return RequestKey(priv, pub, h.EphPub[:], priv.PublicKey().Bytes())
		})
		return err
	}
	if err := open(requester); err != nil {
		t.Fatalf("requester: %v", err)
	}
	if err := open(stranger); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("stranger: got %v", err)
	}
}

func TestRequestKeyRefusesTheZeroPoint(t *testing.T) {
	eph, _ := ecdh.X25519().GenerateKey(rand.Reader)
	zero, err := ecdh.X25519().NewPublicKey(make([]byte, 32))
	if err != nil {
		t.Skip("platform refuses the point already")
	}
	if _, err := RequestKey(eph, zero, eph.PublicKey().Bytes(), make([]byte, 32)); !errors.Is(err, ErrBadPublicKey) {
		t.Fatalf("got %v", err)
	}
}

// --- Attacks from THREAT-MODEL.md: every one must be refused. ---

// layout returns where the content chunks start and how long a full sealed chunk is.
func layout(t *testing.T, env []byte) (start, full int) {
	t.Helper()
	h, err := ReadHeader(bytes.NewReader(env))
	if err != nil {
		t.Fatal(err)
	}
	return len(h.marshal(true)) + int(h.MetaLen), h.chunkSize() + tagSize
}

func threeChunks(t *testing.T) ([]byte, []byte) {
	key := randomKey(t)
	size := 1 << smallChunk
	return seal(t, Params{Mode: ModeLink, ChunkLog2: smallChunk}, LinkKey(key), pattern(2*size+100)), LinkKey(key)
}

func TestChangedBytesAreRefused(t *testing.T) {
	env, key := threeChunks(t)
	for i := 0; i < len(env); i += 97 {
		bad := bytes.Clone(env)
		bad[i] ^= 0x01
		if _, _, err := openAll(bad, key); err == nil {
			t.Fatalf("a flipped bit at byte %d went unnoticed", i)
		}
	}
}

func TestSwappedChunksAreRefused(t *testing.T) {
	env, key := threeChunks(t)
	start, full := layout(t, env)
	bad := bytes.Clone(env)
	copy(bad[start:], env[start+full:start+2*full])
	copy(bad[start+full:], env[start:start+full])
	if _, _, err := openAll(bad, key); !errors.Is(err, ErrAuth) {
		t.Fatalf("got %v, want ErrAuth", err)
	}
}

func TestCutStreamsAreRefused(t *testing.T) {
	env, key := threeChunks(t)
	start, full := layout(t, env)
	for name, end := range map[string]int{
		"after the first chunk":  start + full,
		"after the second chunk": start + 2*full,
		"inside a chunk":         start + full + 10,
		"before any chunk":       start,
	} {
		_, _, err := openAll(env[:end], key)
		if err == nil {
			t.Fatalf("cut %s went unnoticed", name)
		}
		if (name == "after the first chunk" || name == "after the second chunk" || name == "before any chunk") && !errors.Is(err, ErrTruncated) {
			t.Fatalf("cut %s: got %v, want ErrTruncated", name, err)
		}
	}
}

func TestBytesAfterTheEndAreRefused(t *testing.T) {
	key := randomKey(t)
	// A last chunk of exactly full size, then extra bytes.
	env := seal(t, Params{Mode: ModeLink, ChunkLog2: smallChunk}, LinkKey(key), pattern(2<<smallChunk))
	if _, _, err := openAll(append(bytes.Clone(env), 0x00), LinkKey(key)); !errors.Is(err, ErrTrailing) {
		t.Fatalf("got %v, want ErrTrailing", err)
	}
	// A short last chunk, then extra bytes.
	env = seal(t, Params{Mode: ModeLink, ChunkLog2: smallChunk}, LinkKey(key), pattern(100))
	if _, _, err := openAll(append(bytes.Clone(env), 1, 2, 3), LinkKey(key)); err == nil {
		t.Fatal("bytes after a short last chunk went unnoticed")
	}
}

func TestNoContentIsShownBeforeItVerifies(t *testing.T) {
	env, key := threeChunks(t)
	start, _ := layout(t, env)
	bad := bytes.Clone(env)
	bad[start+5] ^= 0x01 // damage the first chunk
	r, err := Open(bytes.NewReader(bad), func(*Header) ([]byte, error) { return key, nil })
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 10)
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, ErrAuth) {
		t.Fatalf("read %d bytes from a damaged chunk (err %v)", n, err)
	}
}

func TestHostileHeadersAreRefusedBeforeAnyWork(t *testing.T) {
	a := ArgonParams{MemoryKiB: 256, Time: 1, Threads: 1}
	env := seal(t, Params{Mode: ModeLinkPassword, Argon: a}, PasswordKey(randomKey(t), "x", a), []byte("x"))
	argonAt := 4 + 1 + 1 + 1 + saltSize
	for name, change := range map[string]func([]byte){
		"64 GB of memory": func(b []byte) { binary.BigEndian.PutUint32(b[argonAt:], 64*1024*1024) },
		"1000 passes":     func(b []byte) { binary.BigEndian.PutUint32(b[argonAt+4:], 1000) },
		"0 threads":       func(b []byte) { b[argonAt+8] = 0 },
		"1 GB chunks":     func(b []byte) { b[6] = 30 },
		"unknown mode":    func(b []byte) { b[5] = 0x7F },
		"version 2":       func(b []byte) { b[4] = 2 },
		"not zarf":        func(b []byte) { b[0] = 'X' },
	} {
		bad := bytes.Clone(env)
		change(bad)
		called := false
		_, err := Open(bytes.NewReader(bad), func(*Header) ([]byte, error) { called = true; return nil, nil })
		if err == nil || called {
			t.Fatalf("%s: accepted (key asked: %v, err %v)", name, called, err)
		}
	}
}

func TestDownloadAuthIsStableAndUnrelatedToTheKeys(t *testing.T) {
	key := randomKey(t)
	a, _ := DownloadAuth(key)
	b, _ := DownloadAuth(key)
	k, _ := deriveKeys(make([]byte, 32), LinkKey(key))
	if !bytes.Equal(a, b) || bytes.Equal(a, key) || bytes.Equal(a, k.content) || len(a) != 32 {
		t.Fatal("download auth must be stable, 32 bytes, and differ from every key")
	}
}

func TestMetadataPaddingHidesNameLength(t *testing.T) {
	short, _ := encodeMetadata(Metadata{Files: []File{{Name: "a"}}})
	long, _ := encodeMetadata(Metadata{Files: []File{{Name: "uzun bir dosya adi olsun.pdf"}}})
	if len(short) != len(long) || len(short)%metaBlock != 0 {
		t.Fatalf("padded sizes %d and %d", len(short), len(long))
	}
	if _, err := decodeMetadata(append(short[:len(short)-1], 1)); err == nil {
		t.Fatal("bad padding accepted")
	}
}
