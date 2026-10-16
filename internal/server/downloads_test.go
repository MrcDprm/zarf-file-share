package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type sent struct {
	upload
	dlAuth []byte
	data   []byte
}

// send uploads a whole envelope and completes it, as a sender would.
func send(t *testing.T, s *Server, size, maxDownloads int) sent {
	t.Helper()
	dlAuth := randomToken(32)
	body := map[string]any{"size": size, "expires_in": 86400, "max_downloads": maxDownloads, "dl_auth_hash": b64.EncodeToString(hashToken(dlAuth))}
	res, out := do(t, s, call{method: "POST", path: "/api/v1/uploads", body: body})
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %v", res.StatusCode, out)
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
	return sent{u, dlAuth, data}
}

func startDownload(t *testing.T, s *Server, f sent, auth []byte) (int, string) {
	t.Helper()
	res, out := do(t, s, call{method: "POST", path: "/api/v1/files/" + f.id + "/download", body: map[string]string{"dl_auth": b64.EncodeToString(auth)}})
	ticket, _ := out["ticket"].(string)
	return res.StatusCode, ticket
}

func fetch(t *testing.T, s *Server, id, ticket string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	res, _ := do(t, s, call{method: "GET", path: "/api/v1/files/" + id + "/content", token: ticket, headers: headers})
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestDownloadingAFile(t *testing.T) {
	s, _ := newTestServer(t, nil)
	f := send(t, s, 5500, 5)

	res, info := do(t, s, call{method: "GET", path: "/api/v1/files/" + f.id})
	if res.StatusCode != 200 || info["size"].(float64) != 5500 || info["downloads_left"].(float64) != 5 {
		t.Fatalf("info: %d %v", res.StatusCode, info)
	}
	if code, _ := startDownload(t, s, f, randomToken(32)); code != 403 {
		t.Fatalf("wrong download value: %d", code)
	}
	code, ticket := startDownload(t, s, f, f.dlAuth)
	if code != 200 || ticket == "" {
		t.Fatalf("start: %d", code)
	}
	res, body := fetch(t, s, f.id, ticket, nil)
	if res.StatusCode != 200 || !bytes.Equal(body, f.data) {
		t.Fatalf("content: %d, %d bytes", res.StatusCode, len(body))
	}
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", res.Header)
	}
	// A broken download resumes with a range, on the same ticket and without spending another.
	res, body = fetch(t, s, f.id, ticket, map[string]string{"Range": "bytes=5000-"})
	if res.StatusCode != 206 || !bytes.Equal(body, f.data[5000:]) {
		t.Fatalf("range: %d, %d bytes", res.StatusCode, len(body))
	}
	if _, info := do(t, s, call{method: "GET", path: "/api/v1/files/" + f.id}); info["downloads_left"].(float64) != 4 {
		t.Fatalf("one download should be spent: %v", info)
	}
}

func TestTicketsBelongToOneFileAndExpire(t *testing.T) {
	s, c := newTestServer(t, nil)
	a, b := send(t, s, 3000, 5), send(t, s, 3000, 5)
	_, ticket := startDownload(t, s, a, a.dlAuth)
	if res, _ := fetch(t, s, b.id, ticket, nil); res.StatusCode != 404 {
		t.Fatalf("a's ticket opened b: %d", res.StatusCode)
	}
	if res, _ := fetch(t, s, a.id, "", nil); res.StatusCode != 404 {
		t.Fatalf("no ticket: %d", res.StatusCode)
	}
	if res, _ := fetch(t, s, a.id, b64.EncodeToString(randomToken(32)), nil); res.StatusCode != 404 {
		t.Fatalf("made-up ticket: %d", res.StatusCode)
	}
	c.add(ticketLife)
	if res, _ := fetch(t, s, a.id, ticket, nil); res.StatusCode != 404 {
		t.Fatalf("expired ticket: %d", res.StatusCode)
	}
}

