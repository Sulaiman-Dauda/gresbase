package storage

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestApplyTransform(t *testing.T) {
	cases := []struct {
		format, quality string
		wantFormat      string
		wantQuality     int
		wantErr         bool
	}{
		{"", "", "", 0, false},
		{"jpeg", "80", "jpeg", 80, false},
		{"jpg", "", "jpeg", 0, false},
		{"png", "", "png", 0, false},
		{"webp", "", "", 0, true},
		{"jpeg", "0", "", 0, true},
		{"jpeg", "101", "", 0, true},
		{"jpeg", "abc", "", 0, true},
	}
	for _, tc := range cases {
		opts := ThumbOptions{Size: "100x100", Crop: "center"}
		err := opts.ApplyTransform(tc.format, tc.quality)
		if tc.wantErr {
			if err == nil {
				t.Errorf("format=%q quality=%q: expected error", tc.format, tc.quality)
			}
			continue
		}
		if err != nil {
			t.Errorf("format=%q quality=%q: unexpected error %v", tc.format, tc.quality, err)
			continue
		}
		if opts.Format != tc.wantFormat || opts.Quality != tc.wantQuality {
			t.Errorf("format=%q quality=%q: got (%q,%d), want (%q,%d)",
				tc.format, tc.quality, opts.Format, opts.Quality, tc.wantFormat, tc.wantQuality)
		}
	}
}

func TestThumbCachePathVariants(t *testing.T) {
	base := thumbCachePath("posts/rec1/photo.png", ThumbOptions{Size: "100x100", Crop: "center"})
	transformed := thumbCachePath("posts/rec1/photo.png", ThumbOptions{Size: "100x100", Crop: "center", Format: "jpeg", Quality: 80})

	if base == transformed {
		t.Fatalf("transform variants must not share a cache path: %s", base)
	}
	if !strings.Contains(transformed, "jpeg") || !strings.Contains(transformed, "q80") {
		t.Fatalf("transformed cache path should encode format and quality: %s", transformed)
	}
	if !strings.HasPrefix(base, thumbPrefix) {
		t.Fatalf("cache path must stay under %s: %s", thumbPrefix, base)
	}
}

func TestThumbnailWithFormatConvertsPNGToJPEG(t *testing.T) {
	var src bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	if err := png.Encode(&src, img); err != nil {
		t.Fatalf("encode source: %v", err)
	}

	proc := NewImageProcessor()
	data, format, err := proc.ThumbnailWithFormat(bytes.NewReader(src.Bytes()), "20x20", "center", "jpeg", 70)
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg output, got %q", format)
	}
	// JPEG SOI marker
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("output is not a JPEG stream")
	}

	// Without an explicit format the source format is kept.
	data, format, err = proc.ThumbnailWithFormat(bytes.NewReader(src.Bytes()), "20x20", "center", "", 0)
	if err != nil {
		t.Fatalf("thumbnail keep-format: %v", err)
	}
	if format != "png" {
		t.Fatalf("expected png passthrough, got %q", format)
	}
	if len(data) < 8 || data[0] != 0x89 || data[1] != 'P' {
		t.Fatalf("output is not a PNG stream")
	}
}
