// Package cpace is the password-authenticated key exchange of live mode: CPace
// (draft-irtf-cfrg-cpace-20) with ristretto255 and SHA-512. It matches web/src/lib/cpace.ts
// byte for byte; both are checked against the test vectors of the draft's appendix B.3.
package cpace

import (
	"crypto/rand"
	"crypto/sha512"
	"errors"

	"github.com/gtank/ristretto255"
)

var dsi = []byte("CPaceRistretto255")

const sha512Block = 128

// ErrInvalidPoint means the peer sent an invalid encoding or the neutral element: abort.
var ErrInvalidPoint = errors.New("cpace: invalid point")

// prependLen puts the length of data, in LEB128, in front of it.
func prependLen(data []byte) []byte {
	var out []byte
	n := len(data)
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			break
		}
	}
	return append(out, data...)
}

// LVCat is the length-value concatenation of the draft.
func LVCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, prependLen(p)...)
	}
	return out
}

func generatorString(prs, ci, sid []byte) []byte {
	zpad := max(0, sha512Block-len(prependLen(prs))-len(prependLen(dsi))-1)
	return LVCat(dsi, prs, make([]byte, zpad), ci, sid)
}

func generator(prs, ci, sid []byte) *ristretto255.Element {
	h := sha512.Sum512(generatorString(prs, ci, sid))
	g, err := ristretto255.NewElement().SetUniformBytes(h[:])
	if err != nil {
		panic(err) // 64 bytes always map to an element
	}
	return g
}

// Share is one side's secret scalar and the message it sends.
type Share struct {
	y *ristretto255.Scalar
	Y []byte
}

// scalarFromLE reads a little-endian scalar below 2^252 (so always canonical).
func scalarFromLE(b []byte) (*ristretto255.Scalar, error) {
	return ristretto255.NewScalar().SetCanonicalBytes(b)
}

// sampleScalar draws 32 random bytes and clears the bits above 252, as the draft recommends.
func sampleScalar() *ristretto255.Scalar {
	for {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		b[31] &= 0x0f
		s, err := scalarFromLE(b)
		if err == nil && s.Equal(ristretto255.NewScalar()) == 0 {
			return s
		}
	}
}

// NewShare computes this side's message from the code (prs), the channel (ci) and the
// session id. scalar is for tests; nil draws a random one.
func NewShare(prs, ci, sid, scalar []byte) (*Share, error) {
	y := sampleScalar()
	if scalar != nil {
		var err error
		if y, err = scalarFromLE(scalar); err != nil {
			return nil, err
		}
	}
	Y := ristretto255.NewElement().ScalarMult(y, generator(prs, ci, sid))
	return &Share{y: y, Y: Y.Bytes()}, nil
}

// scalarMultVfy is y times the peer's point, refusing invalid encodings and the identity.
func (s *Share) scalarMultVfy(peer []byte) ([]byte, error) {
	p, err := ristretto255.NewElement().SetCanonicalBytes(peer)
	if err != nil || p.Equal(ristretto255.NewIdentityElement()) == 1 {
		return nil, ErrInvalidPoint
	}
	k := ristretto255.NewElement().ScalarMult(s.y, p)
	if k.Equal(ristretto255.NewIdentityElement()) == 1 {
		return nil, ErrInvalidPoint
	}
	return k.Bytes(), nil
}

// ISK is the intermediate session key, with the initiator/responder transcript: A's message
// and associated data first, whichever side computes it.
func (s *Share) ISK(peerY, sid, ya, ada, yb, adb []byte) ([]byte, error) {
	k, err := s.scalarMultVfy(peerY)
	if err != nil {
		return nil, err
	}
	in := LVCat(append(append([]byte(nil), dsi...), "_ISK"...), sid, k)
	in = append(in, LVCat(ya, ada)...)
	in = append(in, LVCat(yb, adb)...)
	h := sha512.Sum512(in)
	return h[:], nil
}
