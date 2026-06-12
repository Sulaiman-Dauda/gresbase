package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/rs/zerolog/log"
)

// Service handles file storage operations across local and S3 backends.
type Service struct {
	cfg      *config.Config
	local    *LocalStorage
	s3       *S3Storage
	mu       sync.RWMutex
	thumbSem chan struct{}
}

// NewService creates a new storage service.
func NewService(cfg *config.Config) (*Service, error) {
	s := &Service{cfg: cfg, thumbSem: make(chan struct{}, 4)}

	if cfg.StorageBackend == "local" {
		s.local = NewLocalStorage(cfg.StorageLocal)
		if err := s.local.Ensure(); err != nil {
			return nil, fmt.Errorf("failed to initialize local storage: %w", err)
		}
	} else if cfg.StorageBackend == "s3" {
		s3, err := NewS3Storage(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize S3 storage: %w", err)
		}
		s.s3 = s3
	}

	return s, nil
}

// Upload stores a file and returns its metadata.
func (s *Service) Upload(ctx context.Context, collection, recordID string, file multipart.File, header *multipart.FileHeader) (*FileInfo, error) {
	ext := filepath.Ext(header.Filename)
	hash := sha256.Sum256([]byte(uuid.New().String()))
	uniqueName := hex.EncodeToString(hash[:16]) + ext
	storagePath := filepath.Join(collection, recordID, uniqueName)

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded file: %w", err)
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		// Try to detect MIME type from file extension
		mimeType = detectMimeType(header.Filename)
	}
	// If still unknown, read the first 512 bytes to detect
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(content)
	}

	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return nil, fmt.Errorf("S3 storage not configured")
		}
		if err := s.s3.Put(storagePath, content, mimeType); err != nil {
			return nil, fmt.Errorf("S3 upload failed: %w", err)
		}

		// Generate signed URL
		signedURL, _ := s.s3.GetSignedURL(storagePath, 24*time.Hour)

		fileInfo := &FileInfo{
			Name:         header.Filename,
			OriginalName: header.Filename,
			Path:         storagePath,
			Size:         int64(len(content)),
			MimeType:     mimeType,
			Extension:    ext,
			CreatedAt:    time.Now(),
			URL:          signedURL,
		}
		log.Info().Str("file", fileInfo.Name).Int64("size", fileInfo.Size).Str("backend", "s3").Msg("File uploaded to S3")
		return fileInfo, nil

	default:
		if s.local == nil {
			return nil, fmt.Errorf("local storage not configured")
		}
		if err := s.local.Put(storagePath, content); err != nil {
			return nil, err
		}

		fileInfo := &FileInfo{
			Name:         header.Filename,
			OriginalName: header.Filename,
			Path:         storagePath,
			Size:         int64(len(content)),
			MimeType:     mimeType,
			Extension:    ext,
			CreatedAt:    time.Now(),
			URL:          s.fileURL(storagePath),
		}
		log.Info().Str("file", fileInfo.Name).Int64("size", fileInfo.Size).Str("path", storagePath).Msg("File uploaded")
		return fileInfo, nil
	}
}

// UploadBytes stores raw bytes as a record file using the same naming
// convention as Upload ({collection}/{recordID}/{randomhash}{ext}). It is used
// by upload paths that already hold the full file content in memory (e.g. the
// TUS resumable-upload completion step).
func (s *Service) UploadBytes(ctx context.Context, collection, recordID, originalName string, content []byte, mimeType string) (*FileInfo, error) {
	ext := filepath.Ext(originalName)
	hash := sha256.Sum256([]byte(uuid.New().String()))
	uniqueName := hex.EncodeToString(hash[:16]) + ext
	storagePath := pathpkg.Join(collection, recordID, uniqueName)

	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = detectMimeType(originalName)
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(content)
	}

	if err := s.putAtPath(storagePath, content, mimeType); err != nil {
		return nil, err
	}

	info := &FileInfo{
		Name:         uniqueName,
		OriginalName: originalName,
		Path:         storagePath,
		Size:         int64(len(content)),
		MimeType:     mimeType,
		Extension:    ext,
		CreatedAt:    time.Now(),
		URL:          s.fileURL(storagePath),
	}
	log.Info().Str("file", info.OriginalName).Int64("size", info.Size).Str("path", storagePath).Msg("File uploaded (bytes)")
	return info, nil
}

// DetectMimeFromName exposes extension-based MIME detection so callers that
// sniff content bytes can fall back to the same extension table the multipart
// upload path uses.
func DetectMimeFromName(filename string) string {
	return detectMimeType(filename)
}

