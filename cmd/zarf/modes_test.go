package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer lets a test read what a command running in another goroutine has printed.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func appWithConfig(t *testing.T, config, input string) (*app, *lockedBuffer, *lockedBuffer) {
	t.Helper()
	out, errOut := &lockedBuffer{}, &lockedBuffer{}
	a := &app{in: bufio.NewReader(strings.NewReader(input)), out: out, errOut: errOut}
	a.secret = func(string) (string, error) { t.Fatal("asked for a secret"); return "", nil }
	a.configDir = func() (string, error) { return config, nil }
	return a, out, errOut
}

var requestLinkLine = regexp.MustCompile(`Request link: (\S+)`)

func TestRequestsFromTheCommandLine(t *testing.T) {
	ts, _, _ := startServer(t, nil)
	config := t.TempDir()
	a, out, errOut := appWithConfig(t, config, "")
	if code := a.run([]string{"request", "new", "--server", ts.URL, "--note", "Vergi levhası", "--uploads", "5"}); code != 0 {
		t.Fatalf("request new: %s", errOut)
	}
	link := requestLinkLine.FindStringSubmatch(out.String())[1]
	if !strings.Contains(out.String(), "Check words: ") {
		t.Error("no check words")
	}
	saved, _ := filepath.Glob(filepath.Join(config, "zarf", "requests", "*.json"))
	if len(saved) != 1 {
		t.Fatalf("saved requests: %v", saved)
	}

	// Someone answers the request.
	content := bytes.Repeat([]byte("levha "), 50_000)
	src := filepath.Join(t.TempDir(), "vergi levhası.pdf")
	os.WriteFile(src, content, 0o600)
	b, bout, berr := appWithConfig(t, t.TempDir(), "")
	if code := b.run([]string{"send", "--to", link, src}); code != 0 {
		t.Fatalf("send --to: %s", berr)
	}
	if !strings.Contains(bout.String(), "Asked for: Vergi levhası") {
		t.Errorf("uploader output: %s", bout)
	}

	a, out, errOut = appWithConfig(t, config, "")
	if code := a.run([]string{"request", "list"}); code != 0 || !strings.Contains(out.String(), "1 arrived, 1 new") {
		t.Fatalf("list: %s %s", out, errOut)
	}
	dir := t.TempDir()
	rid := strings.SplitN(strings.TrimPrefix(link[strings.Index(link, "/r/")+3:], ""), "#", 2)[0]
	a, out, errOut = appWithConfig(t, config, "")
	if code := a.run([]string{"request", "get", rid[:6], "-d", dir}); code != 0 {
		t.Fatalf("get: %s", errOut)
	}
	got, err := os.ReadFile(filepath.Join(dir, "vergi levhası.pdf"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("received file: %v", err)
	}
	a, out, _ = appWithConfig(t, config, "")
	if a.run([]string{"request", "get", rid, "-d", dir}); !strings.Contains(out.String(), "Nothing new") {
		t.Errorf("second get: %s", out)
	}

	a, out, errOut = appWithConfig(t, config, "")
	if code := a.run([]string{"request", "close", rid}); code != 0 {
		t.Fatalf("close: %s", errOut)
	}
	b, _, berr = appWithConfig(t, t.TempDir(), "")
	if code := b.run([]string{"send", "--to", link, src}); code == 0 || !strings.Contains(berr.String(), "no request") {
		t.Errorf("upload to a closed request: %d %s", code, berr)
	}
}

var codeLine = regexp.MustCompile(`Code: (\S+)`)

// sendWithCode starts "zarf send --code" and returns its code once printed.
func sendWithCode(t *testing.T, server string, content []byte) (string, chan int, *lockedBuffer) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Sunum.pptx")
	os.WriteFile(src, content, 0o600)
	a, out, errOut := appWithConfig(t, t.TempDir(), "")
	done := make(chan int, 1)
	go func() { done <- a.run([]string{"send", "--code", "--server", server, src}) }()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if m := codeLine.FindStringSubmatch(out.String()); m != nil {
			return m[1], done, errOut
		}
	}
	t.Fatalf("no code: %s %s", out, errOut)
	return "", nil, nil
}

func TestLiveFromTheCommandLine(t *testing.T) {
	ts, _, _ := startServer(t, nil)
	content := bytes.Repeat([]byte("canlı "), 200_000)
	code, sent, sendErr := sendWithCode(t, ts.URL, content)

	dir := t.TempDir()
	r, out, errOut := appWithConfig(t, t.TempDir(), "e\n") // answered in Turkish: "evet"
	if c := r.run([]string{"receive", code, "--server", ts.URL, "-d", dir}); c != 0 {
		t.Fatalf("receive: %s", errOut)
	}
	if !strings.Contains(out.String(), "Incoming: Sunum.pptx") {
		t.Errorf("no offer shown: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Sunum.pptx"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("received: %v", err)
	}
	if c := <-sent; c != 0 {
		t.Fatalf("send: %s", sendErr)
	}
}

func TestLiveWrongCodeAndDecline(t *testing.T) {
	ts, _, _ := startServer(t, nil)
	code, sent, sendErr := sendWithCode(t, ts.URL, []byte("gizli"))
	parts := strings.Split(code, "-")
	wrong := parts[0] + "-" + parts[1] + "-" + map[bool]string{true: "pear", false: "apple"}[parts[2] == "apple"]
	r, _, errOut := appWithConfig(t, t.TempDir(), "")
	if c := r.run([]string{"receive", wrong, "--server", ts.URL, "-y", "-d", t.TempDir()}); c == 0 || !strings.Contains(errOut.String(), "doesn't match") {
		t.Errorf("wrong code: %d %s", c, errOut)
	}
	if c := <-sent; c == 0 || !strings.Contains(sendErr.String(), "doesn't match") {
		t.Errorf("sender after a wrong code: %d %s", c, sendErr)
	}

	code, sent, sendErr = sendWithCode(t, ts.URL, []byte("istenmeyen"))
	dir := t.TempDir()
	r, out, _ := appWithConfig(t, t.TempDir(), "n\n")
	if c := r.run([]string{"receive", code, "--server", ts.URL, "-d", dir}); c != 0 || !strings.Contains(out.String(), "Declined") {
		t.Errorf("decline: %d %s", c, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a declined file was saved: %v", entries)
	}
	if c := <-sent; c == 0 || !strings.Contains(sendErr.String(), "declined") {
		t.Errorf("sender after decline: %d %s", c, sendErr)
	}
}
