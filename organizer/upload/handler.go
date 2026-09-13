package upload

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/autoget-project/autoget/protocol"
)

// Handler serves the HTTP endpoints for resumable file uploads.
type Handler struct {
	store *Store
}

// NewHandler creates a Handler with the specified Store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes registers the 5 resumable upload endpoints on the provided ServeMux.
func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /v1/upload/init", h.HandleInit)
	mux.HandleFunc("GET /v1/upload/{id}", h.HandleGet)
	mux.HandleFunc("HEAD /v1/upload/{id}", h.HandleGet)
	mux.HandleFunc("PATCH /v1/upload/{id}", h.HandlePatch)
	mux.HandleFunc("POST /v1/upload/{id}/finish", h.HandleFinish)
	mux.HandleFunc("DELETE /v1/upload/{id}", h.HandleCancel)
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(protocol.UploadError{Error: message})
}

// HandleInit handles POST /v1/upload/init.
func (h *Handler) HandleInit(w http.ResponseWriter, r *http.Request) {
	var req protocol.UploadInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, isNew, err := h.store.Init(&req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidParam):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrSizeMismatch):
			writeJSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrStorageFull):
			writeJSONError(w, http.StatusInsufficientStorage, err.Error())
		default:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	statusCode := http.StatusOK
	if isNew {
		statusCode = http.StatusCreated
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleGet handles GET and HEAD /v1/upload/{id}.
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	uploadID := r.PathValue("id")
	if uploadID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing upload id")
		return
	}

	resp, err := h.store.Get(uploadID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrMetaCorrupt) {
			writeJSONError(w, http.StatusNotFound, "upload not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set(protocol.UploadOffsetHeader, strconv.FormatInt(resp.Offset, 10))
	w.Header().Set(protocol.UploadLengthHeader, strconv.FormatInt(resp.TotalSize, 10))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// HandlePatch handles PATCH /v1/upload/{id}.
func (h *Handler) HandlePatch(w http.ResponseWriter, r *http.Request) {
	uploadID := r.PathValue("id")
	if uploadID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing upload id")
		return
	}

	offsetStr := r.Header.Get(protocol.UploadOffsetHeader)
	if offsetStr == "" {
		writeJSONError(w, http.StatusBadRequest, "missing Upload-Offset header")
		return
	}
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid Upload-Offset header")
		return
	}

	contentLength := r.ContentLength
	if contentLength < 0 {
		writeJSONError(w, http.StatusLengthRequired, "Content-Length header required")
		return
	}
	if contentLength > protocol.MaxChunkSize {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("chunk exceeds maximum allowed size (%d bytes)", protocol.MaxChunkSize))
		return
	}

	// Dynamic ReadDeadline extension (Review recommendation 2.1)
	// Base 60s + 1s per 1MiB transferred
	rc := http.NewResponseController(w)
	extraSeconds := contentLength / (1024 * 1024)
	_ = rc.SetReadDeadline(time.Now().Add(60*time.Second + time.Duration(extraSeconds)*time.Second))

	checksum := r.Header.Get(protocol.UploadChecksumHeader)
	newOffset, err := h.store.AppendChunk(uploadID, offset, r.Body, contentLength, checksum)
	if err != nil {
		var conflictErr ErrOffsetConflict
		if errors.As(err, &conflictErr) {
			w.Header().Set(protocol.UploadOffsetHeader, strconv.FormatInt(conflictErr.LastOffset, 10))
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "upload not found")
			return
		}
		if errors.Is(err, ErrChecksumFailed) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set(protocol.UploadOffsetHeader, strconv.FormatInt(newOffset, 10))
	w.WriteHeader(http.StatusNoContent)
}

// HandleFinish handles POST /v1/upload/{id}/finish.
func (h *Handler) HandleFinish(w http.ResponseWriter, r *http.Request) {
	uploadID := r.PathValue("id")
	if uploadID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing upload id")
		return
	}

	var finishReq protocol.UploadFinishRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&finishReq)
	}

	err := h.store.Finish(uploadID, &finishReq)
	if err != nil {
		var conflictErr ErrOffsetConflict
		if errors.As(err, &conflictErr) {
			w.Header().Set(protocol.UploadOffsetHeader, strconv.FormatInt(conflictErr.LastOffset, 10))
			writeJSONError(w, http.StatusConflict, fmt.Sprintf("upload not complete: %d bytes written", conflictErr.LastOffset))
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "upload not found")
			return
		}
		if errors.Is(err, ErrChecksumFailed) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(protocol.UploadFinishResponse{Status: protocol.StatusCompleted})
}

// HandleCancel handles DELETE /v1/upload/{id}.
func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	uploadID := r.PathValue("id")
	if uploadID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing upload id")
		return
	}

	_ = h.store.Cancel(uploadID)
	w.WriteHeader(http.StatusNoContent)
}
