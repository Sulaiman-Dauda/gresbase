package storage

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewBackupService(t *testing.T) {
	// This test verifies the service can be created with a mock
	svc := &BackupService{
		backupsDir: t.TempDir(),
	}
	if svc.backupsDir == "" {
		t.Error("backups dir should be set")
	}
}

func TestBackupService_ListBackups_Empty(t *testing.T) {
	svc := &BackupService{backupsDir: t.TempDir()}
	backups, err := svc.ListBackups()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(backups))
	}
}

func TestBackupService_DeleteBackup_NotFound(t *testing.T) {
	svc := &BackupService{backupsDir: t.TempDir()}
	err := svc.DeleteBackup("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent backup")
	}
}

func TestBackupService_GetBackupPath_NotFound(t *testing.T) {
	svc := &BackupService{backupsDir: t.TempDir()}
	_, err := svc.GetBackupPath("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestBackupInfo_Fields(t *testing.T) {
	info := &BackupInfo{
		ID:              "test-id",
		Name:            "test-backup",
		Size:            1024,
		CollectionCount: 5,
		RecordCount:     100,
		Status:          "completed",
		CreatedAt:       time.Now(),
	}

	if info.ID != "test-id" {
		t.Error("ID mismatch")
	}
	if info.Status != "completed" {
		t.Error("status mismatch")
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"posts", `"posts"`},
		{"user_data", `"user_data"`},
		{"with\"quote", `"with""quote"`},
	}
	for _, tt := range tests {
		got := QuoteIdent(tt.input)
		if got != tt.expected {
			t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSplitSQL(t *testing.T) {
	sql := "CREATE TABLE t (id INT); INSERT INTO t VALUES (1);"
	parts := splitSQL(sql)
	if len(parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(parts))
	}

	sql2 := "SELECT * FROM t"
	parts2 := splitSQL(sql2)
	if len(parts2) != 1 {
		t.Errorf("expected 1 part, got %d", len(parts2))
	}
}

func TestImageProcessor_Thumbnail_SizeParse(t *testing.T) {
	w, h, err := parseSize("100x200")
	if err != nil || w != 100 || h != 200 {
		t.Errorf("parse 100x200: w=%d h=%d err=%v", w, h, err)
	}

	w, h, err = parseSize("150x")
	if err != nil || w != 150 || h != 0 {
		t.Errorf("parse 150x: w=%d h=%d err=%v", w, h, err)
	}

	w, h, err = parseSize("x300")
	if err != nil || w != 0 || h != 300 {
		t.Errorf("parse x300: w=%d h=%d err=%v", w, h, err)
	}

	_, _, err = parseSize("invalid")
	if err == nil {
		t.Error("expected error for invalid size")
	}
}

func TestImageProcessor_MimeType(t *testing.T) {
	p := NewImageProcessor()

	if p.MimeType("jpg") != "image/jpeg" {
		t.Error("jpg mime type")
	}
	if p.MimeType("jpeg") != "image/jpeg" {
		t.Error("jpeg mime type")
	}
	if p.MimeType("png") != "image/png" {
		t.Error("png mime type")
	}
	if p.MimeType("gif") != "image/gif" {
		t.Error("gif mime type")
	}
}

func TestImageProcessor_Extension(t *testing.T) {
	p := NewImageProcessor()
	if p.Extension("jpeg") != ".jpg" {
		t.Error("jpeg extension")
	}
	if p.Extension("png") != ".png" {
		t.Error("png extension")
	}
}

func TestImageProcessor_New(t *testing.T) {
	p := NewImageProcessor()
	if p.jpegQuality != 85 {
		t.Errorf("default quality should be 85, got %d", p.jpegQuality)
	}

	p.SetJPEGQuality(95)
	if p.jpegQuality != 95 {
		t.Error("quality not updated")
	}

	p.SetJPEGQuality(200) // should clamp
	if p.jpegQuality != 100 {
		t.Error("quality should be clamped to 100")
	}

	p.SetJPEGQuality(-5) // should clamp
	if p.jpegQuality != 1 {
		t.Error("quality should be clamped to 1")
	}
}

func TestImageProcessor_Thumbnail_JPEG(t *testing.T) {
	// Create a small test JPEG image
	buf := createTestJPEG(100, 100)
	p := NewImageProcessor()

	thumbData, format, err := p.Thumbnail(buf, "50x50", "")
	if err != nil {
		t.Fatalf("thumbnail error: %v", err)
	}
	if len(thumbData) == 0 {
		t.Error("thumbnail data is empty")
	}
	if format != "jpeg" {
		t.Errorf("expected jpeg format, got %s", format)
	}
}

func TestImageProcessor_GetImageDimensions(t *testing.T) {
	buf := createTestJPEG(200, 100)
	p := NewImageProcessor()

	w, h, format, err := p.GetImageDimensions(buf)
	if err != nil {
		t.Fatalf("dimensions error: %v", err)
	}
	if w != 200 || h != 100 {
		t.Errorf("expected 200x100, got %dx%d", w, h)
	}
	if format != "jpeg" {
		t.Errorf("expected jpeg, got %s", format)
	}
}

func TestImageProcessor_Resize(t *testing.T) {
	buf := createTestJPEG(100, 100)
	p := NewImageProcessor()

	data, format, err := p.Resize(buf, 50, 50)
	if err != nil {
		t.Fatalf("resize error: %v", err)
	}
	if len(data) == 0 {
		t.Error("resized data is empty")
	}
	if format != "jpeg" {
		t.Errorf("expected jpeg, got %s", format)
	}
}

func TestImageProcessor_Thumbnail_Crop(t *testing.T) {
	buf := createTestJPEG(200, 100)
	p := NewImageProcessor()

	// Crop center should produce square output
	data, _, err := p.Thumbnail(buf, "50x50", "center")
	if err != nil {
		t.Fatalf("crop thumbnail error: %v", err)
	}
	if len(data) == 0 {
		t.Error("cropped data is empty")
	}
}

func TestImageProcessor_Thumbnail_HeightOnly(t *testing.T) {
	buf := createTestJPEG(100, 200)
	p := NewImageProcessor()

	data, _, err := p.Thumbnail(buf, "x50", "")
	if err != nil {
		t.Fatalf("height-only thumbnail error: %v", err)
	}
	if len(data) == 0 {
		t.Error("data is empty")
	}
}

func TestImageProcessor_Thumbnail_WidthOnly(t *testing.T) {
	buf := createTestJPEG(200, 100)
	p := NewImageProcessor()

	data, _, err := p.Thumbnail(buf, "50x", "")
	if err != nil {
		t.Fatalf("width-only thumbnail error: %v", err)
	}
	if len(data) == 0 {
		t.Error("data is empty")
	}
}

// Helper to create a minimal JPEG for testing
func createTestJPEG(width, height int) *bytes.Buffer {
	// Create a real image using Go's standard library
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a solid color
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{128, 128, 128, 255}}, image.Point{}, draw.Src)

	buf := new(bytes.Buffer)
	err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 80})
	if err != nil {
		// Fallback: return a pre-encoded minimal valid JPEG
		buf.Reset()
		buf.Write(minimalValidJPEG())
	}
	return buf
}

func minimalValidJPEG() []byte {
	// Generate a 1x1 gray JPEG
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{128, 128, 128, 255})
	buf := new(bytes.Buffer)
	jpeg.Encode(buf, img, nil)
	return buf.Bytes()
}

// Ensure unused imports compile
var _ = context.Background
var _ = io.Copy
var _ = os.Create
var _ = filepath.Join
var _ = strings.TrimSpace
