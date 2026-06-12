// TUS resumable uploads (https://tus.io), served in-process — no sidecar
// container. Uploads attach a file to an EXISTING record's file field
// (attach-on-complete); creating a record together with its first file stays
// on the regular multipart path.
//
// Design notes:
//
//   - Chunks are staged on the LOCAL filesystem at {storage_local_path}/.tus_uploads
//     even when the final storage backend is S3. Chunk state is node-local temp
//     state; in a multi-node deployment resumable uploads therefore require
//     sticky routing so every PATCH for an upload reaches the node that
//     accepted the POST. The finished file is moved into the configured
//     storage backend (local or S3) on completion.
//
//   - Auth + the target collection's UPDATE rule are enforced at upload
//     creation (POST), resolved from the Authorization header by the same
//     OptionalAuth middleware the records API uses. The verified identity is
//     stamped into the upload's server-side metadata (client-supplied values
//     for those keys are discarded because the metadata map is rebuilt).
//
//   - The Authorization header is not reliably replayed on the final PATCH, so
//     at completion the update rule is RE-EVALUATED for the identity captured
//     at creation against the record's CURRENT state. Tokens that expire
//     mid-upload do not invalidate the upload, but a rule or record change
//     that would now deny the writer does (fail-closed).
//
//   - Completion validates the actual bytes (MIME sniffing, size, maxSelect
//     capacity) against the file field's options, stores the file with the
//     same {collection}/{recordID}/{hash}{ext} naming the multipart path uses,
//     appends to multi-file fields (up to max_select) or replaces single-file
//     fields (deleting the previous file), and fires the same OnRecordUpdate
//     hook + realtime broadcast as a normal record update.
//
//   - Partial failure at completion: the chunk data is deleted and the error
//     is surfaced in the PATCH response. The upload is NOT kept for retrying
//     the attach step — tusd considers the transfer finished at that point, so
//     clients must restart the upload after fixing the cause.
//
//   - Abandoned uploads expire after 24h; a janitor goroutine garbage-collects
//     stale chunk files hourly.
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/ctxkeys"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/storage"
	"github.com/rs/zerolog/log"
	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
	"golang.org/x/exp/slog" // tusd v2.9 still uses the x/exp slog fork
)

const (
	tusBasePath = "/api/v1/files/tus/"
	// tusGlobalMaxSize caps uploads whose target field has no max_size option.
	tusGlobalMaxSize = int64(5) << 30 // 5 GiB
	// tusUploadTTL is how long an unfinished upload may sit before the janitor
	// garbage-collects its chunks.
	tusUploadTTL = 24 * time.Hour
)

// Client-supplied Upload-Metadata keys (all required).
const (
	tusMetaCollection = "collection"
	tusMetaRecordID   = "recordId"
	tusMetaField      = "field"
	tusMetaFilename   = "filename"
)

// Server-stamped identity keys. The metadata map is rebuilt at creation, so a
// client cannot inject these.
const (
	tusMetaAuthIsAdmin      = "__authIsAdmin"
	tusMetaAuthAdminID      = "__authAdminId"
	tusMetaAuthRole         = "__authRole"
	tusMetaAuthRecordID     = "__authRecordId"
	tusMetaAuthCollectionID = "__authCollectionId"
	tusMetaAuthEmail        = "__authEmail"
	tusMetaAuthVerified     = "__authVerified"
	tusMetaAuthAnonymous    = "__authAnonymous"
	tusMetaExpiresAt        = "__expiresAt"
)

// tusController wires the tusd handler callbacks to Gresbase auth, rules,
// storage and realtime.
type tusController struct {
	app      *app.App
	handlers *Handlers
	composer *handler.StoreComposer
	dir      string
}

