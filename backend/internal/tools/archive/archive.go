// Package tools/archive provides archive creation and extraction utilities.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxDecompressedSize caps the total bytes written when extracting an archive,
// guarding against decompression-bomb (zip/tar) denial-of-service. 10 GiB is
// far above any legitimate Gresbase backup while still bounding worst-case disk
// usage from a hostile archive.
const maxDecompressedSize = 10 << 30 // 10 GiB

// permModeFromTar derives a filesystem permission mode from a tar header's
// mode field. It masks to the low permission bits (0o777), which both keeps the
// resulting permissions sane and makes the int64->uint32 conversion safe from
// overflow (the masked value always fits in 9 bits).
func permModeFromTar(mode int64) os.FileMode {
	//nolint:gosec // G115 false positive: masking to 0o777 bounds the value to 9 bits, so the conversion cannot overflow.
	return os.FileMode(uint32(mode & 0o777))
}

// copyLimited copies from src to dst but fails if more than remaining bytes
// would be written, returning the number of bytes copied. It is used to bound
// total extraction size across all archive entries.
func copyLimited(dst io.Writer, src io.Reader, remaining int64) (int64, error) {
	// Read one byte past the limit so we can detect an over-limit entry.
	n, err := io.Copy(dst, io.LimitReader(src, remaining+1))
	if err != nil {
		return n, err
	}
	if n > remaining {
		return n, fmt.Errorf("archive entry exceeds maximum decompressed size of %d bytes", maxDecompressedSize)
	}
	return n, nil
}

// CreateZip creates a zip archive from a directory.
func CreateZip(sourceDir, destPath string) (err error) {
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer func() {
		// Surface a close (flush) failure only if no earlier error occurred.
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	w := zip.NewWriter(file)

	walkErr := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		f, err := w.Create(relPath)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		_, err = f.Write(data)
		return err
	})

	// Always close the zip writer to flush its central directory; report the
	// walk error first, otherwise the close error.
	closeErr := w.Close()
	if walkErr != nil {
		return walkErr
	}
	return closeErr
}

// CreateTarGz creates a .tar.gz archive from a directory.
func CreateTarGz(sourceDir, destPath string) (err error) {
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create tar.gz: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)

	walkErr := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, err = tw.Write(data)
			return err
		}

		return nil
	})

	// Flush tar then gzip in order; report the first error encountered.
	twErr := tw.Close()
	gwErr := gw.Close()
	switch {
	case walkErr != nil:
		return walkErr
	case twErr != nil:
		return twErr
	default:
		return gwErr
	}
}

// ExtractZip extracts a zip archive to a destination directory.
func ExtractZip(srcPath, destDir string) error {
	r, err := zip.OpenReader(srcPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	remaining := int64(maxDecompressedSize)
	for _, f := range r.File {
		//nolint:gosec // G305 false positive: the zip-slip guard below rejects any path escaping destDir before it is used.
		destPath := filepath.Join(destDir, f.Name)

		// Prevent zip slip attacks
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, f.FileInfo().Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.FileInfo().Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return err
		}

		n, err := copyLimited(outFile, rc, remaining)
		remaining -= n
		_ = outFile.Close()
		_ = rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// ExtractTarGz extracts a .tar.gz archive to a destination directory.
func ExtractTarGz(srcPath, destDir string) error {
	file, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open tar.gz: %w", err)
	}
	defer func() { _ = file.Close() }() // read source; close error not actionable

	gr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }() // read source; close error not actionable

	tr := tar.NewReader(gr)

	remaining := int64(maxDecompressedSize)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		//nolint:gosec // G305 false positive: the path-traversal guard below rejects any path escaping destDir before it is used.
		destPath := filepath.Join(destDir, header.Name)

		// Prevent path traversal
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, permModeFromTar(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			outFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			n, copyErr := copyLimited(outFile, tr, remaining)
			remaining -= n
			closeErr := outFile.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Chmod(destPath, permModeFromTar(header.Mode)); err != nil {
				return err
			}
		}
	}

	return nil
}

// ListArchive returns the file listing from an archive.
func ListArchive(srcPath string) ([]string, error) {
	r, err := zip.OpenReader(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = r.Close() }() // read source; close error not actionable

	var files []string
	for _, f := range r.File {
		files = append(files, f.Name)
	}
	return files, nil
}
