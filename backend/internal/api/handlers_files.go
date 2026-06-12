package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/forms"
	"github.com/gresbase/gresbase/internal/storage"
)

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

func (h *Handlers) FileDownload(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")
	filename := chi.URLParam(r, "filename")
	path := fmt.Sprintf("%s/%s/%s", collName, recordID, filename)

	// A short-lived file token can stand in for the Authorization header so
	// protected files work in <img>/<video> tags. The token carries the
	// requester identity; rule checks below see it as a normal auth context.
	if tokenParam := r.URL.Query().Get("token"); tokenParam != "" && NewRequestInfo(r).AdminID == "" {
		if claims, err := h.app.Auth().ValidateFileToken(tokenParam); err == nil {
			ctx := r.Context()
			if claims.IsAdmin {
				ctx = context.WithValue(ctx, contextKeyAdminID, claims.AdminID)
				ctx = context.WithValue(ctx, contextKeyAdminRole, claims.Role)
				ctx = context.WithValue(ctx, contextKeyTenantID, claims.TenantID)
			} else if claims.RecordID != "" {
				ctx = context.WithValue(ctx, "record_id", claims.RecordID)
				ctx = context.WithValue(ctx, "collection_id", claims.CollectionID)
				ctx = context.WithValue(ctx, "email", claims.Email)
				ctx = context.WithValue(ctx, "verified", claims.Verified)
				ctx = context.WithValue(ctx, contextKeyTenantID, "default")
			}
			r = r.WithContext(ctx)
		}
	}

	// Files are gated by the owning collection's view rule. Paths outside a
	// known collection are admin-only. 404 (not 403) avoids leaking existence.
	cacheControl := "public, max-age=31536000, immutable"
	if coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName); err == nil {
		record, err := h.app.Collections().GetRecord(r.Context(), coll, recordID)
		if err != nil || !h.checkRule(r, coll.ViewRule, nil, record) {
			writeError(w, 404, "File not found")
			return
		}
		if coll.ViewRule == nil || strings.TrimSpace(*coll.ViewRule) != "" {
			cacheControl = "private, max-age=0"
		}
	} else if !NewRequestInfo(r).IsAdmin {
		writeError(w, 404, "File not found")
		return
	}

	// Check thumb parameter
	thumb := r.URL.Query().Get("thumb")

	event := &events.FileDownloadRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfo(r),
		CollectionName: collName,
		RecordID:       recordID,
		Filename:       filename,
		Path:           path,
		Thumb:          thumb,
	}
	if err := h.app.OnFileDownloadRequest().Trigger(event, func(e events.Event) error { return event.Next() }); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	// Serve a cached/generated thumb variant when requested; fall back to the
	// original on any thumb failure (non-image files, bad size params).
	if event.Thumb != "" {
		if opts, err := storage.ParseThumb(event.Thumb); err == nil {
			if err := opts.ApplyTransform(r.URL.Query().Get("format"), r.URL.Query().Get("quality")); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			thumbData, mimeType, err := h.app.Storage().Thumb(r.Context(), event.Path, opts, h.app.ImageProcessor())
			if err == nil {
				w.Header().Set("Content-Type", mimeType)
				w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
				w.Header().Set("Cache-Control", cacheControl)
				w.Write(thumbData)
				return
			}
		}
	}

	data, info, err := h.app.Storage().Download(r.Context(), event.Path)
	if err != nil {
		writeError(w, 404, "File not found")
		return
	}

	w.Header().Set("Content-Type", info.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, info.OriginalName))
	w.Header().Set("Cache-Control", cacheControl)
	w.Write(data)
}

// FileToken mints a short-lived token that authorizes rule-checked file
// downloads via the ?token= query parameter (for img/video tags that cannot
// send an Authorization header).
func (h *Handlers) FileToken(w http.ResponseWriter, r *http.Request) {
	info := NewRequestInfo(r)
	if !info.IsAdmin && !info.IsRecordAuth {
		writeError(w, 401, "Authentication required")
		return
	}

	token, err := h.app.Auth().GenerateFileToken(auth.FileTokenClaims{
		AdminID:      info.AdminID,
		Role:         info.Role,
		Email:        info.Email,
		TenantID:     info.TenantID,
		RecordID:     info.RecordID,
		CollectionID: info.CollectionID,
		Verified:     info.Verified,
	})
	if err != nil {
		writeError(w, 500, "Failed to generate file token")
		return
	}
	writeOK(w, map[string]any{"token": token})
}

func (h *Handlers) FileUpload(w http.ResponseWriter, r *http.Request) {
	form := forms.FileUploadForm{
		Collection: r.URL.Query().Get("collection"),
		RecordID:   r.URL.Query().Get("record"),
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	r.ParseMultipartForm(50 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, "No file provided")
		return
	}
	defer file.Close()

	event := &events.FileUploadRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfoWithBody(r, form),
		CollectionName: form.Collection,
		RecordID:       form.RecordID,
		Filename:       header.Filename,
		Size:           header.Size,
	}
	if err := h.app.OnFileUploadRequest().Trigger(event, func(e events.Event) error { return event.Next() }); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	info, err := h.app.Storage().Upload(r.Context(), event.CollectionName, event.RecordID, file, header)
	if err != nil {
		writeError(w, 500, "Upload failed: "+err.Error())
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "file.upload", event.CollectionName, event.RecordID, map[string]any{"path": info.Path, "filename": info.OriginalName}, r)
	writeJSON(w, 201, info)
}

func (h *Handlers) FilesPromote(w http.ResponseWriter, r *http.Request) {
	var form forms.PromoteFilesForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	infos := make([]*storage.FileInfo, 0, len(form.Files))
	filenames := make([]string, 0, len(form.Files))
	for _, filePath := range form.Files {
		info, err := h.app.Storage().Promote(r.Context(), filePath, form.Collection, form.RecordID)
		if err != nil {
			writeError(w, 400, "Promote failed: "+err.Error())
			return
		}
		infos = append(infos, info)
		filenames = append(filenames, filepath.Base(info.Path))
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "file.promote", form.Collection, form.RecordID, map[string]any{"files": form.Files}, r)
	writeOK(w, map[string]any{"files": infos, "filenames": filenames})
}

func (h *Handlers) FileDelete(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")
	filename := chi.URLParam(r, "filename")
	path := fmt.Sprintf("%s/%s/%s", collName, recordID, filename)

	event := &events.FileDeleteRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfo(r),
		CollectionName: collName,
		RecordID:       recordID,
		Filename:       filename,
		Path:           path,
	}
	if err := h.app.OnFileDeleteRequest().Trigger(event, func(e events.Event) error {
		// Drop cached thumbnail variants alongside the original.
		_ = h.app.Storage().DeleteThumbs(r.Context(), event.Path)
		return h.app.Storage().Delete(r.Context(), event.Path)
	}); err != nil {
		writeError(w, 500, "Delete failed: "+err.Error())
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "file.delete", event.CollectionName, event.RecordID, map[string]any{"path": event.Path}, r)
	writeOK(w, map[string]any{"deleted": path})
}
