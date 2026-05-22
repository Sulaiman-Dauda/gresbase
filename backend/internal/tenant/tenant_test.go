package tenant

import (
	"testing"
)

func TestNewService(t *testing.T) {
	s := NewService(nil)
	if s == nil {
		t.Fatal("expected non-nil tenant service")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Company", "my-company"},
		{"Test", "test"},
		{"Hello World!", "hello-world"},
		{"UPPERCASE", "uppercase"},
		{"a@b.com", "abcom"},
		{"", "tenant"},
		{"   ", "---"},
		{"multi   spaces", "multi---spaces"},
		{"alice-bob", "alice-bob"},
	}

	for _, tt := range tests {
		result := slugify(tt.input)
		if result != tt.expected {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestTenantStruct(t *testing.T) {
	tenant := &Tenant{
		ID:     "t-1",
		Name:   "Test Corp",
		Slug:   "test-corp",
		Active: true,
	}

	if tenant.ID != "t-1" {
		t.Error("ID mismatch")
	}
	if tenant.Slug != "test-corp" {
		t.Error("slug mismatch")
	}
	if !tenant.Active {
		t.Error("tenant should be active")
	}
}