// mountTUS registers the TUS endpoint at /api/v1/files/tus/ and starts the
// chunk janitor. Auth context is provided by OptionalAuth, exactly like the
// records API.
func (s *Server) mountTUS(r chi.Router) {
	cfg := s.app.Config()
	tusDir := filepath.Join(cfg.StorageLocal, ".tus_uploads")
	if err := os.MkdirAll(tusDir, 0o755); err != nil {
		log.Error().Err(err).Str("dir", tusDir).Msg("TUS: cannot create chunk directory; resumable uploads disabled")
		return
	}

	store := filestore.New(tusDir)
	composer := handler.NewStoreComposer()
	composer.UseCore(store)
	composer.UseTerminater(store)
	composer.UseLengthDeferrer(store)
	composer.UseLocker(memorylocker.New())

	ctrl := &tusController{app: s.app, handlers: s.h, composer: composer, dir: tusDir}

	tusHandler, err := handler.NewUnroutedHandler(handler.Config{
		StoreComposer:              composer,
		BasePath:                   tusBasePath,
		MaxSize:                    tusGlobalMaxSize,
		DisableDownload:            true,
		DisableConcatenation:       true,
		Cors:                       &handler.CorsConfig{Disable: true}, // global chi CORS middleware handles it
		Logger:                     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		PreUploadCreateCallback:    ctrl.preCreate,
		PreFinishResponseCallback:  ctrl.preFinish,
		PreUploadTerminateCallback: ctrl.preTerminate,
	})
	if err != nil {
		log.Error().Err(err).Msg("TUS: handler init failed; resumable uploads disabled")
		return
	}

	// Route like tusd's own NewHandler, but inside chi with OptionalAuth so the
	// hook context carries the same identity values as the records API.
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(r.URL.Path, "/")
		switch {
		case id == "" && r.Method == http.MethodPost:
			tusHandler.PostFile(w, r)
		case id != "" && r.Method == http.MethodHead:
			tusHandler.HeadFile(w, r)
		case id != "" && r.Method == http.MethodPatch:
			tusHandler.PatchFile(w, r)
		case id != "" && r.Method == http.MethodDelete:
			tusHandler.DelFile(w, r)
		default:
			w.Header().Set("Allow", "POST, HEAD, PATCH, DELETE, OPTIONS")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// r is the /api/v1 sub-router, so the mount pattern is relative; StripPrefix
	// operates on the full request path, which chi leaves untouched.
	endpoint := http.StripPrefix(strings.TrimSuffix(tusBasePath, "/"), tusHandler.Middleware(mux))
	r.With(s.mw.OptionalAuth).Mount("/files/tus", endpoint)

	s.startTUSJanitor(tusDir)
}

// ---------------------------------------------------------------------------
// Creation: auth + update rule + field constraints, fail-closed.
// ---------------------------------------------------------------------------

func (t *tusController) preCreate(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	none := handler.FileInfoChanges{}
	ctx := hook.Context
	meta := hook.Upload.MetaData

	collName := strings.TrimSpace(meta[tusMetaCollection])
	recordID := strings.TrimSpace(meta[tusMetaRecordID])
	fieldName := strings.TrimSpace(meta[tusMetaField])
	filename := strings.TrimSpace(meta[tusMetaFilename])
	if collName == "" || recordID == "" || fieldName == "" || filename == "" {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_INVALID_METADATA",
			"Upload-Metadata must include collection, recordId, field and filename", http.StatusBadRequest)
	}

	coll, err := t.app.Collections().GetCollectionByName(ctx, collName)
	if err != nil {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_NOT_FOUND", "collection not found", http.StatusNotFound)
	}
	field, ok := findFileField(coll, fieldName)
	if !ok {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_INVALID_METADATA",
			fmt.Sprintf("field %q does not exist or is not a file field", fieldName), http.StatusBadRequest)
	}

	record, err := t.app.Collections().GetRecord(ctx, coll, recordID)
	if err != nil {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_NOT_FOUND", "record not found", http.StatusNotFound)
	}

	// Enforce the collection's UPDATE rule for the requester (locked-by-default:
	// nil rule → superusers only).
	info := tusRequestInfoFromContext(ctx)
	rc := NewRuleContextFromInfo(info)
	rc.Method = http.MethodPatch
	if !t.checkUpdateRule(ctx, coll, rc, record) {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_ACCESS_DENIED",
			"you are not allowed to modify this record", http.StatusForbidden)
	}

	// Per-field size cap from the field options (else the global cap applies).
	if maxSize := fileFieldMaxSize(field); maxSize > 0 && !hook.Upload.SizeIsDeferred && hook.Upload.Size > maxSize {
		return handler.HTTPResponse{}, none, handler.NewError("ERR_UPLOAD_SIZE_EXCEEDED",
			fmt.Sprintf("upload exceeds the field's max size of %d bytes", maxSize), http.StatusRequestEntityTooLarge)
	}

	// Fail early when a multi-file field is already at capacity.
	if maxSelect := fileFieldMaxSelect(field); maxSelect > 1 {
		if len(fileNamesFromValue(record[field.Name])) >= maxSelect {
			return handler.HTTPResponse{}, none, handler.NewError("ERR_FIELD_FULL",
				fmt.Sprintf("field %q already holds the maximum of %d files", fieldName, maxSelect), http.StatusBadRequest)
		}
	}

	// Rebuild the metadata map: keep the validated target keys and stamp the
	// verified identity. Any client-supplied __auth* keys are discarded.
	newMeta := handler.MetaData{
		tusMetaCollection:       collName,
		tusMetaRecordID:         recordID,
		tusMetaField:            fieldName,
		tusMetaFilename:         filename,
		tusMetaAuthIsAdmin:      boolMeta(info.IsAdmin),
		tusMetaAuthAdminID:      info.AdminID,
		tusMetaAuthRole:         info.Role,
		tusMetaAuthRecordID:     info.RecordID,
		tusMetaAuthCollectionID: info.CollectionID,
		tusMetaAuthEmail:        info.Email,
		tusMetaAuthVerified:     boolMeta(info.Verified),
		tusMetaAuthAnonymous:    boolMeta(info.Anonymous),
		tusMetaExpiresAt:        time.Now().Add(tusUploadTTL).UTC().Format(time.RFC3339),
	}

	return handler.HTTPResponse{}, handler.FileInfoChanges{MetaData: newMeta}, nil
}

