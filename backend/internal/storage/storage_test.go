package storage_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/storage"
)

func TestLocalStoragePutGetDelete(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		StorageBackend: "local",
		StorageLocal:   dir,
		LogLevel:       "error",
	}

	svc, err := storage.NewService(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}

	data := []byte("hello, gresbase storage test")
	path := "test/hello.txt"

	// Write
	if err := svc.PutTest(path, data); err != nil {
		t.Fatalf("Failed to put file: %v", err)
	}

	// Check exists
	if !svc.FileExists(nil, path) {
		t.Error("File should exist after put")
	}

	// Read
	got, info, err := svc.Download(nil, path)
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("Data mismatch: got %q, want %q", got, data)
	}
	if info == nil {
		t.Error("FileInfo should not be nil")
	}

	// Delete
	if err := svc.Delete(nil, path); err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	if svc.FileExists(nil, path) {
		t.Error("File should not exist after delete")
	}
}

func TestFileURIGeneration(t *testing.T) {
	cfg := &config.Config{
		StorageBackend: "local",
		StorageLocal:   t.TempDir(),
		LogLevel:       "error",
	}

	svc, err := storage.NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}

	url := svc.GetSignedURLTest("coll/rec/file.txt", 0)
	expected := "/api/v1/files/coll/rec/file.txt"
	if url != expected {
		t.Errorf("Expected %q, got %q", expected, url)
	}
}

func TestLocalStorageDirectoryCreation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "path")
	cfg := &config.Config{
		StorageBackend: "local",
		StorageLocal:   dir,
		LogLevel:       "error",
	}

	svc, err := storage.NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Write to a nested path - directories should be auto-created
	if err := svc.PutTest("a/b/c/file.txt", []byte("nested")); err != nil {
		t.Fatalf("Failed to write nested file: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c", "file.txt")); err != nil {
		t.Errorf("Nested file should exist: %v", err)
	}
}

func TestFileInfoMetadata(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		StorageBackend: "local",
		StorageLocal:   dir,
		LogLevel:       "error",
	}

	svc, _ := storage.NewService(cfg)

	data := []byte("metadata test")
	path := "meta/test.txt"
	svc.PutTest(path, data)

	_, info, err := svc.Download(nil, path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Name != "test.txt" {
		t.Errorf("Expected name 'test.txt', got %q", info.Name)
	}
	if info.Size != int64(len(data)) {
		t.Errorf("Expected size %d, got %d", len(data), info.Size)
	}
}
