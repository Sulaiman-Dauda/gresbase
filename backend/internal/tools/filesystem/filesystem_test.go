package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExists(t *testing.T) {
	tmpDir := t.TempDir()

	if Exists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("nonexistent path should not exist")
	}

	testFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(testFile, []byte("data"), 0644)

	if !Exists(testFile) {
		t.Error("file should exist")
	}
}

func TestIsDir(t *testing.T) {
	tmpDir := t.TempDir()

	if !IsDir(tmpDir) {
		t.Error("temp dir should be a directory")
	}

	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("data"), 0644)
	if IsDir(testFile) {
		t.Error("file should not be a directory")
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	nested := filepath.Join(tmpDir, "a", "b", "c")

	err := EnsureDir(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !IsDir(nested) {
		t.Error("nested directory should exist")
	}
}

func TestReadWriteFileString(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	err := WriteFileString(path, "hello world")
	if err != nil {
		t.Fatal(err)
	}

	content, err := ReadFileString(path)
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello world" {
		t.Errorf("expected 'hello world', got %q", content)
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	os.WriteFile(src, []byte("copy me"), 0644)
	err := CopyFile(src, dst)
	if err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(dst)
	if string(content) != "copy me" {
		t.Errorf("copy failed: got %q", string(content))
	}
}

func TestCopyDir(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")

	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("b"), 0644)

	err := CopyDir(srcDir, dstDir)
	if err != nil {
		t.Fatal(err)
	}

	if !Exists(filepath.Join(dstDir, "a.txt")) {
		t.Error("a.txt should be copied")
	}
	if !Exists(filepath.Join(dstDir, "sub", "b.txt")) {
		t.Error("sub/b.txt should be copied")
	}
}

func TestSafePath(t *testing.T) {
	base := "/data"

	// Valid paths
	valid, err := SafePath(base, "users/123/avatar.png")
	if err != nil {
		t.Errorf("unexpected error for valid path: %v", err)
	}
	if valid != "/data/users/123/avatar.png" {
		t.Errorf("expected '/data/users/123/avatar.png', got %q", valid)
	}

	// Path traversal attempt
	_, err = SafePath(base, "../../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestListFiles(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "sub", "b.txt"), []byte("b"), 0644)

	files, err := ListFiles(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		size     int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		result := HumanSize(tt.size)
		if result != tt.expected {
			t.Errorf("HumanSize(%d) = %q, want %q", tt.size, result, tt.expected)
		}
	}
}

func TestDiskUsage(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("1234567890"), 0644)

	size, err := DiskUsage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if size != 10 {
		t.Errorf("expected 10 bytes, got %d", size)
	}
}

func TestIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	empty, err := IsEmpty(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("new directory should be empty")
	}

	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	empty, err = IsEmpty(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if empty {
		t.Error("directory with file should not be empty")
	}
}

func TestRemoveAll(t *testing.T) {
	tmpDir := t.TempDir()
	nested := filepath.Join(tmpDir, "to-remove")
	os.MkdirAll(nested, 0755)
	os.WriteFile(filepath.Join(nested, "file.txt"), []byte("x"), 0644)

	err := RemoveAll(nested)
	if err != nil {
		t.Fatal(err)
	}
	if Exists(nested) {
		t.Error("directory should be removed")
	}
}