// ---------------------------------------------------------------------------
// Completion: re-check rule for the creation-time identity, validate bytes,
// move into real storage, attach to the record, broadcast, clean up chunks.
// ---------------------------------------------------------------------------

func (t *tusController) preFinish(hook handler.HookEvent) (handler.HTTPResponse, error) {
	ctx := hook.Context
	meta := hook.Upload.MetaData

	collName := meta[tusMetaCollection]
	recordID := meta[tusMetaRecordID]
	fieldName := meta[tusMetaField]
	filename := meta[tusMetaFilename]

	fail := func(code, msg string, status int) (handler.HTTPResponse, error) {
		// Attach failed or was denied: surface the error and drop the chunks.
		// tusd considers the transfer done, so the upload cannot be resumed —
		// the client must start over after fixing the cause.
		t.removeUpload(ctx, hook.Upload.ID)
		return handler.HTTPResponse{}, handler.NewError(code, msg, status)
	}

	coll, err := t.app.Collections().GetCollectionByName(ctx, collName)
	if err != nil {
		return fail("ERR_NOT_FOUND", "collection not found", http.StatusNotFound)
	}
	field, ok := findFileField(coll, fieldName)
	if !ok {
		return fail("ERR_INVALID_METADATA", "field no longer exists or is not a file field", http.StatusBadRequest)
	}
	record, err := t.app.Collections().GetRecord(ctx, coll, recordID)
	if err != nil {
		return fail("ERR_NOT_FOUND", "record not found", http.StatusNotFound)
	}

	// Re-evaluate the update rule for the identity verified at creation against
	// the record's CURRENT state (rules or the record may have changed while
	// the upload was in flight).
	rc := tusRuleContextFromMeta(meta)
	if !t.checkUpdateRule(ctx, coll, rc, record) {
		return fail("ERR_ACCESS_DENIED", "you are no longer allowed to modify this record", http.StatusForbidden)
	}

	// Read the assembled upload.
	upload, err := t.composer.Core.GetUpload(ctx, hook.Upload.ID)
	if err != nil {
		return fail("ERR_INTERNAL", "upload data unavailable", http.StatusInternalServerError)
	}
	reader, err := upload.GetReader(ctx)
	if err != nil {
		return fail("ERR_INTERNAL", "upload data unavailable", http.StatusInternalServerError)
	}
	content, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return fail("ERR_INTERNAL", "failed to read upload data", http.StatusInternalServerError)
	}

	// Size: enforce against the actual byte count (covers deferred lengths).
	maxSize := fileFieldMaxSize(field)
	if maxSize <= 0 {
		maxSize = tusGlobalMaxSize
	}
	if int64(len(content)) > maxSize {
		return fail("ERR_UPLOAD_SIZE_EXCEEDED",
			fmt.Sprintf("upload exceeds the field's max size of %d bytes", maxSize), http.StatusRequestEntityTooLarge)
	}

	// MIME: sniff the actual bytes; fall back to the extension table for types
	// http.DetectContentType cannot identify.
	mimeType := sniffMime(content, filename)
	if allowed := fileFieldMimeTypes(field); len(allowed) > 0 && !mimeAllowed(mimeType, allowed) {
		return fail("ERR_INVALID_FILE_TYPE",
			fmt.Sprintf("file type %q is not allowed for field %q", mimeType, fieldName), http.StatusUnsupportedMediaType)
	}

	// Field value: multi-file fields append up to max_select; single-file
	// fields replace (the previous file is deleted after the update, matching
	// the record update path's removed-files cleanup).
	maxSelect := fileFieldMaxSelect(field)
	current := fileNamesFromValue(record[field.Name])
	if maxSelect > 1 && len(current) >= maxSelect {
		return fail("ERR_FIELD_FULL",
			fmt.Sprintf("field %q already holds the maximum of %d files", fieldName, maxSelect), http.StatusBadRequest)
	}

	// Move the bytes into the real storage backend with the standard naming.
	fileInfo, err := t.app.Storage().UploadBytes(ctx, collName, recordID, filename, content, mimeType)
	if err != nil {
		return fail("ERR_INTERNAL", "failed to store file: "+err.Error(), http.StatusInternalServerError)
	}
	storedName := filepath.Base(fileInfo.Path)

	var newValue any
	if maxSelect > 1 {
		newValue = append(append([]string{}, current...), storedName)
	} else {
		newValue = storedName
	}

	patch := map[string]any{field.Name: newValue}
	attachErr := t.app.Collections().ValidateRecordPatch(coll, patch)
	if attachErr == nil {
		attachErr = t.app.Collections().UpdateRecord(ctx, coll, recordID, patch)
	}
	if attachErr != nil {
		// Roll back the stored file so a failed attach leaves no orphan.
		if delErr := t.app.Storage().Delete(ctx, fileInfo.Path); delErr != nil {
			log.Error().Err(delErr).Str("path", fileInfo.Path).Msg("tus: failed to roll back orphaned upload")
		}
		return fail("ERR_ATTACH_FAILED", "failed to update record: "+attachErr.Error(), http.StatusBadRequest)
	}

	updated, _ := t.app.Collections().GetRecord(ctx, coll, recordID)

	// Same post-update side effects as the records API: model hook, realtime
	// broadcast, replaced-file cleanup, audit log.
	_ = t.app.OnRecordUpdate().Trigger(&events.RecordEvent{
		App: t.app, Record: updated, CollectionID: coll.ID, CollectionName: coll.Name,
		RecordID: recordID, Type: events.ModelEventUpdate,
	}, func(e events.Event) error { return e.Next() })
	t.app.Realtime().BroadcastRecord("update", coll.Name, recordID, updated)
	for _, path := range removedRecordFilePaths(coll, recordID, record, updated) {
		if err := t.app.Storage().Delete(ctx, path); err != nil {
			log.Warn().Err(err).Str("collection", coll.Name).Str("record", recordID).Str("path", path).Msg("Failed to delete replaced file after TUS upload")
		}
	}
	t.app.Auth().RecordAudit(ctx, meta[tusMetaAuthAdminID], "file.tus_upload", coll.Name, recordID,
		map[string]any{"path": fileInfo.Path, "filename": filename, "field": fieldName}, nil)

	// Success: drop the local chunk files.
	t.removeUpload(ctx, hook.Upload.ID)

	return handler.HTTPResponse{Header: handler.HTTPHeader{"X-Gresbase-Filename": storedName}}, nil
}

