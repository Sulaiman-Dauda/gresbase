package filter

import (
	"strings"
	"testing"
)

// TestParseFilter tests the tokenizer and parser.
func TestParseFilter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple eq", `title = "hello"`, false},
		{"simple neq", `status != "deleted"`, false},
		{"gt number", `age > 18`, false},
		{"gte number", `age >= 21`, false},
		{"lt number", `price < 100`, false},
		{"lte number", `price <= 50`, false},
		{"like contains", `name ~ "john"`, false},
		{"nlike not contains", `title !~ "spam"`, false},
		{"in array", `tags ?= "go"`, false},
		{"nin not in array", `tags ?!= "rust"`, false},
		{"and two conditions", `status = "active" && age > 18`, false},
		{"or two conditions", `role = "admin" || role = "mod"`, false},
		{"paren group", `(status = "active" || status = "pending") && age > 18`, false},
		{"not unary", `!deleted = true`, false},
		{"null value", `deleted = null`, false},
		{"bool true", `active = true`, false},
		{"bool false", `published = false`, false},
		{"empty string", ``, false},
		{"whitespace only", `   `, false},
		{"nested parens", `((a = 1) && (b = 2)) || c = 3`, false},
		{"complex", `status = "active" && (role = "admin" || role ~ "mod") && !deleted = true`, false},
		{"invalid operator", `title == "x"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := ParseFilter(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseFilter(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseFilter(%q) unexpected error: %v", tt.input, err)
				return
			}
			if tt.input != "" && strings.TrimSpace(tt.input) != "" && expr == nil {
				t.Errorf("ParseFilter(%q) returned nil expression", tt.input)
			}
			t.Logf("Parsed: %s => %s", tt.input, expr)
		})
	}
}

func TestSQLBuilder_Build(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantSQL string
	}{
		{
			name:    "simple eq",
			filter:  `title = "hello"`,
			wantSQL: `"title" = $1`,
		},
		{
			name:    "and conditions",
			filter:  `status = "active" && age > 18`,
			wantSQL: `("status" = $1 AND "age" > $2)`,
		},
		{
			name:    "like contains",
			filter:  `name ~ "john"`,
			wantSQL: `"name" ILIKE $1`,
		},
		{
			name:    "is null",
			filter:  `deleted = null`,
			wantSQL: `"deleted" IS NULL`,
		},
		{
			name:    "is not null",
			filter:  `deleted != null`,
			wantSQL: `"deleted" IS NOT NULL`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := ParseFilter(tt.filter)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			sql, params, err := FilterToSQL(expr, nil)
			if err != nil {
				t.Fatalf("Build error: %v", err)
			}

			if sql != tt.wantSQL {
				t.Errorf("SQL mismatch:\n  got:  %s\n  want: %s", sql, tt.wantSQL)
			}

			t.Logf("SQL: %s | Params: %v", sql, params)
		})
	}
}

func TestFilterMatches(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		record map[string]any
		want   bool
	}{
		{
			name:   "field equals",
			filter: `status = "active"`,
			record: map[string]any{"status": "active"},
			want:   true,
		},
		{
			name:   "field not equals",
			filter: `status != "deleted"`,
			record: map[string]any{"status": "active"},
			want:   true,
		},
		{
			name:   "number comparison",
			filter: `age >= 18`,
			record: map[string]any{"age": 25},
			want:   true,
		},
		{
			name:   "number comparison fails",
			filter: `age >= 18`,
			record: map[string]any{"age": 15},
			want:   false,
		},
		{
			name:   "string contains",
			filter: `name ~ "john"`,
			record: map[string]any{"name": "John Doe"},
			want:   true,
		},
		{
			name:   "string not contains",
			filter: `name !~ "john"`,
			record: map[string]any{"name": "Alice"},
			want:   true,
		},
		{
			name:   "and true",
			filter: `status = "active" && age > 18`,
			record: map[string]any{"status": "active", "age": 25},
			want:   true,
		},
		{
			name:   "and false",
			filter: `status = "active" && age > 18`,
			record: map[string]any{"status": "active", "age": 15},
			want:   false,
		},
		{
			name:   "or first true",
			filter: `role = "admin" || role = "mod"`,
			record: map[string]any{"role": "admin"},
			want:   true,
		},
		{
			name:   "or second true",
			filter: `role = "admin" || role = "mod"`,
			record: map[string]any{"role": "mod"},
			want:   true,
		},
		{
			name:   "or false",
			filter: `role = "admin" || role = "mod"`,
			record: map[string]any{"role": "user"},
			want:   false,
		},
		{
			name:   "nil filter",
			filter: ``,
			record: map[string]any{"foo": "bar"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := ParseFilter(tt.filter)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			got, err := FilterMatches(expr, tt.record)
			if err != nil {
				t.Fatalf("Evaluate error: %v", err)
			}

			if got != tt.want {
				t.Errorf("FilterMatches(%q, %v) = %v, want %v", tt.filter, tt.record, got, tt.want)
			}
		})
	}
}

func TestFilterMatchesWithResolver(t *testing.T) {
	expr, err := ParseFilter(`owner = @request.auth.id && @request.auth.role = "member"`)
	if err != nil {
		t.Fatalf("ParseFilter error: %v", err)
	}

	record := map[string]any{"owner": "rec_123"}
	resolver := func(key string) (any, error) {
		switch key {
		case "@request.auth.id":
			return "rec_123", nil
		case "@request.auth.role":
			return "member", nil
		default:
			return nil, nil
		}
	}

	matched, err := FilterMatchesWithResolver(expr, record, resolver)
	if err != nil {
		t.Fatalf("FilterMatchesWithResolver error: %v", err)
	}
	if !matched {
		t.Fatal("expected rule with right-side resolver values to match")
	}
}

func TestValidateFields(t *testing.T) {
	expr, _ := ParseFilter(`status = "active" && age > 18`)

	err := ValidateFields(expr, []string{"status", "age", "name"})
	if err != nil {
		t.Errorf("Valid fields should pass: %v", err)
	}

	err = ValidateFields(expr, []string{"status"})
	if err == nil {
		t.Error("Missing field 'age' should cause error")
	}
}

func TestCombineRules(t *testing.T) {
	result := CombineRules(`"status" = $1`, `"age" > $2`, `TRUE`, ``)
	expected := `("status" = $1) AND ("age" > $2)`
	if result != expected {
		t.Errorf("CombineRules = %q, want %q", result, expected)
	}
}

func TestBuildRuleSQL(t *testing.T) {
	sql, params, err := BuildRuleSQL(`status = "active"`, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(params) != 1 {
		t.Errorf("Expected 1 param, got %d", len(params))
	}
	if params[0] != "active" {
		t.Errorf("Expected param 'active', got %v", params[0])
	}
	t.Logf("SQL: %s, Params: %v", sql, params)
}
