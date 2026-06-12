package storage

import "testing"

func TestParseThumb(t *testing.T) {
	cases := []struct {
		in       string
		wantSize string
		wantCrop string
		wantErr  bool
	}{
		{"100x100", "100x100", "center", false},
		{"100x100t", "100x100", "top", false},
		{"100x100f", "100x100", "fit", false},
		{"100x100b", "100x100", "center", false},
		{"200x", "200x", "center", false},
		{"x300", "x300", "center", false},
		{"", "", "", true},
		{"abc", "", "", true},
		{"0x0", "", "", true},
		{"9999x100", "", "", true}, // exceeds maxThumbDimension
	}
	for _, tc := range cases {
		opts, err := ParseThumb(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseThumb(%q): expected error, got %+v", tc.in, opts)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseThumb(%q): unexpected error %v", tc.in, err)
			continue
		}
		if opts.Size != tc.wantSize || opts.Crop != tc.wantCrop {
			t.Errorf("ParseThumb(%q) = %+v, want size=%q crop=%q", tc.in, opts, tc.wantSize, tc.wantCrop)
		}
	}
}

func TestThumbCachePath(t *testing.T) {
	got := thumbCachePath("posts/rec1/photo.jpg", ThumbOptions{Size: "100x100", Crop: "center"})
	want := "_thumbs/posts/rec1/photo.jpg/100x100_center_photo.jpg"
	if got != want {
		t.Errorf("thumbCachePath = %q, want %q", got, want)
	}
}
