package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const defaultServer = "https://zarf.miracdeprem.com"

// client talks to a Zarf server. It never sends a key: only envelopes, the download value
// and tokens the server handed out.
type client struct {
	base     *url.URL
	http     *http.Client
	retries  int
	progress io.Writer // nil: no progress line
}

func newClient(server string, progress io.Writer) (*client, error) {
	u, err := url.Parse(strings.TrimRight(server, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%q is not a server address", server)
	}
	// Plain HTTP would let anyone on the way see the tokens and the envelope; only a server
	// on this machine (for testing) may use it.
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("the server must use https: %s", server)
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	return &client{base: u, http: &http.Client{Transport: transport}, retries: 3, progress: progress}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// apiError is the server's answer when it said no.
type apiError struct {
	Status int
	Code   string
}

func (e *apiError) Error() string {
	switch e.Code {
	case "too_large":
		return "the file is too large: the limit is 1024 MB"
	case "gone":
		return "this link has expired or its downloads are used up"
	case "not_found":
		return "there is no such file (check the link)"
	case "wrong_key":
		return "the link's key does not match this file"
	case "too_many_requests":
		return "too many uploads from this address; try again later"
	case "storage_full":
		return "the server is full right now; try again later"
	}
	return fmt.Sprintf("the server said no (%d %s)", e.Status, e.Code)
}

func (c *client) url(path string) string { return c.base.String() + "/api/v1" + path }

// call sends a JSON request (body may be nil, []byte or a value) and decodes a JSON answer into out.
func (c *client) call(ctx context.Context, method, path, token string, body, out any) error {
	var r io.Reader
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		r, contentType = bytes.NewReader(b), "application/octet-stream"
	default:
		j, err := json.Marshal(b)
		if err != nil {
			return err
		}
		r = bytes.NewReader(j)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), r)
	if err != nil {
		return err
	}
	if r != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "zarf/"+version)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&e)
		return &apiError{Status: res.StatusCode, Code: e.Error}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	return nil
}

// retry runs f again after network failures and server errors; a clear "no" (4xx) is final.
func (c *client) retry(ctx context.Context, f func() error) error {
	var err error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 500 * time.Millisecond):
			}
		}
		if err = f(); err == nil {
			return nil
		}
		var api *apiError
		if errors.As(err, &api) && api.Status < 500 {
			return err
		}
	}
	return err
}

// --- progress ---

type progress struct {
	w           io.Writer
	label       string
	total, done int64
	last        time.Time
}

func (c *client) newProgress(label string, total int64) *progress {
	return &progress{w: c.progress, label: label, total: total}
}

func (p *progress) add(n int64) {
	p.done += n
	if p.w == nil || (time.Since(p.last) < 200*time.Millisecond && p.done < p.total) {
		return
	}
	p.last = time.Now()
	pct := 100
	if p.total > 0 {
		pct = int(p.done * 100 / p.total)
	}
	fmt.Fprintf(p.w, "\r%s %3d%%  %s / %s ", p.label, pct, mb(p.done), mb(p.total))
	if p.done >= p.total {
		fmt.Fprintln(p.w)
	}
}

func mb(n int64) string { return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MB" }

// terminalProgress shows progress only when stderr is a terminal (not in scripts or logs).
func terminalProgress() io.Writer {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		return os.Stderr
	}
	return nil
}

// --- resumable download ---

// resumingBody reads the envelope; when the connection breaks, it asks for the rest with a
// Range request on the same ticket, so a long download does not start over.
type resumingBody struct {
	ctx      context.Context
	c        *client
	path     string
	ticket   string
	size     int64
	offset   int64
	body     io.ReadCloser
	failures int
	progress *progress
}

func (r *resumingBody) open() error {
	req, err := http.NewRequestWithContext(r.ctx, "GET", r.c.url(r.path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.ticket)
	req.Header.Set("User-Agent", "zarf/"+version)
	if r.offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", r.offset))
	}
	res, err := r.c.http.Do(req)
	if err != nil {
		return err
	}
	want := http.StatusOK
	if r.offset > 0 {
		want = http.StatusPartialContent
	}
	if res.StatusCode != want {
		res.Body.Close()
		return &apiError{Status: res.StatusCode, Code: "download_failed"}
	}
	r.body = res.Body
	return nil
}

func (r *resumingBody) Read(p []byte) (int, error) {
	for {
		if r.offset >= r.size {
			return 0, io.EOF
		}
		if r.body == nil {
			if err := r.open(); err != nil {
				if r.failures++; r.failures > r.c.retries {
					return 0, err
				}
				time.Sleep(time.Duration(r.failures) * 500 * time.Millisecond)
				continue
			}
		}
		n, err := r.body.Read(p)
		r.offset += int64(n)
		r.progress.add(int64(n))
		if n > 0 {
			return n, nil
		}
		if err == io.EOF && r.offset >= r.size {
			return 0, io.EOF
		}
		// Cut short: close and continue from where we are.
		r.body.Close()
		r.body = nil
		if r.failures++; r.failures > r.c.retries {
			if err == nil || err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
	}
}

func (r *resumingBody) Close() error {
	if r.body != nil {
		return r.body.Close()
	}
	return nil
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
