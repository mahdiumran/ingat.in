package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ingatin/backend/internal/attachments"
	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   F21 — Lampiran pendukung
   --------------------------------------------------------------------------- */

// handleListAttachments mengembalikan daftar lampiran sebuah work item.
//
// GET /api/items/{id}/attachments
func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadItemForAttachment(w, r)
	if !ok {
		return
	}
	list, err := s.attachments.List(r.Context(), item.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"attachments": list,
		"max_bytes":   s.attachments.MaxBytes(),
	})
}

// handleUploadAttachment menerima unggahan satu berkas (multipart, field "file").
//
// POST /api/items/{id}/attachments
func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadItemForAttachment(w, r)
	if !ok {
		return
	}
	if !canWriteRole(userFrom(r)) {
		writeErr(w, http.StatusForbidden, "viewer tidak dapat mengunggah lampiran")
		return
	}
	// Batasi ukuran body sejak awal agar tidak membaca berkas raksasa.
	r.Body = http.MaxBytesReader(w, r.Body, s.attachments.MaxBytes()+1<<20)

	file, header, err := r.FormFile("file")
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("berkas melebihi batas %d MB", s.attachments.MaxBytes()/1024/1024))
			return
		}
		writeErr(w, http.StatusBadRequest, "berkas tidak ditemukan pada field 'file'")
		return
	}
	defer file.Close()

	// Tentukan MIME: pakai nilai dari klien, fallback dari ekstensi.
	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = detectMimeByName(header.Filename)
	}

	res, err := s.attachments.Save(r.Context(), item.ID, header.Filename, mime, currentUsername(r), file)
	if err != nil {
		switch {
		case errors.Is(err, attachments.ErrTooLarge):
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("berkas melebihi batas %d MB", s.attachments.MaxBytes()/1024/1024))
		case errors.Is(err, attachments.ErrTypeNotAllowed):
			writeErr(w, http.StatusUnsupportedMediaType, err.Error())
		case errors.Is(err, attachments.ErrEmptyFile):
			writeErr(w, http.StatusBadRequest, "berkas kosong")
		default:
			writeInternalError(w, err)
		}
		return
	}

	_ = s.store.AppendEventSimple(r.Context(), item.ID, "attachment_added", map[string]any{
		"filename": res.Attachment.Filename,
		"size":     res.Attachment.SizeBytes,
	})
	s.audit(r, currentUsername(r), "attachment.upload", "work_item", item.ID.String(),
		map[string]any{"filename": res.Attachment.Filename, "size": res.Attachment.SizeBytes}, true)

	writeJSON(w, http.StatusCreated, res.Attachment)
}

// handleDownloadAttachment mengunduh berkas lampiran (ber-auth).
//
// GET /api/attachments/{id}
func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	att, f, err := s.attachments.Open(r.Context(), id)
	if err != nil {
		if errors.Is(err, attachments.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "lampiran tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	defer f.Close()

	if att.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(att.SizeBytes, 10))
	}
	// Selalu paksa unduh; Content-Type dari DB (bukan tebakan) + nosniff.
	w.Header().Set("Content-Type", safeMime(att.Mime))
	w.Header().Set("Content-Disposition", contentDisposition(att.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = copyStream(w, f)
}

// handleDeleteAttachment menghapus lampiran (pembuat/owner/admin).
//
// DELETE /api/attachments/{id}
func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	att, err := s.store.GetAttachment(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "lampiran tidak ditemukan")
		return
	}
	item, err := s.store.GetWorkItem(r.Context(), att.WorkItemID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// Pembuat/owner/admin; pengunggah juga boleh menghapus berkasnya sendiri.
	user := userFrom(r)
	allowed := canManageItem(user, item) ||
		(user != nil && strings.EqualFold(user.Username, att.UploadedBy))
	if !allowed {
		writeErr(w, http.StatusForbidden, "hanya pembuat/owner/admin (atau pengunggah) yang dapat menghapus")
		return
	}

	if err := s.attachments.Remove(r.Context(), id); err != nil {
		if errors.Is(err, attachments.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "lampiran tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, currentUsername(r), "attachment.delete", "work_item", att.WorkItemID.String(),
		map[string]any{"filename": att.Filename}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// loadItemForAttachment memuat work item dari path {id}.
func (s *Server) loadItemForAttachment(w http.ResponseWriter, r *http.Request) (*models.WorkItem, bool) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return nil, false
	}
	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return nil, false
	}
	return item, true
}

// uuidParam mengurai path param UUID.
func uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeErr(w, http.StatusBadRequest, name+" tidak valid")
		return uuid.UUID{}, false
	}
	return id, true
}

// canWriteRole melaporkan apakah user boleh menulis (bukan viewer).
func canWriteRole(u *models.User) bool {
	return u != nil && u.Role != models.RoleViewer
}

// copyStream menyalin reader ke writer (dipisah agar mudah diuji).
func copyStream(w io.Writer, r io.Reader) (int64, error) { return io.Copy(w, r) }

// safeMime membatasi Content-Type agar selalu aman.
func safeMime(mime string) string {
	mime = strings.TrimSpace(mime)
	if mime == "" {
		return "application/octet-stream"
	}
	return mime
}

// contentDisposition menyusun header unduhan, membuang karakter berbahaya.
func contentDisposition(filename string) string {
	safe := strings.ReplaceAll(filename, `"`, "")
	safe = strings.ReplaceAll(safe, "\r", "")
	safe = strings.ReplaceAll(safe, "\n", "")
	return fmt.Sprintf(`attachment; filename="%s"`, safe)
}

// detectMimeByName menebak MIME dari ekstensi (fallback bila header kosong).
func detectMimeByName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain"
	case strings.HasSuffix(lower, ".csv"):
		return "text/csv"
	case strings.HasSuffix(lower, ".zip"):
		return "application/zip"
	case strings.HasSuffix(lower, ".gz"):
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}
