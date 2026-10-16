package envelope

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"golang.org/x/crypto/argon2"
)

// keys are the envelope's working keys, all derived from its salt and the mode's input key (PROTOCOL.md 2.2).
type keys struct {
	content, meta, commit []byte
}

func deriveKeys(salt []byte, ikm []byte) (keys, error) {
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return keys{}, err
	}
	expand := func(label string) ([]byte, error) { return hkdf.Expand(sha256.New, prk, label, 32) }
	var k keys
	if k.content, err = expand("zarf v1 content"); err != nil {
		return keys{}, err
	}
	if k.meta, err = expand("zarf v1 metadata"); err != nil {
		return keys{}, err
	}
	if k.commit, err = expand("zarf v1 commitment"); err != nil {
		return keys{}, err
	}
	return k, nil
}

// commitment binds the header (up to the commitment field) to one key, so a crafted
// envelope cannot open under two different keys (PROTOCOL.md 2.3).
func commitment(commitKey []byte, h *Header) []byte {
	start := sha256.Sum256(h.marshal(false))
	mac := hmac.New(sha256.New, commitKey)
	mac.Write([]byte("zarf v1 commit"))
	mac.Write(start[:])
	return mac.Sum(nil)
}

// The input key of each mode.

// LinkKey is the input key of a link without a password: the 32 random bytes after "#".
func LinkKey(fileKey []byte) []byte { return append([]byte(nil), fileKey...) }

// PasswordKey needs both the link key and the password: either one alone opens nothing.
func PasswordKey(fileKey []byte, password string, a ArgonParams) []byte {
	pw := argon2.IDKey([]byte(password), a.Salt[:], a.Time, a.MemoryKiB, a.Threads, 32)
	return append(append([]byte(nil), fileKey...), pw...)
}

// ErrBadPublicKey means an X25519 key agreement produced the all-zero value (a malicious key).
var ErrBadPublicKey = errors.New("envelope: invalid public key")

// RequestKey is the input key of request mode: the X25519 shared secret, bound to both
// public keys as in HPKE. The sender passes its one-time private key and the requester's
// public key; the requester its own private key and the sender's one-time public key.
func RequestKey(priv *ecdh.PrivateKey, peer *ecdh.PublicKey, ephPub, reqPub []byte) ([]byte, error) {
	shared, err := priv.ECDH(peer) // crypto/ecdh rejects the all-zero result
	if err != nil {
		return nil, ErrBadPublicKey
	}
	ikm := append(shared, ephPub...)
	return append(ikm, reqPub...), nil
}

// DownloadAuth is what a downloader shows the server to spend a download. It comes from
// the link key by its own label, so it says nothing about the encryption keys.
func DownloadAuth(fileKey []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, fileKey, nil, "zarf v1 download auth", 32)
}

// KeyMatches checks an input key against the header's commitment without touching any
// ciphertext, so a reader can ask for a password again instead of failing the whole file.
func (h *Header) KeyMatches(ikm []byte) bool {
	k, err := deriveKeys(h.Salt[:], ikm)
	return err == nil && hmac.Equal(commitment(k.commit, h), h.Commitment[:])
}
