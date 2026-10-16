package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MrcDprm/zarf-file-share/internal/server"
)

const testPartSize = 64 * 1024

// startServer runs a real Zarf server in the test. wrap, when given, can misbehave on purpose.
func startServer(t *testing.T, wrap func(http.Handler) http.Handler) (*httptest.Server, *server.Server, string) {
	t.Helper()
	data := t.TempDir()
	cfg := server.DefaultConfig(data)
	cfg.PartSize = testPartSize
	s, err := server.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var h http.Handler = s
	if wrap != nil {
		h = wrap(s)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(func() { ts.Close(); s.Close() })
	return ts, s, data
}

var linkLine = regexp.MustCompile(`Link: (\S+)`)
var deleteLine = regexp.MustCompile(`zarf delete (\S+) (\S+)`)

func sendFile(t *testing.T, ts *httptest.Server, content []byte, extra []string, secrets ...string) (link, deleteCode string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Sözleşme.pdf")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := testApp(t, secrets...)
	args := append([]string{"send", "--server", ts.URL, path}, extra...)
	if code := a.run(args); code != 0 {
		t.Fatalf("send failed: %s", errOut)
	}
	if strings.Contains(out.String(), ts.URL+"/d/") == false {
		t.Fatalf("no link: %s", out)
	}
	return linkLine.FindStringSubmatch(out.String())[1], deleteLine.FindStringSubmatch(out.String())[2]
}

func receive(t *testing.T, link string, secrets ...string) (string, int, string) {
	t.Helper()
	dir := t.TempDir()
	a, _, errOut := testApp(t, secrets...)
	code := a.run([]string{"receive", link, "-d", dir})
	return dir, code, errOut.String()
}

func TestSendAndReceiveThroughTheServer(t *testing.T) {
	ts, _, data := startServer(t, nil)
	content := bytes.Repeat([]byte("Zarf! "), 60000) // about 350 KB: several upload parts
	link, _ := sendFile(t, ts, content, nil)

	// The server stores only ciphertext: the content never appears on its disk.
	_ = filepath.Walk(data, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte("Zarf! Zarf!")) || bytes.Contains(b, []byte("Sözleşme")) {
				t.Errorf("%s holds plaintext", p)
			}
		}
		return nil
	})

	dir, code, errOut := receive(t, link)
	if code != 0 {
		t.Fatalf("receive: %s", errOut)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Sözleşme.pdf"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("content differs: %v", err)
	}
	// One download by default: the second try is told so.
	if _, code, errOut := receive(t, link); code != 1 || !strings.Contains(errOut, "expired or its downloads are used up") {
		t.Fatalf("second receive: %d %s", code, errOut)
	}
}

