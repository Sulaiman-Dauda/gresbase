package collection

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

// ValidateRecord validates all fields in a record against the collection schema.
func (s *Service) ValidateRecord(coll *Collection, data map[string]any) error {
	for _, field := range coll.Schema {
		if field.System {
			continue
		}

		val, exists := data[field.Name]
		if !exists && field.Required {
			return fmt.Errorf("field %q is required", field.Name)
		}
		if !exists {
			continue
		}

		// Type-specific validation
		if err := validateField(field, val); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}

		// Unique constraint
		if field.Unique && val != nil && val != "" {
			// Unique check done at DB level via unique index
		}
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
func ValidateRuleExpression(rule string) error {
	if rule == "" {
		return nil
	}

	// Basic syntax validation — check for balanced quotes and parentheses
	if strings.Count(rule, "'")%2 != 0 {
		return fmt.Errorf("unbalanced single quotes in rule")
	}
	if strings.Count(rule, "\"")%2 != 0 {
		return fmt.Errorf("unbalanced double quotes in rule")
	}
	if strings.Count(rule, "(") != strings.Count(rule, ")") {
		return fmt.Errorf("unbalanced parentheses in rule")
	}

	return nil
}
