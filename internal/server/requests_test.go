package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type request struct{ rid, owner string }

func newRequest(t *testing.T, s *Server, label string, maxUploads int) request {
	t.Helper()
	res, out := do(t, s, call{method: "POST", path: "/api/v1/requests", body: map[string]any{"label": label, "expires_in": 86400, "max_uploads": maxUploads}})
	if res.StatusCode != 201 {
		t.Fatalf("create request: %d %v", res.StatusCode, out)
	}
	return request{out["rid"].(string), out["owner_token"].(string)}
}

// give uploads a whole envelope to a request, as the person asked would.
func give(t *testing.T, s *Server, q request, size int, from string) (upload, int) {
	t.Helper()
	res, out := do(t, s, call{method: "POST", path: "/api/v1/requests/" + q.rid + "/uploads", body: map[string]any{"size": size}, from: from})
	if res.StatusCode != 201 {
		return upload{}, res.StatusCode
	}
	u := upload{out["id"].(string), out["upload_token"].(string), out["delete_token"].(string)}
	data := envelopeBytes(size)
	for n := 0; n*testPart < size; n++ {
		if r := putPart(t, s, u, n, data[n*testPart:min((n+1)*testPart, size)]); r.StatusCode != 204 {
			t.Fatalf("part %d: %d", n, r.StatusCode)
		}
	}
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/uploads/" + u.id + "/complete", token: u.token}); res.StatusCode != 200 {
		t.Fatalf("complete: %d", res.StatusCode)
	}
	return u, 201
}

func TestRequestFlow(t *testing.T) {
	s, _ := newTestServer(t, nil)
	q := newRequest(t, s, "  Kimlik\nfotokopisi \u202e ve vergi levhası  ", 5)

	res, info := do(t, s, call{method: "GET", path: "/api/v1/requests/" + q.rid})
	if res.StatusCode != 200 || info["label"] != "Kimlik fotokopisi ve vergi levhası" || info["uploads_left"].(float64) != 5 {
		t.Fatalf("public info: %d %v", res.StatusCode, info)
	}
	if _, ok := info["files"]; ok {
		t.Fatal("the public view lists files")
	}

	u, _ := give(t, s, q, 3000, "")
	res, info = do(t, s, call{method: "GET", path: "/api/v1/requests/" + q.rid, token: q.owner})
	files, _ := info["files"].([]any)
	if res.StatusCode != 200 || len(files) != 1 || files[0].(map[string]any)["id"] != u.id || info["uploads_left"].(float64) != 4 {
		t.Fatalf("owner info: %d %v", res.StatusCode, info)
	}

	// Only the owner token opens a download; the link-mode way does not.
	if code, _ := startDownload(t, s, sent{u, make([]byte, 32), nil}, make([]byte, 32)); code != 403 {
		t.Errorf("link-mode download of a request file: %d", code)
	}
	path := "/api/v1/requests/" + q.rid + "/files/" + u.id + "/download"
	if res, _ := do(t, s, call{method: "POST", path: path, token: b64.EncodeToString(randomToken(32))}); res.StatusCode != 404 {
		t.Errorf("wrong owner token: %d", res.StatusCode)
	}
	res, out := do(t, s, call{method: "POST", path: path, token: q.owner})
	if res.StatusCode != 200 {
		t.Fatalf("owner download: %d", res.StatusCode)
	}
	got, body := fetch(t, s, u.id, out["ticket"].(string), nil)
	if got.StatusCode != 200 || string(body) != string(envelopeBytes(3000)) {
		t.Fatalf("content: %d", got.StatusCode)
	}

	// Another request's owner cannot reach this file.
	other := newRequest(t, s, "", 1)
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/requests/" + other.rid + "/files/" + u.id + "/download", token: other.owner}); res.StatusCode != 404 {
		t.Errorf("file of another request: %d", res.StatusCode)
	}

	// Closing the request deletes what arrived.
	if res, _ := do(t, s, call{method: "DELETE", path: "/api/v1/requests/" + q.rid, token: q.owner}); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if res, _ := do(t, s, call{method: "GET", path: "/api/v1/requests/" + q.rid}); res.StatusCode != 404 {
		t.Errorf("deleted request: %d", res.StatusCode)
	}
	if ok, _ := s.store.exists(t.Context(), u.id); ok {
		t.Error("the file outlived its request")
	}
}

func TestRequestLimits(t *testing.T) {
	s, c := newTestServer(t, nil)
	for _, body := range []map[string]any{
		{"label": "", "expires_in": 86400, "max_uploads": 3},
		{"label": "", "expires_in": 60, "max_uploads": 5},
		{"label": "", "expires_in": 86400, "max_uploads": 5, "owner": "x"},
	} {
		if res, _ := do(t, s, call{method: "POST", path: "/api/v1/requests", body: body}); res.StatusCode != 400 {
			t.Errorf("%v: %d", body, res.StatusCode)
		}
	}
	long := newRequest(t, s, strings.Repeat("ş", 300), 1)
	_, info := do(t, s, call{method: "GET", path: "/api/v1/requests/" + long.rid})
	if n := len([]rune(info["label"].(string))); n != maxLabelLen {
		t.Errorf("label kept %d characters", n)
	}

	// Two uploaders race for the last place: exactly one gets it.
	q := newRequest(t, s, "", 1)
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Go(func() {
			res, _ := do(t, s, call{method: "POST", path: "/api/v1/requests/" + q.rid + "/uploads", body: map[string]any{"size": minEnvelope}, from: fmt.Sprintf("10.0.0.%d:1", i)})
			codes[i] = res.StatusCode
		})
	}
	wg.Wait()
	won := 0
	for _, code := range codes {
		if code == 201 {
			won++
		} else if code != 410 {
			t.Errorf("unexpected answer %d", code)
		}
	}
	if won != 1 {
		t.Errorf("%d uploads took one place", won)
	}

	// An expired request takes nothing, and cleanup removes it.
	c.add(25 * time.Hour)
	if _, code := give(t, s, newRequestAt(t, s), minEnvelope, ""); code != 201 {
		t.Fatalf("fresh request: %d", code)
	}
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/requests/" + q.rid + "/uploads", body: map[string]any{"size": minEnvelope}}); res.StatusCode != 410 {
		t.Errorf("upload to an expired request: %d", res.StatusCode)
	}
	if _, err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.request(t.Context(), q.rid); err == nil {
		t.Error("cleanup left an expired request")
	}
}

func newRequestAt(t *testing.T, s *Server) request { return newRequest(t, s, "", 5) }
