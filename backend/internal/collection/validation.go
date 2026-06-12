package collection

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/filter"
)

var reservedSystemFields = map[string]struct{}{
	"id":         {},
	"created_at": {},
	"updated_at": {},
}

// ValidateCollectionDefinition validates collection names, field names, rules and
// view queries before any schema mutation is attempted.
func (s *Service) ValidateCollectionDefinition(coll *Collection) error {
	if coll == nil {
		return fmt.Errorf("collection is required")
	}

	if err := database.ValidateIdentifier(coll.Name, "collection name"); err != nil {
		return err
	}

	for _, rule := range []struct {
		name string
		val  *string
	}{
		{"list_rule", coll.ListRule},
		{"view_rule", coll.ViewRule},
		{"create_rule", coll.CreateRule},
		{"update_rule", coll.UpdateRule},
		{"delete_rule", coll.DeleteRule},
	} {
		if rule.val == nil {
			continue // locked (superusers only)
		}
		if err := ValidateRuleExpression(*rule.val); err != nil {
			return fmt.Errorf("invalid %s: %w", rule.name, err)
		}
	}

	if coll.Type == TypeView {
		if err := validateViewQuery(coll.ViewQuery); err != nil {
			return err
		}
	}

	seen := make(map[string]struct{}, len(coll.Schema))
	hasAuthIdentity := false

	for _, field := range coll.Schema {
		if err := database.ValidateIdentifier(field.Name, "field name"); err != nil {
			return err
		}

		key := strings.ToLower(field.Name)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate field name %q", field.Name)
		}
		seen[key] = struct{}{}

		if _, reserved := reservedSystemFields[field.Name]; reserved {
			return fmt.Errorf("field name %q is reserved", field.Name)
		}
		if coll.Type == TypeAuth && field.Name == "verified" {
			return fmt.Errorf("field name %q is reserved for auth collections", field.Name)
		}

		if _, err := s.defaultSQLLiteral(field); err != nil {
			return err
		}

		if coll.Type == TypeAuth && (field.Type == FieldEmail || field.Name == "username") {
			hasAuthIdentity = true
		}
	}

	if coll.Type == TypeAuth && !hasAuthIdentity {
		return fmt.Errorf("auth collections require at least an email or username field")
	}

	return nil
}

// ValidateRecord validates a full new record payload against the collection schema.
func (s *Service) ValidateRecord(coll *Collection, data map[string]any) error {
	return s.validateRecord(coll, data, false)
}

// ValidateRecordPatch validates a partial update payload.
func (s *Service) ValidateRecordPatch(coll *Collection, data map[string]any) error {
	return s.validateRecord(coll, data, true)
}

func (s *Service) validateRecord(coll *Collection, data map[string]any, partial bool) error {
	if coll == nil {
		return fmt.Errorf("collection is required")
	}

	allowed := make(map[string]SchemaField, len(coll.Schema))
	for _, field := range coll.Schema {
		if field.System {
			continue
		}
		allowed[field.Name] = field
	}

	for key := range data {
		if _, reserved := reservedSystemFields[key]; reserved {
			return fmt.Errorf("field %q is read-only", key)
		}
		if coll.Type == TypeAuth && key == "verified" {
			return fmt.Errorf("field %q is read-only", key)
		}
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}

	for _, field := range coll.Schema {
		if field.System {
			continue
		}

		val, exists := data[field.Name]
		if !exists {
			if !partial && field.Required {
				return fmt.Errorf("field %q is required", field.Name)
			}
			continue
		}

		if field.Required {
			if str, ok := val.(string); ok && strings.TrimSpace(str) == "" {
				return fmt.Errorf("field %q is required", field.Name)
			}
			if val == nil {
				return fmt.Errorf("field %q is required", field.Name)
			}
		}

		// Type-specific validation
		if err := validateField(field, val); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}

	return nil
}

func validateViewQuery(query string) error {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return fmt.Errorf("view collections require a view_query")
	}
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("view_query must be a single SELECT statement")
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT ") && !strings.HasPrefix(upper, "WITH ") {
		return fmt.Errorf("view_query must start with SELECT or WITH")
	}
	return nil
}