// ---------------------------------------------------------------------------
// Termination: only the identity that created the upload (or a superuser) may
// DELETE it. Uploads created anonymously can only be terminated by superusers
// (an anonymous client cannot prove it is the same one — fail-closed).
// ---------------------------------------------------------------------------

func (t *tusController) preTerminate(hook handler.HookEvent) (handler.HTTPResponse, error) {
	meta := hook.Upload.MetaData
	info := tusRequestInfoFromContext(hook.Context)

	switch {
	case info.IsAdmin:
		return handler.HTTPResponse{}, nil // superusers may always terminate
	case info.IsRecordAuth && info.RecordID != "" && info.RecordID == meta[tusMetaAuthRecordID]:
		return handler.HTTPResponse{}, nil // same record identity that created it
	default:
		return handler.HTTPResponse{}, handler.NewError("ERR_ACCESS_DENIED",
			"you are not allowed to terminate this upload", http.StatusForbidden)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// checkUpdateRule mirrors Handlers.checkRule semantics for a pre-built rule
// context: admins bypass, nil rule is locked, "" is public, otherwise the
// filter expression is evaluated against the record.
func (t *tusController) checkUpdateRule(ctx context.Context, coll *collection.Collection, rc *RuleContext, record map[string]any) bool {
	if rc.IsAdmin {
		return true
	}
	rule := coll.UpdateRule
	if rule == nil {
		return false
	}
	if strings.TrimSpace(*rule) == "" {
		return true
	}
	return t.handlers.rules.EvaluateRuleBool(ctx, *rule, rc, record)
}

// removeUpload deletes the chunk + info files for an upload, ignoring errors.
func (t *tusController) removeUpload(ctx context.Context, id string) {
	upload, err := t.composer.Core.GetUpload(ctx, id)
	if err != nil {
		return
	}
	if err := t.composer.Terminater.AsTerminatableUpload(upload).Terminate(ctx); err != nil {
		log.Warn().Err(err).Str("upload", id).Msg("TUS: failed to remove chunk files")
	}
}

// tusRequestInfoFromContext builds a RequestInfo from the context values the
// auth middleware injects (the same keys NewRequestInfo reads from a request).
func tusRequestInfoFromContext(ctx context.Context) *RequestInfo {
	info := &RequestInfo{Query: map[string]string{}, Headers: map[string]string{}, Body: map[string]any{}}
	if adminID, ok := ctx.Value(contextKeyAdminID).(string); ok && adminID != "" {
		info.IsAdmin = true
		info.AdminID = adminID
	}
	if role, ok := ctx.Value(contextKeyAdminRole).(string); ok {
		info.Role = role
	}
	if email, ok := ctx.Value(ctxkeys.Email).(string); ok {
		info.Email = email
	}
	if recordID, ok := ctx.Value(ctxkeys.RecordID).(string); ok && recordID != "" {
		info.IsRecordAuth = true
		info.RecordID = recordID
	}
	if collectionID, ok := ctx.Value(ctxkeys.CollectionID).(string); ok {
		info.CollectionID = collectionID
	}
	if verified, ok := ctx.Value(ctxkeys.Verified).(bool); ok {
		info.Verified = verified
	}
	if anonymous, ok := ctx.Value(ctxkeys.Anonymous).(bool); ok {
		info.Anonymous = anonymous
	}
	return info
}

// tusRuleContextFromMeta reconstructs the rule context for the identity that
// was verified when the upload was created.
func tusRuleContextFromMeta(meta handler.MetaData) *RuleContext {
	return &RuleContext{
		IsAdmin:      meta[tusMetaAuthIsAdmin] == "true",
		IsRecordAuth: meta[tusMetaAuthRecordID] != "",
		AdminID:      meta[tusMetaAuthAdminID],
		RecordID:     meta[tusMetaAuthRecordID],
		CollectionID: meta[tusMetaAuthCollectionID],
		Role:         meta[tusMetaAuthRole],
		Email:        meta[tusMetaAuthEmail],
		Verified:     meta[tusMetaAuthVerified] == "true",
		Anonymous:    meta[tusMetaAuthAnonymous] == "true",
		Method:       http.MethodPatch,
		Query:        map[string]string{},
		Body:         map[string]any{},
	}
}

func boolMeta(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// findFileField returns the named schema field if it exists and is a file field.
func findFileField(coll *collection.Collection, name string) (*collection.SchemaField, bool) {
	for i := range coll.Schema {
		if coll.Schema[i].Name == name {
			if coll.Schema[i].Type != collection.FieldFile {
				return nil, false
			}
			return &coll.Schema[i], true
		}
	}
	return nil, false
}

func fileFieldMaxSize(field *collection.SchemaField) int64 {
	if field.Options == nil {
		return 0
	}
	switch v := field.Options["max_size"].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func fileFieldMaxSelect(field *collection.SchemaField) int {
	if field.Options == nil {
		return 1
	}
	switch v := field.Options["max_select"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 1
	}
}

func fileFieldMimeTypes(field *collection.SchemaField) []string {
	if field.Options == nil {
		return nil
	}
	switch v := field.Options["mime_types"].(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// sniffMime detects the MIME type from the actual content bytes, falling back
// to the extension table only where sniffing is inconclusive. The fallback is
// deliberately narrow: text content may only refine to other textual types,
// never to a binary type, so renaming evil.txt to evil.png cannot smuggle text
// bytes past an image/* mime_types restriction.
func sniffMime(content []byte, filename string) string {
	sniffed := http.DetectContentType(content)
	if i := strings.Index(sniffed, ";"); i >= 0 {
		sniffed = strings.TrimSpace(sniffed[:i])
	}
	byExt := storage.DetectMimeFromName(filename)
	switch sniffed {
	case "application/octet-stream":
		// Sniffing learned nothing; trust the extension if it knows more.
		if byExt != "" && byExt != "application/octet-stream" {
			return byExt
		}
	case "text/plain":
		// Refine generic text only to other textual types.
		if isTextualMime(byExt) {
			return byExt
		}
	}
	return sniffed
}

func isTextualMime(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/javascript", "application/typescript":
		return true
	default:
		return false
	}
}

// mimeAllowed reports whether mime matches the allowed list (exact match or a
// "type/*" wildcard).
func mimeAllowed(mime string, allowed []string) bool {
	for _, entry := range allowed {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, mime) {
			return true
		}
		if strings.HasSuffix(entry, "/*") &&
			strings.HasPrefix(strings.ToLower(mime), strings.ToLower(strings.TrimSuffix(entry, "*"))) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Janitor: garbage-collect chunk files of uploads abandoned for > tusUploadTTL.
// ---------------------------------------------------------------------------

func (s *Server) startTUSJanitor(dir string) {
	stop := make(chan struct{})
	s.tusJanitorStop = stop
	cleanExpiredTUSUploads(dir, tusUploadTTL)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				cleanExpiredTUSUploads(dir, tusUploadTTL)
			}
		}
	}()
}

// cleanExpiredTUSUploads removes chunk (+ .info) files whose last modification
// is older than ttl. The chunk file's mtime advances with every PATCH, so an
// upload only expires after ttl of inactivity.
func cleanExpiredTUSUploads(dir string, ttl time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-ttl)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fi, err := entry.Info()
		if err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".info") {
			// Only remove the info file once its chunk file is gone or expired.
			binPath := filepath.Join(dir, strings.TrimSuffix(name, ".info"))
			if binInfo, err := os.Stat(binPath); err == nil && binInfo.ModTime().After(cutoff) {
				continue
			}
		}
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			log.Debug().Str("file", name).Msg("TUS: removed expired upload artifact")
		}
	}
}
