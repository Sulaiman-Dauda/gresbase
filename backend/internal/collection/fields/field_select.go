package fields

import (
	"fmt"
	"strings"
)

type SelectField struct {
	BaseField
	Values  []string `json:"values,omitempty"`
	MaxSelect int    `json:"max_select,omitempty"`
	Multiple bool    `json:"multiple,omitempty"`
}

func NewSelectField(name string) *SelectField {
	return &SelectField{BaseField: NewBaseField(name, TypeSelect)}
}

func (f *SelectField) PGType() string { return "TEXT" }
func (f *SelectField) PGDefault() string { return "" }
func (f *SelectField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef("TEXT", "")
}

func (f *SelectField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}

	// Single value
	if !f.Multiple {
		s := fmt.Sprintf("%v", raw)
		s = strings.TrimSpace(s)
		if s == "" {
			if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
			return nil, nil
		}
		if len(f.Values) > 0 && !containsString(f.Values, s) {
			return nil, fmt.Errorf("field %q: value %q not in allowed values: %v", f.name, s, f.Values)
		}
		return s, nil
	}

	// Multiple values
	var values []string
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			values = append(values, fmt.Sprintf("%v", item))
		}
	case []string:
		values = v
	case string:
		values = strings.Split(v, ",")
	default:
		return nil, fmt.Errorf("field %q: expected array of values", f.name)
	}

	if len(values) == 0 {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}

	if f.MaxSelect > 0 && len(values) > f.MaxSelect {
		return nil, fmt.Errorf("field %q: max %d selections allowed, got %d", f.name, f.MaxSelect, len(values))
	}

	seen := make(map[string]bool)
	cleaned := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" { continue }
		if len(f.Values) > 0 && !containsString(f.Values, v) {
			return nil, fmt.Errorf("field %q: value %q not in allowed values", f.name, v)
		}
		if seen[v] { continue }
		seen[v] = true
		cleaned = append(cleaned, v)
	}
	return cleaned, nil
}

func (f *SelectField) Marshal(value any) (any, error) {
	v, err := f.Validate(value)
	if err != nil { return nil, err }
	if f.Multiple {
		if arr, ok := v.([]string); ok {
			return strings.Join(arr, ","), nil
		}
	}
	return v, nil
}

func (f *SelectField) Unmarshal(raw any) (any, error) {
	if raw == nil { return nil, nil }
	s := fmt.Sprintf("%v", raw)
	s = strings.TrimSpace(s)
	if s == "" { return nil, nil }
	if f.Multiple {
		return strings.Split(s, ","), nil
	}
	return s, nil
}

func (f *SelectField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}

func (f *SelectField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["values"]; ok {
		if arr, ok := v.([]any); ok {
			f.Values = make([]string, len(arr))
			for i, a := range arr { f.Values[i] = fmt.Sprintf("%v", a) }
		}
	}
	if v, ok := opts["max_select"]; ok {
		if n, ok := toInt(v); ok { f.MaxSelect = n }
	}
	if v, ok := opts["multiple"]; ok {
		f.Multiple, _ = v.(bool)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle { return true }
	}
	return false
}
