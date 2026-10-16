package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a time source the tests move by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

const testPart = 2000 // small parts, so a test upload has several

func newTestServer(t *testing.T, change func(*Config)) (*Server, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	cfg := DefaultConfig(t.TempDir())
	cfg.PartSize, cfg.Now = testPart, c.now
	if change != nil {
		change(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, c
}

type call struct {
	method, path string
	body         any    // JSON, or []byte sent as is
	token        string // bearer
	from         string // RemoteAddr
	headers      map[string]string
}

func do(t *testing.T, s *Server, c call) (*http.Response, map[string]any) {
	t.Helper()
	var body io.Reader
	switch b := c.body.(type) {
	case nil:
	case []byte:
		body = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		body = bytes.NewReader(j)
	}
	req := httptest.NewRequest(c.method, c.path, body)
	if c.from != "" {
		req.RemoteAddr = c.from
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	res := rec.Result()
	var out map[string]any
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(res.Body).Decode(&out)
	}
	return res, out
}

func validCreate(size int64) map[string]any {
	return map[string]any{"size": size, "expires_in": 86400, "max_downloads": 5, "dl_auth_hash": b64.EncodeToString(bytes.Repeat([]byte{7}, 32))}
}

type upload struct{ id, token, deleteToken string }

func create(t *testing.T, s *Server, size int64) upload {
	t.Helper()
	res, out := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: validCreate(size)})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %v", res.StatusCode, out)
	}
	return upload{out["id"].(string), out["upload_token"].(string), out["delete_token"].(string)}
}

func putPart(t *testing.T, s *Server, u upload, n int, data []byte) *http.Response {
	t.Helper()
	res, _ := do(t, s, call{method: "PUT", path: fmt.Sprintf("/api/v1/uploads/%s/parts/%d", u.id, n), body: data, token: u.token})
	return res
}

func envelopeBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func TestCreateChecksEveryField(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for name, c := range map[string]struct {
		change func(map[string]any)
		status int
		code   string
	}{
		"over 1024 MB":       {func(m map[string]any) { m["size"] = 1024*MB + 1 }, 413, "too_large"},
		"smaller than any":   {func(m map[string]any) { m["size"] = 10 }, 400, "invalid_size"},
		"odd expiry":         {func(m map[string]any) { m["expires_in"] = 1234 }, 400, "invalid_expiry"},
		"30 days":            {func(m map[string]any) { m["expires_in"] = 30 * 86400 }, 400, "invalid_expiry"},
		"odd download limit": {func(m map[string]any) { m["max_downloads"] = 3 }, 400, "invalid_download_limit"},
		"short auth hash":    {func(m map[string]any) { m["dl_auth_hash"] = "abc" }, 400, "invalid_request"},
		"extra field":        {func(m map[string]any) { m["owner"] = "me" }, 400, "invalid_request"},
	} {
		body := validCreate(5000)
		c.change(body)
		res, out := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: body})
		if res.StatusCode != c.status || out["error"] != c.code {
			t.Errorf("%s: %d %v", name, res.StatusCode, out)
		}
	}
	res, out := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: validCreate(1024 * MB)})
	if res.StatusCode != 201 || len(out["id"].(string)) != 22 || len(out["upload_token"].(string)) != 43 {
		t.Fatalf("exactly 1024 MB should be fine: %d %v", res.StatusCode, out)
	}
}

func TestTheWholeUploadAndResuming(t *testing.T) {
	s, _ := newTestServer(t, nil)
	data := envelopeBytes(5500) // parts of 2000, 2000 and 1500 bytes
	u := create(t, s, int64(len(data)))

	if r := putPart(t, s, u, 0, data[:2000]); r.StatusCode != 204 {
		t.Fatalf("part 0: %d", r.StatusCode)
	}
	if r := putPart(t, s, u, 2, data[4000:]); r.StatusCode != 204 {
		t.Fatalf("part 2: %d", r.StatusCode)
	}
	// Completing too early names what is missing.
	res, out := do(t, s, call{method: "POST", path: "/api/v1/uploads/" + u.id + "/complete", token: u.token})
	if res.StatusCode != 409 || out["error"] != "missing_parts" {
		t.Fatalf("early complete: %d %v", res.StatusCode, out)
	}
	// After a broken connection, the sender asks what arrived.
	_, status := do(t, s, call{method: "GET", path: "/api/v1/uploads/" + u.id, token: u.token})
	if fmt.Sprint(status["received"]) != "[0 2]" || status["parts"].(float64) != 3 {
		t.Fatalf("status: %v", status)
	}
	if r := putPart(t, s, u, 1, data[2000:4000]); r.StatusCode != 204 {
		t.Fatalf("part 1: %d", r.StatusCode)
	}
	if r := putPart(t, s, u, 1, data[2000:4000]); r.StatusCode != 204 { // sending a part again is fine
		t.Fatalf("part 1 again: %d", r.StatusCode)
	}
	res, _ = do(t, s, call{method: "POST", path: "/api/v1/uploads/" + u.id + "/complete", token: u.token})
	if res.StatusCode != 200 {
		t.Fatalf("complete: %d", res.StatusCode)
	}
	stored, err := os.ReadFile(s.blobs.envelopePath(u.id))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("stored envelope differs (%v)", err)
	}
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/uploads/" + u.id + "/complete", token: u.token}); res.StatusCode != 200 {
		t.Fatal("completing twice should be harmless")
	}
	if r := putPart(t, s, u, 0, data[:2000]); r.StatusCode != 409 {
		t.Fatalf("a part after completion: %d", r.StatusCode)
	}
}

