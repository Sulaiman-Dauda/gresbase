package storage

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"

	// Register image decoders
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// ImageProcessor provides image manipulation utilities for file storage.
type ImageProcessor struct {
	jpegQuality int
}

// NewImageProcessor creates an image processor with default quality (85).
func NewImageProcessor() *ImageProcessor {
	return &ImageProcessor{jpegQuality: 85}
}

// SetJPEGQuality sets the JPEG quality for processed images (1-100).
func (p *ImageProcessor) SetJPEGQuality(quality int) {
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	p.jpegQuality = quality
}

// ThumbnailRequest describes a thumbnail generation request.
type ThumbnailRequest struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Format  string `json:"format,omitempty"` // "jpeg", "png", "" = auto
	Quality int    `json:"quality,omitempty"`
	Crop    string `json:"crop,omitempty"` // "center", "top", ""
}

// Thumbnail generates a thumbnail from an image reader.
// The size string format is "WxH" e.g. "100x100", "200x", "x300".
func (p *ImageProcessor) Thumbnail(r io.Reader, size string, crop string) ([]byte, string, error) {
	return p.ThumbnailWithFormat(r, size, crop, "", 0)
}

// ThumbnailWithFormat generates a thumbnail and optionally re-encodes it to
// outFormat ("jpeg" or "png", "" keeps the source format) at the given JPEG
// quality (0 uses the processor default).
func (p *ImageProcessor) ThumbnailWithFormat(r io.Reader, size, crop, outFormat string, quality int) ([]byte, string, error) {
	width, height, err := parseSize(size)
	if err != nil {
		return nil, "", err
	}

	src, format, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}

	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	if width <= 0 && height <= 0 {
		width = srcW
		height = srcH
	} else if width <= 0 {
		width = int(float64(srcW) * float64(height) / float64(srcH))
	} else if height <= 0 {
		height = int(float64(srcH) * float64(width) / float64(srcW))
	}

	var dst *image.RGBA

	switch crop {
	case "center", "top":
		dst = p.cropResize(src, srcBounds, width, height, crop)
	default:
		dst = p.fitResize(src, srcBounds, width, height)
	}

	target := strings.ToLower(outFormat)
	if target == "" {
		target = strings.ToLower(format)
	}
	if target == "jpg" {
		target = "jpeg"
	}
	q := p.jpegQuality
	if quality >= 1 && quality <= 100 {
		q = quality
	}

	var buf bytes.Buffer
	switch target {
	case "jpeg":
		jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q})
		format = "jpeg"
	case "png":
		png.Encode(&buf, dst)
		format = "png"
	default:
		// Default to JPEG for formats without an encoder (gif, webp sources)
		jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q})
		format = "jpeg"
	}

	return buf.Bytes(), format, nil
}

// Resize resizes an image to exact dimensions.
func (p *ImageProcessor) Resize(r io.Reader, width, height int) ([]byte, string, error) {
	src, format, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if strings.ToLower(format) == "png" {
		png.Encode(&buf, dst)
	} else {
		jpeg.Encode(&buf, dst, &jpeg.Options{Quality: p.jpegQuality})
		format = "jpeg"
	}

	return buf.Bytes(), format, nil
}

// GetImageDimensions returns the width and height of an image.
func (p *ImageProcessor) GetImageDimensions(r io.Reader) (int, int, string, error) {
	cfg, format, err := image.DecodeConfig(r)
	if err != nil {
		return 0, 0, "", err
	}
	return cfg.Width, cfg.Height, format, nil
}

// MimeType returns the MIME type for an image format.
func (p *ImageProcessor) MimeType(format string) string {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// Extension returns the file extension for an image format.
func (p *ImageProcessor) Extension(format string) string {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return ".jpg"
	case "png":
		return ".png"
	case "gif":
		return ".gif"
	case "webp":
		return ".webp"
	default:
		return filepath.Ext(format)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (p *ImageProcessor) fitResize(src image.Image, srcBounds image.Rectangle, width, height int) *image.RGBA {
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	// Calculate fit dimensions maintaining aspect ratio
	ratio := float64(width) / float64(height)
	srcRatio := float64(srcW) / float64(srcH)

	var dstW, dstH int
	if srcRatio > ratio {
		dstW = width
		dstH = int(float64(width) / srcRatio)
	} else {
		dstH = height
		dstW = int(float64(height) * srcRatio)
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, srcBounds, draw.Over, nil)
	return dst
}

func (p *ImageProcessor) cropResize(src image.Image, srcBounds image.Rectangle, width, height int, anchor string) *image.RGBA {
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	// Calculate crop rectangle
	cropRatio := float64(width) / float64(height)
	srcRatio := float64(srcW) / float64(srcH)

	var cropX, cropY, cropW, cropH int

	if srcRatio > cropRatio {
		// Source is wider — crop width
		cropH = srcH
		cropW = int(float64(srcH) * cropRatio)
		cropY = 0
		if anchor == "top" {
			cropX = 0
		} else {
			cropX = (srcW - cropW) / 2 // center
		}
	} else {
		// Source is taller — crop height
		cropW = srcW
		cropH = int(float64(srcW) / cropRatio)
		cropX = 0
		if anchor == "top" {
			cropY = 0
		} else {
			cropY = (srcH - cropH) / 2 // center
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	cropRect := image.Rect(cropX, cropY, cropX+cropW, cropY+cropH)

	// First crop, then scale
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)
	return dst
}

func parseSize(size string) (int, int, error) {
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size format: %s (expected WxH)", size)
	}

	var width, height int
	if parts[0] != "" {
		fmt.Sscanf(parts[0], "%d", &width)
	}
	if parts[1] != "" {
		fmt.Sscanf(parts[1], "%d", &height)
	}

	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}

	return width, height, nil
}
