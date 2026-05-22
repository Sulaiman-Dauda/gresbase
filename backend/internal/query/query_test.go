package query

import (
	"testing"
)

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	if p.Page != 1 {
		t.Errorf("expected page 1, got %d", p.Page)
	}
	if p.PerPage != 30 {
		t.Errorf("expected perPage 30, got %d", p.PerPage)
	}
}

func TestParseParams(t *testing.T) {
	tests := []struct {
		raw       string
		wantPage  int
		wantPerPage int
		wantFilter string
		wantSort   string
		wantExpand string
		wantFields string
	}{
		{"", 1, 30, "", "", "", ""},
		{"page=2&perPage=50", 2, 50, "", "", "", ""},
		{"filter=status%3D%22active%22&sort=-created", 1, 30, "status%3D%22active%22", "-created", "", ""},
		{"expand=author,category&fields=id,title", 1, 30, "", "", "author,category", "id,title"},
		{"perPage=1000", 1, 500, "", "", "", ""}, // capped at 500
		{"page=0", 1, 30, "", "", "", ""}, // page 0 becomes 1
		{"skipTotal=1", 1, 30, "", "", "", ""},
	}

	for _, tt := range tests {
		p := ParseParams(tt.raw)
		if p.Page != tt.wantPage {
			t.Errorf("ParseParams(%q).page = %d, want %d", tt.raw, p.Page, tt.wantPage)
		}
		if p.PerPage != tt.wantPerPage {
			t.Errorf("ParseParams(%q).perPage = %d, want %d", tt.raw, p.PerPage, tt.wantPerPage)
		}
		if p.Filter != tt.wantFilter {
			t.Errorf("ParseParams(%q).filter = %q, want %q", tt.raw, p.Filter, tt.wantFilter)
		}
		if p.Sort != tt.wantSort {
			t.Errorf("ParseParams(%q).sort = %q, want %q", tt.raw, p.Sort, tt.wantSort)
		}
	}
}

func TestNewListResponse(t *testing.T) {
	items := []map[string]any{
		{"id": "1", "name": "Item 1"},
		{"id": "2", "name": "Item 2"},
	}

	resp := NewListResponse(items, 42, 1, 20)
	if resp.Page != 1 {
		t.Errorf("expected page 1, got %d", resp.Page)
	}
	if resp.TotalItems != 42 {
		t.Errorf("expected 42 items, got %d", resp.TotalItems)
	}
	if resp.TotalPages != 3 { // ceil(42/20) = 3
		t.Errorf("expected 3 pages, got %d", resp.TotalPages)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Items))
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"users", `"users"`},
		{"my_table", `"my_table"`},
		{`has"quote`, `"has""quote"`},
	}

	for _, tt := range tests {
		result := QuoteIdent(tt.input)
		if result != tt.expected {
			t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestNewBuilder(t *testing.T) {
	b := NewBuilder(nil, nil)
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
	if b.params.Page != 1 {
		t.Errorf("expected default page 1, got %d", b.params.Page)
	}
}
