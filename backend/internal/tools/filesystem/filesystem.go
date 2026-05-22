// Package tools/filesystem provides file system utilities.
package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileInfo extends fs.FileInfo with path information.
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime int64  `json:"mod_time"`
}

// Exists checks if a path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir checks if a path is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// EnsureDir creates a directory and all parents if they don't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// ListFiles returns all files in a directory recursively.
func ListFiles(root string) ([]*FileInfo, error) {
	var files []*FileInfo
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(root, path)
		files = append(files, &FileInfo{
			Name:    info.Name(),
			Path:    filepath.ToSlash(relPath),
			Size:    info.Size(),
			IsDir:   false,
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})
	return files, err
}

// ListDirs returns all subdirectories recursively.
func ListDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path != root {
			relPath, _ := filepath.Rel(root, path)
			dirs = append(dirs, filepath.ToSlash(relPath))
		}
		return nil
	})
	return dirs, err
}

// CopyFile copies a single file from src to dst.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// CopyDir copies a directory recursively.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return CopyFile(path, dstPath)
	})
}

// RemoveAll removes a path and all its contents.
func RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// ReadFileString reads a file as a string.
func ReadFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFileString writes a string to a file.
func WriteFileString(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// SafePath ensures a path is within a base directory (prevents path traversal).
func SafePath(base, path string) (string, error) {
	// Clean both paths
	cleanBase := filepath.Clean(base)
	cleanPath := filepath.Clean(filepath.Join(cleanBase, path))

	// Check if the result is still under the base
	rel, err := filepath.Rel(cleanBase, cleanPath)
	if err != nil {
		return "", fmt.Errorf("path traversal detected")
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path traversal detected")
	}

	return cleanPath, nil
}

// HumanSize returns a human-readable file size string.
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// FindFiles returns files matching a glob pattern.
func FindFiles(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// TempDir creates a temporary directory and returns its path.
func TempDir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

// TempFile creates a temporary file and returns it.
func TempFile(prefix string) (*os.File, error) {
	return os.CreateTemp("", prefix)
}

// DiskUsage returns the total size of a directory in bytes.
func DiskUsage(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// IsEmpty checks whether a directory is empty.
func IsEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

// Touch creates an empty file or updates its modification time.
func Touch(path string) error {
	if Exists(path) {
		now := time.Now // not a real call, placeholder
		_ = now
		return os.Chtimes(path, time.Now(), time.Now())
	}
	return os.WriteFile(path, []byte{}, 0644)
}

// Ensure io is used
var _ = io.EOF