func TestPartsMustHaveTheirExactSize(t *testing.T) {
	s, _ := newTestServer(t, nil)
	u := create(t, s, 5500)
	for name, c := range map[string]struct {
		n    int
		size int
	}{
		"too short":       {0, 1999},
		"too long":        {0, 2001},
		"last too long":   {2, 1501},
		"past the end":    {3, 10},
		"negative number": {-1, 10},
	} {
		if r := putPart(t, s, u, c.n, envelopeBytes(c.size)); r.StatusCode != 400 {
			t.Errorf("%s: %d", name, r.StatusCode)
		}
	}
	if entries, _ := os.ReadDir(s.blobs.dir(u.id)); len(entries) != 0 {
		t.Fatalf("refused parts left files behind: %v", entries)
	}
}

func TestTokensAndIdsAreChecked(t *testing.T) {
	s, _ := newTestServer(t, nil)
	u := create(t, s, 5500)
	other := create(t, s, 5500)
	for name, c := range map[string]call{
		"no token":         {method: "GET", path: "/api/v1/uploads/" + u.id},
		"another's token":  {method: "GET", path: "/api/v1/uploads/" + u.id, token: other.token},
		"the delete token": {method: "GET", path: "/api/v1/uploads/" + u.id, token: u.deleteToken},
		"unknown id":       {method: "GET", path: "/api/v1/uploads/" + newID(), token: u.token},
		"path in id":       {method: "GET", path: "/api/v1/uploads/..%2F..%2Fzarf.db", token: u.token},
		"malformed token":  {method: "GET", path: "/api/v1/uploads/" + u.id, token: "not-base64!"},
	} {
		if res, out := do(t, s, c); res.StatusCode != 404 || out["error"] != "not_found" {
			t.Errorf("%s: %d %v", name, res.StatusCode, out)
		}
	}
}

func TestExpiredUploadsTakeNoMoreParts(t *testing.T) {
	s, c := newTestServer(t, nil)
	u := create(t, s, 5500)
	c.add(24 * time.Hour)
	if r := putPart(t, s, u, 0, envelopeBytes(2000)); r.StatusCode != 410 {
		t.Fatalf("got %d", r.StatusCode)
	}
}

func TestStorageQuota(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.StorageQuota = 10000 })
	create(t, s, 6000)
	res, out := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: validCreate(5000)})
	if res.StatusCode != 507 || out["error"] != "storage_full" {
		t.Fatalf("got %d %v", res.StatusCode, out)
	}
}

func TestUploadsPerAddressAndForgedForwardedHeaders(t *testing.T) {
	s, c := newTestServer(t, func(c *Config) { c.UploadsPerHour = 2; c.TrustProxy = true })
	try := func(from, xff string) int {
		h := map[string]string{}
		if xff != "" {
			h["X-Forwarded-For"] = xff
		}
		res, _ := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: validCreate(5000), from: from, headers: h})
		return res.StatusCode
	}
	// Directly from the internet: a forged X-Forwarded-For changes nothing.
	for i, xff := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		want := map[bool]int{true: 201, false: 429}[i < 2]
		if got := try("203.0.113.9:4000", xff); got != want {
			t.Fatalf("direct request %d: got %d, want %d", i, got, want)
		}
	}
	// Through the local proxy: the address it appended counts, not what the client wrote before it.
	if try("127.0.0.1:5000", "6.6.6.6, 198.51.100.7") != 201 || try("127.0.0.1:5000", "7.7.7.7, 198.51.100.7") != 201 {
		t.Fatal("first two through the proxy should pass")
	}
	if got := try("127.0.0.1:5000", "8.8.8.8, 198.51.100.7"); got != 429 {
		t.Fatalf("changing the client-written part must not reset the limit: %d", got)
	}
	c.add(time.Hour)
	if got := try("203.0.113.9:4000", ""); got != 201 {
		t.Fatalf("a new hour: %d", got)
	}
}

func TestErrorsNeverLeakDetails(t *testing.T) {
	s, _ := newTestServer(t, nil)
	res, _ := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: []byte("{not json")})
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 400 || strings.Contains(string(b), "invalid character") {
		t.Fatalf("got %d %s", res.StatusCode, b)
	}
}
