// Package server is the Zarf server: it stores and hands out envelopes it cannot read.
// The API is described in docs/PROTOCOL.md, section 6.
package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MB = 1024 * 1024
	GB = 1024 * MB
)

// Config holds the limits of an instance (PROTOCOL.md section 7). Self-hosters change them.
type Config struct {
	DataDir        string
	MaxSize        int64 // largest envelope
	PartSize       int64 // upload part size
	StorageQuota   int64 // all reserved envelopes together
	UploadsPerHour int   // new uploads per network address
	// TrustProxy takes the client address from X-Forwarded-For, but only when the request
	// comes from this machine (the reverse proxy in front). Anyone else could forge it.
	TrustProxy bool
	Now        func() time.Time
}

// DefaultConfig is the public instance: 1024 MB per file, 100 GB in total.
func DefaultConfig(dataDir string) Config {
	return Config{DataDir: dataDir, MaxSize: 1024 * MB, PartSize: 8 * MB, StorageQuota: 100 * GB, UploadsPerHour: 20, Now: time.Now}
}

// The choices a sender has (PROTOCOL.md section 7).
var (
	Expiries       = []time.Duration{time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}
	DownloadLimits = []int{1, 5, 10, 20}
)

type Server struct {
	cfg     Config
	store   *store
	blobs   *blobs
	uploads *limiter
	reports *limiter
	mux     *http.ServeMux

	completing sync.Mutex // assembling an upload happens once, even if "complete" arrives twice
}

func New(cfg Config) (*Server, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "blobs"), 0o700); err != nil {
		return nil, err
	}
	st, err := openStore(filepath.Join(cfg.DataDir, "zarf.db"))
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, store: st, blobs: &blobs{root: filepath.Join(cfg.DataDir, "blobs")},
		uploads: newLimiter(cfg.UploadsPerHour, time.Hour, cfg.Now), reports: newLimiter(10, time.Hour, cfg.Now), mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

func (s *Server) Close() error { return s.store.close() }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/v1/uploads", s.createUpload)
	s.mux.HandleFunc("PUT /api/v1/uploads/{id}/parts/{n}", s.putPart)
	s.mux.HandleFunc("GET /api/v1/uploads/{id}", s.uploadStatus)
	s.mux.HandleFunc("POST /api/v1/uploads/{id}/complete", s.completeUpload)
	s.downloadRoutes()
}

// --- small helpers ---

var b64 = base64.RawURLEncoding

func randomToken(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the platform random source failing is not something to carry on from
	}
	return b
}

// validID accepts only what newID makes: 16 bytes in base64url, safe as a folder name.
func validID(id string) bool {
	b, err := b64.DecodeString(id)
	return err == nil && len(b) == 16 && b64.EncodeToString(b) == id
}

func newID() string { return b64.EncodeToString(randomToken(16)) }

// bearer reads "Authorization: Bearer <token>"; nil when missing or malformed.
func bearer(r *http.Request) []byte {
	v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil
	}
	t, err := b64.DecodeString(strings.TrimSpace(v))
	if err != nil || len(t) != 32 {
		return nil
	}
	return t
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends a short code; details stay on the server (never a stack trace to the client).
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// readJSON takes a small body with only the expected fields.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

// clientAddress is used only for rate limits, in memory; it is never stored or logged.
func (s *Server) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.cfg.TrustProxy {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			// The proxy appends the address it saw; earlier entries come from the client and can be forged.
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				return strings.TrimSpace(parts[len(parts)-1])
			}
		}
	}
	return host
}

// limiter allows a number of events per window and key (fixed windows, in memory).
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	counts map[string]windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func newLimiter(max int, window time.Duration, now func() time.Time) *limiter {
	return &limiter{max: max, window: window, now: now, counts: map[string]windowCount{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.counts) > 100000 { // forget old windows rather than grow without end
		for k, c := range l.counts {
			if now.Sub(c.start) >= l.window {
				delete(l.counts, k)
			}
		}
	}
	c := l.counts[key]
	if now.Sub(c.start) >= l.window {
		c = windowCount{start: now}
	}
	if c.n >= l.max {
		return false
	}
	c.n++
	l.counts[key] = c
	return true
}
