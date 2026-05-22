package search

import (
	"testing"
)

func TestNewProvider(t *testing.T) {
	p := NewProvider(nil)
	if p == nil {
		t.Fatal("expected non-nil search provider")
	}
}

func TestBuildTSQuery(t *testing.T) {
	p := NewProvider(nil)

	tests := []struct {
		input    string
		contains string
	}{
		{"hello world", "hello & world"},
		{"test", "test"},
		{"hello OR world", "hello | world"},
		{"-excluded term", "!excluded & term"},
		{"", ""},
	}

	for _, tt := range tests {
		result := p.buildTSQuery(tt.input)
		if result != tt.contains {
			t.Errorf("buildTSQuery(%q) = %q, want %q", tt.input, result, tt.contains)
		}
	}
}

func TestSearchQueryStruct(t *testing.T) {
	q := SearchQuery{
		Collection: "posts",
		Query:      "hello world",
		Language:   "english",
		Page:       1,
		PerPage:    20,
		Highlight:  true,
		Rank:       true,
	}

	if q.Collection != "posts" {
		t.Error("collection mismatch")
	}
	if q.Page != 1 {
		t.Error("page should default to 1")
	}
	if !q.Rank {
		t.Error("rank should be enabled")
	}
}