// UploadTemp stores a staged file under the internal _tmp prefix.
func (s *Service) UploadTemp(ctx context.Context, session string, file multipart.File, header *multipart.FileHeader) (*FileInfo, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil, fmt.Errorf("session is required")
	}

	ext := filepath.Ext(header.Filename)
	hash := sha256.Sum256([]byte(uuid.New().String()))
	uniqueName := hex.EncodeToString(hash[:16]) + ext
	storagePath := pathpkg.Join("_tmp", session, uniqueName)

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded file: %w", err)
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = detectMimeType(header.Filename)
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(content)
	}

	if err := s.putAtPath(storagePath, content, mimeType); err != nil {
		return nil, err
	}

	return &FileInfo{
		Name:         header.Filename,
		OriginalName: header.Filename,
		Path:         storagePath,
		Size:         int64(len(content)),
		MimeType:     mimeType,
		Extension:    ext,
		CreatedAt:    time.Now(),
		URL:          s.fileURL(storagePath),
	}, nil
}

// Promote moves a staged file from _tmp/<session>/... into <collection>/<recordID>/...
func (s *Service) Promote(ctx context.Context, tempPath, collection, recordID string) (*FileInfo, error) {
	cleaned := pathpkg.Clean(strings.TrimSpace(tempPath))
	if cleaned == "." || cleaned == "" || strings.Contains(cleaned, "..") {
		return nil, fmt.Errorf("invalid temp file path")
	}
	if !strings.HasPrefix(cleaned, "_tmp/") {
		return nil, fmt.Errorf("file is not a staged temp upload")
	}
	if strings.TrimSpace(collection) == "" || strings.TrimSpace(recordID) == "" {
		return nil, fmt.Errorf("collection and recordID are required")
	}

	data, info, err := s.Download(ctx, cleaned)
	if err != nil {
		return nil, err
	}

	finalName := pathpkg.Base(cleaned)
	finalPath := pathpkg.Join(collection, recordID, finalName)
	if err := s.putAtPath(finalPath, data, info.MimeType); err != nil {
		return nil, err
	}
	if err := s.Delete(ctx, cleaned); err != nil {
		log.Warn().Err(err).Str("path", cleaned).Msg("Failed to delete temp upload after promotion")
	}

	return &FileInfo{
		Name:         finalName,
		OriginalName: firstNonEmpty(info.OriginalName, info.Name, finalName),
		Path:         finalPath,
		Size:         int64(len(data)),
		MimeType:     info.MimeType,
		Extension:    filepath.Ext(finalName),
		CreatedAt:    time.Now(),
		URL:          s.fileURL(finalPath),
	}, nil
}

func (s *Service) putAtPath(path string, data []byte, mimeType string) error {
	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return fmt.Errorf("S3 storage not configured")
		}
		if err := s.s3.Put(path, data, mimeType); err != nil {
			return fmt.Errorf("S3 upload failed: %w", err)
		}
		return nil
	default:
		if s.local == nil {
			return fmt.Errorf("local storage not configured")
		}
		return s.local.Put(path, data)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// Download retrieves a file by its path.
func (s *Service) Download(ctx context.Context, path string) ([]byte, *FileInfo, error) {
	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return nil, nil, fmt.Errorf("S3 storage not configured")
		}
		return s.s3.Get(path)
	default:
		if s.local == nil {
			return nil, nil, fmt.Errorf("local storage not configured")
		}
		return s.local.Get(path)
	}
}

// Delete removes a file from storage.
func (s *Service) Delete(ctx context.Context, path string) error {
	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return fmt.Errorf("S3 storage not configured")
		}
		return s.s3.Delete(path)
	default:
		if s.local == nil {
			return fmt.Errorf("local storage not configured")
		}
		return s.local.Delete(path)
	}
}

// DeletePrefix removes every stored object under the provided path prefix.
func (s *Service) DeletePrefix(ctx context.Context, prefix string) error {
	cleaned := strings.TrimSpace(strings.Trim(prefix, "/"))
	if cleaned == "" || cleaned == "." || strings.Contains(cleaned, "..") {
		return fmt.Errorf("invalid storage prefix")
	}
	if !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}

	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return fmt.Errorf("S3 storage not configured")
		}
		return s.s3.DeletePrefix(ctx, cleaned)
	default:
		if s.local == nil {
			return fmt.Errorf("local storage not configured")
		}
		return s.local.DeletePrefix(cleaned)
	}
}

