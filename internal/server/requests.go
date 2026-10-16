package server

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	maxLabelLen = 100
	// Each upload to a request can be downloaded this many times, all by the requester:
	// enough to retry, or to open it on a second visit.
	requestFileDownloads = 20
)

func (s *Server) requestRoutes() {
	s.mux.HandleFunc("POST /api/v1/requests", s.createRequest)
	s.mux.HandleFunc("GET /api/v1/requests/{rid}", s.requestInfo)
	s.mux.HandleFunc("DELETE /api/v1/requests/{rid}", s.deleteRequest)
	s.mux.HandleFunc("POST /api/v1/requests/{rid}/uploads", s.createRequestUpload)
	s.mux.HandleFunc("POST /api/v1/requests/{rid}/files/{id}/download", s.startRequestDownload)
}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label      string `json:"label"`
		ExpiresIn  int64  `json:"expires_in"`
		MaxUploads int    `json:"max_uploads"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	expiry := time.Duration(req.ExpiresIn) * time.Second
	switch {
	case !slices.Contains(Expiries, expiry):
		writeError(w, http.StatusBadRequest, "invalid_expiry")
		return
	case !slices.Contains(DownloadLimits, req.MaxUploads):
		writeError(w, http.StatusBadRequest, "invalid_upload_limit")
		return
	}
	if !s.uploads.allow(s.clientAddress(r)) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}
	now := s.cfg.Now().Truncate(time.Second)
	token := randomToken(32)
	q := &requestRow{
		ID: newID(), Label: oneLine(req.Label, maxLabelLen), OwnerTokenHash: hashToken(token),
		MaxUploads: req.MaxUploads, CreatedAt: now, ExpiresAt: now.Add(expiry),
	}
	if err := s.store.insertRequest(r.Context(), q); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rid": q.ID, "owner_token": b64.EncodeToString(token), "expires_at": q.ExpiresAt.UTC()})
}

// openRequest finds a request that still takes uploads: 404 when it never existed, 410 when
// it expired, so the page can say which.
func (s *Server) openRequest(w http.ResponseWriter, r *http.Request) (*requestRow, bool) {
	rid := r.PathValue("rid")
	if !validID(rid) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	q, err := s.store.request(r.Context(), rid)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.fail(w, err)
		return nil, false
	}
	if !s.cfg.Now().Before(q.ExpiresAt) {
		writeError(w, http.StatusGone, "gone")
		return nil, false
	}
	return q, true
}

// ownedRequest is openRequest for the requester: a wrong owner token gets the same answer as
// an unknown request.
func (s *Server) ownedRequest(w http.ResponseWriter, r *http.Request) (*requestRow, bool) {
	q, ok := s.openRequest(w, r)
	if ok && !tokenMatches(bearer(r), q.OwnerTokenHash) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	return q, ok
}

// requestInfo tells the uploader's page what is asked for and whether there is room. With the
// owner token it also lists what has arrived.
func (s *Server) requestInfo(w http.ResponseWriter, r *http.Request) {
	open := s.openRequest
	if r.Header.Get("Authorization") != "" {
		open = s.ownedRequest
	}
	q, ok := open(w, r)
	if !ok {
		return
	}
	used, err := s.store.requestUploads(r.Context(), q.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	res := map[string]any{"label": q.Label, "expires_at": q.ExpiresAt.UTC(), "uploads_left": max(0, q.MaxUploads-used)}
	if r.Header.Get("Authorization") != "" {
		files, err := s.store.requestFiles(r.Context(), q.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		res["files"] = files
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) createRequestUpload(w http.ResponseWriter, r *http.Request) {
	q, ok := s.openRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Size int64 `json:"size"`
	}
	if !readJSON(w, r, &req) || !s.sizeAllowed(w, req.Size) || !s.admit(w, r, req.Size) {
		return
	}
	// No download value: only the requester's owner token can fetch these files.
	f, res := s.newFile(req.Size, []byte{}, requestFileDownloads, s.cfg.Now().Truncate(time.Second), q.ExpiresAt)
	f.RequestID = q.ID
	added, err := s.store.insertRequestFile(r.Context(), f, q.MaxUploads)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !added {
		writeError(w, http.StatusGone, "request_full")
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) startRequestDownload(w http.ResponseWriter, r *http.Request) {
	q, ok := s.ownedRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	f, err := s.store.file(r.Context(), id)
	if errors.Is(err, errNotFound) || (err == nil && (f.RequestID != q.ID || !f.Complete)) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.spendAndTicket(w, r, f)
}

func (s *Server) deleteRequest(w http.ResponseWriter, r *http.Request) {
	q, ok := s.ownedRequest(w, r)
	if !ok {
		return
	}
	ids, err := s.store.requestFileIDs(r.Context(), q.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, id := range ids {
		if err := s.remove(r.Context(), id); err != nil {
			s.fail(w, err)
			return
		}
	}
	if err := s.store.deleteRequest(r.Context(), q.ID); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// oneLine keeps a text from a user to one plain line of limited length: no control or
// invisible formatting characters, no runs of spaces.
func oneLine(s string, maxRunes int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxRunes {
		s = strings.TrimSpace(string(r[:maxRunes]))
	}
	return s
}
