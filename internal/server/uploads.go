package server

import (
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// The smallest possible envelope: header, one block of metadata and one empty chunk.
const minEnvelope = 75 + 1024 + 16 + 16

type createUploadRequest struct {
	Size         int64  `json:"size"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	MaxDownloads int    `json:"max_downloads"`
	DLAuthHash   string `json:"dl_auth_hash"`
}

type createUploadResponse struct {
	ID          string    `json:"id"`
	UploadToken string    `json:"upload_token"`
	DeleteToken string    `json:"delete_token"`
	PartSize    int64     `json:"part_size"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	var req createUploadRequest
	if !readJSON(w, r, &req) {
		return
	}
	expiry := time.Duration(req.ExpiresIn) * time.Second
	dlAuthHash, err := b64.DecodeString(req.DLAuthHash)
	switch {
	case req.Size > s.cfg.MaxSize:
		writeError(w, http.StatusRequestEntityTooLarge, "too_large")
		return
	case req.Size < minEnvelope:
		writeError(w, http.StatusBadRequest, "invalid_size")
		return
	case !slices.Contains(Expiries, expiry):
		writeError(w, http.StatusBadRequest, "invalid_expiry")
		return
	case !slices.Contains(DownloadLimits, req.MaxDownloads):
		writeError(w, http.StatusBadRequest, "invalid_download_limit")
		return
	case err != nil || len(dlAuthHash) != 32:
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !s.uploads.allow(s.clientAddress(r)) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}
	reserved, err := s.store.reservedBytes(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if reserved+req.Size > s.cfg.StorageQuota {
		writeError(w, http.StatusInsufficientStorage, "storage_full")
		return
	}

	now := s.cfg.Now()
	uploadToken, deleteToken := randomToken(32), randomToken(32)
	f := &fileRow{
		ID: newID(), Size: req.Size, PartSize: s.cfg.PartSize,
		UploadTokenHash: hashToken(uploadToken), DeleteTokenHash: hashToken(deleteToken), DLAuthHash: dlAuthHash,
		MaxDownloads: req.MaxDownloads, CreatedAt: now, ExpiresAt: now.Add(expiry),
	}
	if err := s.store.insertFile(r.Context(), f); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createUploadResponse{
		ID: f.ID, UploadToken: b64.EncodeToString(uploadToken), DeleteToken: b64.EncodeToString(deleteToken),
		PartSize: f.PartSize, ExpiresAt: f.ExpiresAt.UTC(),
	})
}

// uploading finds an upload still in progress whose upload token the request carries.
// Unknown ids and wrong tokens get the same answer, so neither can be told apart.
func (s *Server) uploading(w http.ResponseWriter, r *http.Request) (*fileRow, bool) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	f, err := s.store.file(r.Context(), id)
	if errors.Is(err, errNotFound) || (err == nil && !tokenMatches(bearer(r), f.UploadTokenHash)) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.fail(w, err)
		return nil, false
	}
	if !s.cfg.Now().Before(f.ExpiresAt) {
		writeError(w, http.StatusGone, "expired")
		return nil, false
	}
	return f, true
}

func (s *Server) putPart(w http.ResponseWriter, r *http.Request) {
	f, ok := s.uploading(w, r)
	if !ok {
		return
	}
	if f.Complete {
		writeError(w, http.StatusConflict, "already_complete")
		return
	}
	n, err := strconv.ParseInt(r.PathValue("n"), 10, 64)
	if err != nil || n < 0 || n >= f.parts() {
		writeError(w, http.StatusBadRequest, "invalid_part")
		return
	}
	want := f.partLen(n)
	body := http.MaxBytesReader(w, r.Body, want)
	if err := s.blobs.writePart(f.ID, n, want, body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.Is(err, errWrongPartSize) || errors.As(err, &tooBig) {
			writeError(w, http.StatusBadRequest, "wrong_part_size")
			return
		}
		s.fail(w, err)
		return
	}
	if err := s.store.recordPart(r.Context(), f.ID, n, want); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type uploadStatusResponse struct {
	Parts    int64   `json:"parts"`
	Received []int64 `json:"received"`
	Complete bool    `json:"complete"`
}

// uploadStatus tells an interrupted sender which parts arrived, so it sends only the rest.
func (s *Server) uploadStatus(w http.ResponseWriter, r *http.Request) {
	f, ok := s.uploading(w, r)
	if !ok {
		return
	}
	received, err := s.store.receivedParts(r.Context(), f.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, uploadStatusResponse{Parts: f.parts(), Received: received, Complete: f.Complete})
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	s.completing.Lock()
	defer s.completing.Unlock()
	f, ok := s.uploading(w, r) // read again under the lock: another request may have just completed it
	if !ok {
		return
	}
	if !f.Complete {
		received, err := s.store.receivedParts(r.Context(), f.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		if int64(len(received)) != f.parts() {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "missing_parts", "received": received, "parts": f.parts()})
			return
		}
		if err := s.blobs.assemble(f.ID, f.parts()); err != nil {
			s.fail(w, err)
			return
		}
		if err := s.store.markComplete(r.Context(), f.ID); err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": f.ID, "expires_at": f.ExpiresAt.UTC()})
}

// fail logs the real error for the operator and gives the client only a short code.
func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("zarf: internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal")
}
