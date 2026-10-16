package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	ticketLife   = time.Hour      // long enough to finish (or resume) a large download
	staleUpload  = 24 * time.Hour // an upload never completed is dropped after this
	maxReasonLen = 500
)

func (s *Server) downloadRoutes() {
	s.mux.HandleFunc("GET /api/v1/files/{id}", s.fileInfo)
	s.mux.HandleFunc("POST /api/v1/files/{id}/download", s.startDownload)
	s.mux.HandleFunc("GET /api/v1/files/{id}/content", s.content)
	s.mux.HandleFunc("DELETE /api/v1/files/{id}", s.deleteFile)
	s.mux.HandleFunc("POST /api/v1/files/{id}/report", s.report)
}

// available finds a finished file that can still be downloaded: 404 when it never existed,
// 410 when it expired or was used up, so the page can say which.
func (s *Server) available(w http.ResponseWriter, r *http.Request) (*fileRow, bool) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	f, err := s.store.file(r.Context(), id)
	if errors.Is(err, errNotFound) || (err == nil && !f.Complete) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.fail(w, err)
		return nil, false
	}
	if !s.cfg.Now().Before(f.ExpiresAt) || f.Downloads >= f.MaxDownloads {
		writeError(w, http.StatusGone, "gone")
		return nil, false
	}
	return f, true
}

type fileInfoResponse struct {
	Size          int64     `json:"size"`
	ExpiresAt     time.Time `json:"expires_at"`
	DownloadsLeft int       `json:"downloads_left"`
}

// fileInfo tells the receiving page what to expect. It holds no secret and spends nothing.
func (s *Server) fileInfo(w http.ResponseWriter, r *http.Request) {
	f, ok := s.available(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, fileInfoResponse{Size: f.Size, ExpiresAt: f.ExpiresAt.UTC(), DownloadsLeft: f.MaxDownloads - f.Downloads})
}

// startDownload spends one download and hands out a ticket for the content. The download
// value comes from the link key, so knowing only the id is not enough to use up a file.
func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	f, ok := s.available(w, r)
	if !ok {
		return
	}
	var req struct {
		DLAuth string `json:"dl_auth"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	auth, err := b64.DecodeString(req.DLAuth)
	if err != nil || !tokenMatches(auth, f.DLAuthHash) {
		writeError(w, http.StatusForbidden, "wrong_key")
		return
	}
	now := s.cfg.Now()
	spent, err := s.store.spendDownload(r.Context(), f.ID, now)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !spent {
		writeError(w, http.StatusGone, "gone") // someone else took the last download just now
		return
	}
	ticket := randomToken(32)
	if err := s.store.insertTicket(r.Context(), hashToken(ticket), f.ID, now.Add(ticketLife)); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": b64.EncodeToString(ticket), "size": f.Size})
}

// content sends the envelope. The ticket travels in the Authorization header, never in the
// address, so it does not end up in logs. Range requests let a broken download resume.
func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ticket := bearer(r)
	if !validID(id) || ticket == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	ok, err := s.store.ticketValid(r.Context(), hashToken(ticket), id, s.cfg.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	f, err := os.Open(s.blobs.envelopePath(id))
	if err != nil {
		s.fail(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="zarf"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	f, err := s.store.file(r.Context(), id)
	if errors.Is(err, errNotFound) || (err == nil && !tokenMatches(bearer(r), f.DeleteTokenHash)) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.remove(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// report records an abuse report for the operator. Files are encrypted end to end, so the
// operator cannot look inside; they decide from the report and can remove the file.
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.reports.allow(s.clientAddress(r)) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}
	exists, err := s.store.exists(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if exists {
		if err := s.store.addReport(r.Context(), id, cleanReason(req.Reason), s.cfg.Now()); err != nil {
			s.fail(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent) // the same answer either way
}

// cleanReason keeps a report to one plain line of limited length.
func cleanReason(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxReasonLen {
		s = string(r[:maxReasonLen])
	}
	return s
}

// remove deletes a file's row and its bytes.
func (s *Server) remove(ctx context.Context, id string) error {
	if err := s.store.deleteFile(ctx, id); err != nil {
		return err
	}
	return s.blobs.remove(id)
}

// Cleanup deletes expired, abandoned and used-up files, then bytes on disk that no longer
// belong to any file. The server runs it every few minutes; it is safe to run at any time.
func (s *Server) Cleanup(ctx context.Context) (int, error) {
	now := s.cfg.Now()
	ids, err := s.store.finished(ctx, now, staleUpload)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.remove(ctx, id); err != nil {
			return 0, err
		}
	}
	if err := s.store.dropExpiredTickets(ctx, now); err != nil {
		return len(ids), err
	}
	entries, err := os.ReadDir(s.blobs.root)
	if err != nil {
		return len(ids), err
	}
	for _, e := range entries {
		if !validID(e.Name()) {
			continue
		}
		if ok, err := s.store.exists(ctx, e.Name()); err == nil && !ok {
			if err := s.blobs.remove(e.Name()); err != nil {
				log.Printf("zarf: could not remove leftover %s: %v", e.Name(), err)
			}
		}
	}
	return len(ids), nil
}

// Remove deletes a file at the operator's request (after a report, for example).
func (s *Server) Remove(ctx context.Context, id string) error {
	if !validID(id) {
		return errors.New("not a file id")
	}
	return s.remove(ctx, id)
}

// Report is one abuse report, for the operator's command line.
type Report struct {
	FileID    string
	Reason    string
	CreatedAt time.Time
}

func (s *Server) Reports(ctx context.Context) ([]Report, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT file_id, reason, created_at FROM reports ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Report
	for rows.Next() {
		var r Report
		var at int64
		if err := rows.Scan(&r.FileID, &r.Reason, &at); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}
