package cpace

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// draft-irtf-cfrg-cpace-20, appendices A.1 and B.3 (ristretto255, SHA-512): the same vectors
// as web/test/cpace.test.ts.
func h(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

var (
	prs = []byte("Password")
	ci  = h("0b415f696e69746961746f720b425f726573706f6e646572")
	sid = h("7e4b4791d6a8ef019b936c79fb7f2c57")
)

func TestStringHelpers(t *testing.T) {
	if got := hex.EncodeToString(LVCat([]byte("1234"), []byte("5"), nil, []byte("678"))); got != "043132333401350003363738" {
		t.Errorf("lv_cat: %s", got)
	}
	long := make([]byte, 128)
	if got := hex.EncodeToString(prependLen(long)[:3]); got != "800100" {
		t.Errorf("prepend_len(128 bytes): %s", got)
	}
}

func TestDraftVectors(t *testing.T) {
	if n := len(generatorString(prs, ci, sid)); n != 170 {
		t.Errorf("generator string: %d bytes", n)
	}
	if got := hex.EncodeToString(generator(prs, ci, sid).Bytes()); got != "222b6b195fe84b1652badb6f6a3ae3d24341e7306967f0b8115b40d5698c7e56" {
		t.Fatalf("generator %s", got)
	}
	a, _ := NewShare(prs, ci, sid, h("da3d23700a9e5699258aef94dc060dfda5ebb61f02a5ea77fad53f4ff0976d08"))
	b, _ := NewShare(prs, ci, sid, h("d2316b454718c35362d83d69df6320f38578ed5984651435e2949762d900b80d"))
	if hex.EncodeToString(a.Y) != "d6bac480f2c386c394efc7c47adb9925dcd2630b64f240c50f8d0eec482b9157" ||
		hex.EncodeToString(b.Y) != "3ea7e0b19560d7c0b0f5734f63b955286dfa8232b5ebe63324e2d9e7433f7258" {
		t.Fatalf("Ya %x, Yb %x", a.Y, b.Y)
	}
	want := h("b69effbf61b51d56401c0f65601abe428de8206feaaf0e32198896dcae7b35cd2b38950a39dfd5d4a79164614c2984f7daa460b588c1e80c3fa2068af7900447")
	ka, err := a.ISK(b.Y, sid, a.Y, []byte("ADa"), b.Y, []byte("ADb"))
	if err != nil || !bytes.Equal(ka, want) {
		t.Errorf("ISK of A: %x %v", ka, err)
	}
	kb, err := b.ISK(a.Y, sid, a.Y, []byte("ADa"), b.Y, []byte("ADb"))
	if err != nil || !bytes.Equal(kb, want) {
		t.Errorf("ISK of B: %x %v", kb, err)
	}
}

func TestInvalidPointsAbort(t *testing.T) {
	s, _ := NewShare(prs, ci, sid, h("7cd0e075fa7955ba52c02759a6c90dbbfc10e6d40aea8d283e407d88cf538a05"))
	k, err := s.scalarMultVfy(h("2c3c6b8c4f3800e7aef6864025b4ed79bd599117e427c41bd47d93d654b4a51c"))
	if err != nil || hex.EncodeToString(k) != "7c13645fe790a468f62c39beb7388e541d8405d1ade69d1778c5fe3e7f6b600e" {
		t.Errorf("valid point: %x %v", k, err)
	}
	for _, bad := range [][]byte{h("2b3c6b8c4f3800e7aef6864025b4ed79bd599117e427c41bd47d93d654b4a51c"), make([]byte, 32)} {
		if _, err := s.scalarMultVfy(bad); err != ErrInvalidPoint {
			t.Errorf("%x: %v", bad, err)
		}
	}
}
