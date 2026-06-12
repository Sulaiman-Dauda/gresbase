package storage

import (
	"bytes"
	"context"
	"fmt"
	pathpkg "path"
	"strconv"
	"strings"
)

// thumbPrefix is the internal storage prefix for cached thumbnail variants.
// It lives outside any collection path so collection rules never expose it
// directly; thumbnails are only served through the rule-checked download path.
const thumbPrefix = "_thumbs"

// maxThumbDimension caps requested thumb sizes: thumbnail generation decodes
// the full image into memory, so unbounded dimensions are a DoS vector.
const maxThumbDimension = 2048

// ThumbOptions describes a requested thumbnail variant.
type ThumbOptions struct {
	Size    string // "WxH", "Wx", "xH"
	Crop    string // "center" (default), "top", "fit"
	Format  string // "" (keep source format), "jpeg", "png"
	Quality int    // 0 = default, else 1-100 (JPEG encode quality)
}

// ApplyTransform validates and applies optional output transform parameters
// (?format= and ?quality= on the file download URL).
func (o *ThumbOptions) ApplyTransform(format, quality string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
	case "jpg", "jpeg":
		o.Format = "jpeg"
	case "png":
		o.Format = "png"
	default:
		return fmt.Errorf("unsupported format %q (supported: jpeg, png)", format)
	}
	if q := strings.TrimSpace(quality); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 100 {
			return fmt.Errorf("quality must be an integer between 1 and 100")
		}
		o.Quality = n
	}
	return nil
}

// ParseThumb parses a thumb parameter: "100x100" (center
// crop), "100x100t" (top crop), "100x100f" (fit, no crop), "100x300".
func ParseThumb(raw string) (ThumbOptions, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	opts := ThumbOptions{Crop: "center"}
	if raw == "" {
		return opts, fmt.Errorf("empty thumb size")
	}
	switch raw[len(raw)-1] {
	case 't':
		opts.Crop = "top"
		raw = raw[:len(raw)-1]
	case 'f':
		opts.Crop = "fit"
		raw = raw[:len(raw)-1]
	case 'b':
		// Bottom crop is accepted for compatibility but anchors
		// center: the resizer supports center/top anchors only.
		opts.Crop = "center"
		raw = raw[:len(raw)-1]
	}
	w, h, err := parseSize(raw)
	if err != nil {
		return opts, err
	}
	if w <= 0 && h <= 0 {
		return opts, fmt.Errorf("invalid thumb size %q", raw)
	}
	if w > maxThumbDimension || h > maxThumbDimension {
		return opts, fmt.Errorf("thumb size exceeds %dpx limit", maxThumbDimension)
	}
	opts.Size = raw
	return opts, nil
}

// Thumb returns a thumbnail for the stored image at originalPath, generating
// and caching it on first request. Concurrency is capped by a semaphore so a
// burst of cold thumb requests cannot exhaust memory; cached variants are
// served without taking a slot.
func (s *Service) Thumb(ctx context.Context, originalPath string, opts ThumbOptions, proc *ImageProcessor) ([]byte, string, error) {
	cachePath := thumbCachePath(originalPath, opts)

	if data, info, err := s.Download(ctx, cachePath); err == nil {
		return data, info.MimeType, nil
	}

	select {
	case s.thumbSem <- struct{}{}:
		defer func() { <-s.thumbSem }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}

	// Re-check after acquiring: another request may have generated it.
	if data, info, err := s.Download(ctx, cachePath); err == nil {
		return data, info.MimeType, nil
	}

	original, _, err := s.Download(ctx, originalPath)
	if err != nil {
		return nil, "", err
	}

	crop := opts.Crop
	if crop == "fit" {
		crop = ""
	}
	data, format, err := proc.ThumbnailWithFormat(bytes.NewReader(original), opts.Size, crop, opts.Format, opts.Quality)
	if err != nil {
		return nil, "", err
	}

	mimeType := proc.MimeType(format)
	// Cache failures are non-fatal: the thumb is still served, just not cached.
	_ = s.putAtPath(cachePath, data, mimeType)

	return data, mimeType, nil
}

// DeleteThumbs removes all cached thumbnail variants for a stored file.
func (s *Service) DeleteThumbs(ctx context.Context, originalPath string) error {
	dir, base := pathpkg.Split(originalPath)
	return s.DeletePrefix(ctx, pathpkg.Join(thumbPrefix, dir, base))
}

func thumbCachePath(originalPath string, opts ThumbOptions) string {
	dir, base := pathpkg.Split(originalPath)
	variant := opts.Size + "_" + opts.Crop
	if opts.Format != "" {
		variant += "_" + opts.Format
	}
	if opts.Quality > 0 {
		variant += fmt.Sprintf("_q%d", opts.Quality)
	}
	variant += "_" + base
	return pathpkg.Join(thumbPrefix, dir, base, variant)
}
