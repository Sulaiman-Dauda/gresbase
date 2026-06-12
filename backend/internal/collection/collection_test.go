package collection_test

import (
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
)

func TestFieldTypeToPGType(t *testing.T) {
	tests := []struct {
		fieldType collection.FieldType
		expected  string
	}{
		{collection.FieldText, "TEXT"},
		{collection.FieldNumber, "DOUBLE PRECISION"},
		{collection.FieldBool, "BOOLEAN"},
		{collection.FieldEmail, "TEXT"},
		{collection.FieldURL, "TEXT"},
		{collection.FieldDate, "TIMESTAMPTZ"},
		{collection.FieldSelect, "TEXT"},
		{collection.FieldJSON, "JSONB"},
		{collection.FieldFile, "JSONB"},
		{collection.FieldRelation, "TEXT"},
		{collection.FieldPassword, "TEXT"},
		{collection.FieldEditor, "TEXT"},
		{collection.FieldGeoPoint, "JSONB"},
		{collection.FieldAutoDate, "TIMESTAMPTZ"},
	}

	svc := collection.NewService(nil)

	for _, tt := range tests {
		t.Run(string(tt.fieldType), func(t *testing.T) {
			result := svc.FieldToPGTypeExport(tt.fieldType)
			if result != tt.expected {
				t.Errorf("Expected %s for %s, got %s", tt.expected, tt.fieldType, result)
			}
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	svc := collection.NewService(nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"test", `"test"`},
		{"my_table", `"my_table"`},
		{"MixedCase", `"MixedCase"`},
		{`has"quote`, `"has""quote"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := svc.QuoteIdentExport(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildColumnDefs(t *testing.T) {
	svc := collection.NewService(nil)

	fields := []collection.SchemaField{
		{Name: "title", Type: collection.FieldText, Required: true},
		{Name: "count", Type: collection.FieldNumber},
		{Name: "active", Type: collection.FieldBool},
	}

	columns := svc.BuildColumnDefsExport(fields)

	if len(columns) < 5 {
		t.Errorf("Expected at least 5 columns (id + fields + created_at + updated_at), got %d", len(columns))
	}

	// Check id column
	if columns[0] != `id TEXT PRIMARY KEY DEFAULT gen_random_uuid()` {
		t.Errorf("Unexpected id column: %s", columns[0])
	}
}

func TestFieldsEqual(t *testing.T) {
	a := collection.SchemaField{Name: "x", Type: collection.FieldText, Required: true, Unique: false}
	b := collection.SchemaField{Name: "x", Type: collection.FieldText, Required: true, Unique: false}
	c := collection.SchemaField{Name: "x", Type: collection.FieldNumber, Required: true, Unique: false}
	d := collection.SchemaField{Name: "x", Type: collection.FieldText, Required: false, Unique: false}

	if !collection.FieldsEqualExport(a, b) {
		t.Error("Identical fields should be equal")
	}
	if collection.FieldsEqualExport(a, c) {
		t.Error("Different type should not be equal")
	}
	if collection.FieldsEqualExport(a, d) {
		t.Error("Different required should not be equal")
	}
}

func TestValidateRuleExpression(t *testing.T) {
	tests := []struct {
		rule    string
		isValid bool
	}{
		{"", true},
		{"published = true", true},
		{"@request.auth.role = 'admin'", true},
		{"count > 5 AND status = 'active'", true},
		{"count > 5 && status = 'active'", true},
		{"unbalanced 'quote", false},
		{"unbalanced (paren", false},
		{`"double" 'single'`, false}, // two bare literals are not a comparison
	}

	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			err := collection.ValidateRuleExpression(tt.rule)
			if tt.isValid && err != nil {
				t.Errorf("Expected valid, got error: %v", err)
			}
			if !tt.isValid && err == nil {
				t.Errorf("Expected error for invalid rule: %q", tt.rule)
			}
		})
	}
}

func TestValidateCollectionDefinition(t *testing.T) {
	svc := collection.NewService(nil)

	t.Run("rejects invalid identifiers", func(t *testing.T) {
		coll := &collection.Collection{
			Name:   "bad-name",
			Type:   collection.TypeBase,
			Schema: []collection.SchemaField{{Name: "title", Type: collection.FieldText}},
		}
		if err := svc.ValidateCollectionDefinition(coll); err == nil {
			t.Fatal("expected invalid collection name error")
		}
	})

	t.Run("rejects dangerous view queries", func(t *testing.T) {
		coll := &collection.Collection{
			Name:      "reports",
			Type:      collection.TypeView,
			ViewQuery: "SELECT 1; DROP TABLE users;",
		}
		if err := svc.ValidateCollectionDefinition(coll); err == nil {
			t.Fatal("expected invalid view_query error")
		}
	})

	t.Run("rejects invalid defaults", func(t *testing.T) {
		coll := &collection.Collection{
			Name: "posts",
			Type: collection.TypeBase,
			Schema: []collection.SchemaField{{
				Name:    "published",
				Type:    collection.FieldBool,
				Options: map[string]any{"default": "yes"},
			}},
		}
		err := svc.ValidateCollectionDefinition(coll)
		if err == nil || !strings.Contains(err.Error(), "default") {
			t.Fatalf("expected default validation error, got %v", err)
		}
	})
}

func TestValidateRecordAndPatch(t *testing.T) {
	svc := collection.NewService(nil)
	coll := &collection.Collection{
		Name: "posts",
		Type: collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
			{Name: "published", Type: collection.FieldBool},
		},
	}

	if err := svc.ValidateRecord(coll, map[string]any{"published": true}); err == nil {
		t.Fatal("expected create validation to require title")
	}

	if err := svc.ValidateRecordPatch(coll, map[string]any{"published": true}); err != nil {
		t.Fatalf("expected partial validation to allow missing required fields, got %v", err)
	}

	if err := svc.ValidateRecordPatch(coll, map[string]any{"unknown": true}); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}

	if err := svc.ValidateRecordPatch(coll, map[string]any{"created_at": "hack"}); err == nil {
		t.Fatal("expected system field to be rejected")
	}
}