// GetSignedURL generates a signed URL for temporary access.
func (s *Service) GetSignedURL(path string, expiry time.Duration) (string, error) {
	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return "", fmt.Errorf("S3 storage not configured")
		}
		return s.s3.GetSignedURL(path, expiry)
	default:
		return s.fileURL(path), nil
	}
}

// FileExists checks if a file exists.
func (s *Service) FileExists(ctx context.Context, path string) bool {
	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 != nil {
			return s.s3.Exists(path)
		}
		return false
	default:
		if s.local != nil {
			return s.local.Exists(path)
		}
		return false
	}
}

// ListFiles lists files in a directory.
func (s *Service) ListFiles(ctx context.Context, prefix string) ([]*FileInfo, error) {
	switch s.cfg.StorageBackend {
	case "s3":
		if s.s3 == nil {
			return nil, fmt.Errorf("S3 storage not configured")
		}
		return s.s3.List(prefix)
	default:
		if s.local == nil {
			return nil, fmt.Errorf("local storage not configured")
		}
		return s.local.List(prefix)
	}
}

func (s *Service) fileURL(path string) string {
	return fmt.Sprintf("/api/v1/files/%s", path)
}

// PutTest is a test helper that writes raw bytes to a path.
func (s *Service) PutTest(path string, data []byte) error {
	if s.cfg.StorageBackend == "local" && s.local != nil {
		return s.local.Put(path, data)
	}
	return fmt.Errorf("not local storage")
}

// GetSignedURLTest is a test helper that returns the file URL.
func (s *Service) GetSignedURLTest(path string, _ time.Duration) string {
	return s.fileURL(path)
}

// -------------------------------------------------------------------
// Local Filesystem Storage
// -------------------------------------------------------------------

type LocalStorage struct {
	basePath string
}

func NewLocalStorage(basePath string) *LocalStorage {
	return &LocalStorage{basePath: basePath}
}

func (l *LocalStorage) Ensure() error {
	return os.MkdirAll(l.basePath, 0755)
}

func (l *LocalStorage) Put(path string, data []byte) error {
	fullPath := filepath.Join(l.basePath, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return os.WriteFile(fullPath, data, 0644)
}

func (l *LocalStorage) Get(path string) ([]byte, *FileInfo, error) {
	fullPath := filepath.Join(l.basePath, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, nil, err
	}
	stat, _ := os.Stat(fullPath)
	info := &FileInfo{
		Name:      filepath.Base(path),
		Path:      path,
		Size:      stat.Size(),
		CreatedAt: stat.ModTime(),
	}
	return data, info, nil
}

func (l *LocalStorage) Delete(path string) error {
	fullPath := filepath.Join(l.basePath, path)
	return os.Remove(fullPath)
}

func (l *LocalStorage) DeletePrefix(prefix string) error {
	cleaned := strings.TrimSpace(strings.Trim(prefix, "/"))
	if cleaned == "" || cleaned == "." || strings.Contains(cleaned, "..") {
		return fmt.Errorf("invalid storage prefix")
	}
	fullPath := filepath.Join(l.basePath, filepath.FromSlash(cleaned))
	return os.RemoveAll(fullPath)
}

func (l *LocalStorage) Exists(path string) bool {
	fullPath := filepath.Join(l.basePath, path)
	_, err := os.Stat(fullPath)
	return err == nil
}

func (l *LocalStorage) List(prefix string) ([]*FileInfo, error) {
	fullPath := filepath.Join(l.basePath, prefix)
	dir := filepath.Dir(fullPath)
	var files []*FileInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(l.basePath, path)
		relPath = filepath.ToSlash(relPath)
		if !strings.HasPrefix(relPath, prefix) {
			return nil
		}
		files = append(files, &FileInfo{
			Name:      info.Name(),
			Path:      relPath,
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
		return nil
	})
	return files, err
}

// -------------------------------------------------------------------
// Models
// -------------------------------------------------------------------

type FileInfo struct {
	Name         string    `json:"name"`
	OriginalName string    `json:"original_name"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mime_type"`
	Extension    string    `json:"extension"`
	URL          string    `json:"url"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// -------------------------------------------------------------------
// MIME type detection helpers
// -------------------------------------------------------------------

// detectMimeType detects MIME type from file extension.
func detectMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg", ".ogv":
		return "video/ogg"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	case ".pdf":
		return "application/pdf"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js", ".mjs":
		return "application/javascript"
	case ".ts":
		return "application/typescript"
	case ".txt", ".md", ".markdown":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	case ".rar":
		return "application/vnd.rar"
	case ".7z":
		return "application/x-7z-compressed"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		// Try Go's standard mime type detection
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}