func TestAPasswordTypoDoesNotWasteTheDownload(t *testing.T) {
	ts, _, _ := startServer(t, nil)
	link, _ := sendFile(t, ts, []byte("gizli belge"), []string{"-p"}, "uzun bir şifre", "uzun bir şifre")
	dir, code, errOut := receive(t, link, "yanlış", "uzun bir şifre")
	if code != 0 || !strings.Contains(errOut, "Wrong password.") {
		t.Fatalf("receive: %d %s", code, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Sözleşme.pdf")); string(b) != "gizli belge" {
		t.Fatal("content differs")
	}
}

func TestALinkWithoutItsKeyOrWithAnotherKeyFails(t *testing.T) {
	ts, _, _ := startServer(t, nil)
	link, _ := sendFile(t, ts, []byte("x"), []string{"--downloads", "5"})
	noKey := link[:strings.Index(link, "#")]
	if _, code, errOut := receive(t, noKey); code != 1 || !strings.Contains(errOut, "has no key") {
		t.Fatalf("no key: %d %s", code, errOut)
	}
	other := noKey + "#" + b64.EncodeToString(bytes.Repeat([]byte{9}, 32))
	if _, code, errOut := receive(t, other); code != 1 || !strings.Contains(errOut, "does not match") {
		t.Fatalf("other key: %d %s", code, errOut)
	}
	// Neither attempt spent a download.
	if _, code, _ := receive(t, link); code != 0 {
		t.Fatal("the real link should still work")
	}
}

func TestDeletingASentFile(t *testing.T) {
	ts, _, _ := startServer(t, nil)
	link, code := sendFile(t, ts, []byte("x"), []string{"--downloads", "5"})
	a, out, errOut := testApp(t)
	if c := a.run([]string{"delete", link, code}); c != 0 || !strings.Contains(out.String(), "Deleted") {
		t.Fatalf("delete: %s", errOut)
	}
	if _, c, errOut := receive(t, link); c != 1 || !strings.Contains(errOut, "no such file") {
		t.Fatalf("after delete: %d %s", c, errOut)
	}
}

func TestUploadsRetryAndDownloadsResume(t *testing.T) {
	var failedPart, cutDownload atomic.Bool
	ts, _, _ := startServer(t, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The first part upload fails with a server error: the client must try again.
			if r.Method == "PUT" && failedPart.CompareAndSwap(false, true) {
				http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
				return
			}
			// The first download breaks off halfway: the client must continue with a Range.
			if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/content") && r.Header.Get("Range") == "" && cutDownload.CompareAndSwap(false, true) {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				body := rec.Body.Bytes()
				w.Header().Set("Content-Length", rec.Header().Get("Content-Length"))
				w.WriteHeader(rec.Code)
				w.Write(body[:len(body)/2])
				panic(http.ErrAbortHandler) // drop the connection mid-way
			}
			h.ServeHTTP(w, r)
		})
	})
	content := bytes.Repeat([]byte("0123456789"), 30000)
	link, _ := sendFile(t, ts, content, nil)
	if !failedPart.Load() {
		t.Fatal("the failing part was never hit")
	}
	dir, code, errOut := receive(t, link)
	if code != 0 {
		t.Fatalf("receive: %s", errOut)
	}
	if !cutDownload.Load() {
		t.Fatal("the download was never cut")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "Sözleşme.pdf")); !bytes.Equal(got, content) {
		t.Fatal("content differs after resuming")
	}
}

func TestATamperedServerIsCaught(t *testing.T) {
	ts, _, data := startServer(t, nil)
	link, _ := sendFile(t, ts, bytes.Repeat([]byte("a"), 200000), nil)
	// Change one byte of the stored envelope, as a malicious server could.
	_ = filepath.Walk(data, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Name() == "envelope" {
			b, _ := os.ReadFile(p)
			b[len(b)-50] ^= 1
			os.WriteFile(p, b, 0o600)
		}
		return nil
	})
	dir, code, errOut := receive(t, link)
	if code != 1 || !strings.Contains(errOut, "changed or damaged") {
		t.Fatalf("got %d %s", code, errOut)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a damaged file was saved: %v", entries)
	}
}

func TestPlainHTTPIsRefusedExceptOnThisMachine(t *testing.T) {
	if _, err := newClient("http://example.com", nil); err == nil {
		t.Fatal("plain http to another machine was accepted")
	}
	for _, ok := range []string{"https://zarf.example", "http://127.0.0.1:8080", "http://localhost:9"} {
		if _, err := newClient(ok, nil); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
}

func TestSendChecksItsOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(path, []byte("x"), 0o600)
	for _, args := range [][]string{
		{"send", "--expires", "30d", path},
		{"send", "--downloads", "3", path},
		{"send", "--server", "http://example.com", path},
	} {
		a, _, _ := testApp(t)
		if code := a.run(args); code == 0 {
			t.Fatalf("%v accepted", args)
		}
	}
	if _, _, _, err := parseLink("https://zarf.example/x/abc#key"); err == nil {
		t.Fatal("a link outside /d/ was accepted")
	}
}