// validateField validates a single field value based on its type and options.
func validateField(field SchemaField, val any) error {
	if val == nil && !field.Required {
		return nil
	}

	opts := field.Options

	switch field.Type {
	case FieldText:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		if min, ok := opts["min"].(float64); ok && len(s) < int(min) {
			return fmt.Errorf("must be at least %d characters", int(min))
		}
		if max, ok := opts["max"].(float64); ok && len(s) > int(max) {
			return fmt.Errorf("must be at most %d characters", int(max))
		}
		if pattern, ok := opts["pattern"].(string); ok && pattern != "" {
			re, err := regexp.Compile(pattern)
			if err == nil && !re.MatchString(s) {
				return fmt.Errorf("does not match pattern %q", pattern)
			}
		}

	case FieldNumber:
		switch v := val.(type) {
		case float64:
			if min, ok := opts["min"].(float64); ok && v < min {
				return fmt.Errorf("must be >= %v", min)
			}
			if max, ok := opts["max"].(float64); ok && v > max {
				return fmt.Errorf("must be <= %v", max)
			}
		case int, int64:
			// coerce to float64
		default:
			return fmt.Errorf("expected number, got %T", val)
		}

	case FieldBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}

	case FieldEmail:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		if _, err := mail.ParseAddress(s); err != nil {
			return fmt.Errorf("invalid email address")
		}

	case FieldURL:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid URL")
		}

	case FieldSelect:
		values, ok := opts["values"].([]any)
		if !ok {
			return nil // No values constraint
		}
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		found := false
		for _, v := range values {
			if fmt.Sprint(v) == s {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("value %q is not in the allowed options", s)
		}

	case FieldJSON:
		// Any valid JSON value is fine
		return nil

	case FieldFile:
		// File validation done during upload
		return nil

	case FieldPassword:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		if len(s) < 8 {
			return fmt.Errorf("password must be at least 8 characters")
		}
		if max, ok := opts["max"].(float64); ok && len(s) > int(max) {
			return fmt.Errorf("password must be at most %d characters", int(max))
		}

	case FieldEditor:
		// HTML content, no specific validation
		return nil

	case FieldDate:
		switch val.(type) {
		case string, float64, int64:
			// Accept ISO 8601 strings or timestamps
			return nil
		default:
			return fmt.Errorf("expected date string or timestamp, got %T", val)
		}

	case FieldGeoPoint:
		// Expected: {"lat": 1.23, "lng": 4.56}
		return nil

	case FieldVector:
		dim := vectorDimensions(field)
		switch v := val.(type) {
		case string:
			// Accept pre-formatted "[1,2,3]" strings.
			if _, err := encodeVector(v); err != nil {
				return err
			}
		case []any:
			if dim > 0 && len(v) != dim {
				return fmt.Errorf("expected %d dimensions, got %d", dim, len(v))
			}
			for _, item := range v {
				switch item.(type) {
				case float64, float32, int, int64, json.Number:
				default:
					return fmt.Errorf("vector elements must be numbers")
				}
			}
		default:
			return fmt.Errorf("expected an array of numbers, got %T", val)
		}
		return nil

	case FieldAutoDate:
		return nil // Auto-managed

	case FieldRelation:
		// Single ID string or array of IDs
		switch v := val.(type) {
		case string:
			if v == "" && !field.Required {
				return nil
			}
		case []any:
			// Multiple relation
			return nil
		default:
			return fmt.Errorf("expected relation ID string or array, got %T", val)
		}
	}

	return nil
}

// ValidateRuleExpression parses and validates a collection rule expression.
// Rules use a filter-like syntax: "published = true", "@request.auth.role = 'admin'", etc.
// An empty rule means public access and is always valid.
func ValidateRuleExpression(rule string) error {
	if strings.TrimSpace(rule) == "" {
		return nil
	}

	if _, err := filter.ParseFilter(rule); err != nil {
		return fmt.Errorf("invalid rule expression: %w", err)
	}

	return nil
}