func TestTheDownloadLimitHoldsUnderARush(t *testing.T) {
	s, _ := newTestServer(t, nil)
	f := send(t, s, 3000, 5)
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/v1/files/"+f.id+"/download", bytes.NewReader([]byte(`{"dl_auth":"`+b64.EncodeToString(f.dlAuth)+`"}`)))
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code == 200 {
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got != 5 {
		t.Fatalf("%d downloads started, the limit is 5", got)
	}
	if res, out := do(t, s, call{method: "GET", path: "/api/v1/files/" + f.id}); res.StatusCode != 410 || out["error"] != "gone" {
		t.Fatalf("used up: %d %v", res.StatusCode, out)
	}
}

func TestUnfinishedUploadsCannotBeDownloaded(t *testing.T) {
	s, _ := newTestServer(t, nil)
	u := create(t, s, 3000)
	if res, _ := do(t, s, call{method: "GET", path: "/api/v1/files/" + u.id}); res.StatusCode != 404 {
		t.Fatalf("got %d", res.StatusCode)
	}
}

func TestDeletingAFile(t *testing.T) {
	s, _ := newTestServer(t, nil)
	f := send(t, s, 3000, 5)
	if res, _ := do(t, s, call{method: "DELETE", path: "/api/v1/files/" + f.id, token: f.token}); res.StatusCode != 404 {
		t.Fatalf("the upload token must not delete: %d", res.StatusCode)
	}
	if res, _ := do(t, s, call{method: "DELETE", path: "/api/v1/files/" + f.id, token: f.deleteToken}); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, err := os.Stat(s.blobs.dir(f.id)); !os.IsNotExist(err) {
		t.Fatal("the bytes are still on disk")
	}
	if res, _ := do(t, s, call{method: "GET", path: "/api/v1/files/" + f.id}); res.StatusCode != 404 {
		t.Fatalf("after delete: %d", res.StatusCode)
	}
}

func TestCleanup(t *testing.T) {
	s, c := newTestServer(t, nil)
	ctx := context.Background()
	expiring := send(t, s, 3000, 5)
	usedUp := send(t, s, 3000, 1)
	abandoned := create(t, s, 3000)
	putPart(t, s, abandoned, 0, envelopeBytes(testPart))
	keep := send(t, s, 3000, 5)
	orphan := filepath.Join(s.blobs.root, newID())
	os.MkdirAll(orphan, 0o700)

	_, ticket := startDownload(t, s, usedUp, usedUp.dlAuth)
	if n, err := s.Cleanup(ctx); err != nil || n != 0 {
		t.Fatalf("nothing is due yet: %d %v", n, err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("leftover bytes without a file were kept")
	}
	// A used-up file stays while its last download may still be running.
	if res, _ := fetch(t, s, usedUp.id, ticket, nil); res.StatusCode != 200 {
		t.Fatalf("running download: %d", res.StatusCode)
	}

	c.add(ticketLife) // the used-up file's ticket ran out
	if n, _ := s.Cleanup(ctx); n != 1 {
		t.Fatalf("used up: %d removed", n)
	}
	c.add(staleUpload) // a day later: the abandoned upload and the 1-day file go
	if n, _ := s.Cleanup(ctx); n != 3 {
		t.Fatalf("expired and abandoned: %d removed", n)
	}
	for _, id := range []string{expiring.id, usedUp.id, abandoned.id, keep.id} {
		if _, err := os.Stat(s.blobs.dir(id)); !os.IsNotExist(err) {
			t.Fatalf("%s still on disk", id)
		}
	}
}

func TestReports(t *testing.T) {
	s, _ := newTestServer(t, nil)
	f := send(t, s, 3000, 5)
	reason := "zararlı\u202E dosya\n\tikinci satır"
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/files/" + f.id + "/report", body: map[string]string{"reason": reason}}); res.StatusCode != 204 {
		t.Fatalf("report: %d", res.StatusCode)
	}
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/files/" + newID() + "/report", body: map[string]string{"reason": "x"}}); res.StatusCode != 204 {
		t.Fatalf("unknown file should get the same answer: %d", res.StatusCode)
	}
	reports, err := s.Reports(context.Background())
	if err != nil || len(reports) != 1 || reports[0].Reason != "zararlı dosya ikinci satır" {
		t.Fatalf("reports: %+v %v", reports, err)
	}
	for i := 0; i < 9; i++ {
		do(t, s, call{method: "POST", path: "/api/v1/files/" + f.id + "/report", body: map[string]string{"reason": "x"}})
	}
	if res, _ := do(t, s, call{method: "POST", path: "/api/v1/files/" + f.id + "/report", body: map[string]string{"reason": "x"}}); res.StatusCode != 429 {
		t.Fatalf("eleventh report in an hour: %d", res.StatusCode)
	}
	if err := s.Remove(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	if res, _ := do(t, s, call{method: "GET", path: "/api/v1/files/" + f.id}); res.StatusCode != 404 {
		t.Fatal("the operator's removal did not take")
	}
}
