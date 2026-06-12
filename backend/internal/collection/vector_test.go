package collection

import "testing"

func TestEncodeVector(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		want    string
		wantErr bool
	}{
		{"floats", []float64{1, 2.5, -3}, "[1,2.5,-3]", false},
		{"anys", []any{1.0, 2.0, 3.0}, "[1,2,3]", false},
		{"ints-as-any", []any{1, 2, 3}, "[1,2,3]", false},
		{"preformatted", "[1,2,3]", "[1,2,3]", false},
		{"empty-string", "", "", false},
		{"nil", nil, "", false},
		{"bad-string", "1,2,3", "", true},
		{"bad-type", 42, "", true},
		{"bad-element", []any{"x"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeVector(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVectorFieldOptions(t *testing.T) {
	f := SchemaField{
		Name:    "embedding",
		Type:    FieldVector,
		Options: map[string]any{"dimensions": float64(1536), "distance": "l2", "index": "ivfflat"},
	}
	if got := vectorDimensions(f); got != 1536 {
		t.Fatalf("dimensions = %d, want 1536", got)
	}
	if got := vectorDistanceOf(f); got != DistanceL2 {
		t.Fatalf("distance = %q, want l2", got)
	}
	if got := vectorIndexKind(f); got != "ivfflat" {
		t.Fatalf("index = %q, want ivfflat", got)
	}
	if got := vectorColumnType(f); got != "vector(1536)" {
		t.Fatalf("columnType = %q, want vector(1536)", got)
	}
}

func TestVectorDefaults(t *testing.T) {
	f := SchemaField{Name: "v", Type: FieldVector, Options: map[string]any{}}
	if got := vectorDistanceOf(f); got != DistanceCosine {
		t.Fatalf("default distance = %q, want cosine", got)
	}
	if got := vectorIndexKind(f); got != "hnsw" {
		t.Fatalf("default index = %q, want hnsw", got)
	}
	if got := vectorColumnType(f); got != "vector" {
		t.Fatalf("unsized columnType = %q, want vector", got)
	}
}

func TestVectorLen(t *testing.T) {
	if got := vectorLen("[1,2,3]"); got != 3 {
		t.Fatalf("vectorLen = %d, want 3", got)
	}
	if got := vectorLen("[]"); got != 0 {
		t.Fatalf("vectorLen empty = %d, want 0", got)
	}
}
