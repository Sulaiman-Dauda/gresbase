package fields

import (
	"fmt"
	"strconv"
	"strings"
)

// TextField handles text/string values.
type TextField struct {
	BaseField
	MinLength int    `json:"min_length"`
	MaxLength int    `json:"max_length"`
	Pattern   string `json:"pattern,omitempty"`
}

// NewTextField creates a new text field.
func NewTextField(name string) *TextField {
	return &TextField{
		BaseField: NewBaseField(name, TypeText),
		MinLength: 0,
		MaxLength: 0,
	}
}

func (f *TextField) PGType() string {
	if f.MaxLength > 0 && f.MaxLength <= 10485760 {
		return fmt.Sprintf("VARCHAR(%d)", f.MaxLength)
	}
	return "TEXT"
}

func (f *TextField) PGDefault() string { return "" }

func (f *TextField) ColumnDef() string {
	def := f.PGType()
	if f.required {
		def += " NOT NULL"
	}
	if f.unique {
		def += " UNIQUE"
	}
	return QuoteIdent(f.name) + " " + def
}

func (f *TextField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required {
			return nil, fmt.Errorf("field %q is required", f.name)
		}
		return nil, nil
	}

	var s string
	switch v := raw.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case float64:
		s = strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		s = strconv.FormatInt(v, 10)
	default:
		s = fmt.Sprintf("%v", v)
	}

	s = strings.TrimSpace(s)
	if f.required && s == "" {
		return nil, fmt.Errorf("field %q is required", f.name)
	}

	if s == "" {
		return nil, nil
	}

	if f.MinLength > 0 && len(s) < f.MinLength {
		return nil, fmt.Errorf("field %q: minimum length is %d, got %d", f.name, f.MinLength, len(s))
	}
	if f.MaxLength > 0 && len(s) > f.MaxLength {
		return nil, fmt.Errorf("field %q: maximum length is %d, got %d", f.name, f.MaxLength, len(s))
	}

	return s, nil
}

func (f *TextField) Marshal(value any) (any, error) {
	return f.Validate(value)
}

func (f *TextField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		return "", nil
	}
	switch v := raw.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func (f *TextField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	clone.id = f.id
	return &clone
}

func (f *TextField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["min_length"]; ok {
		if n, ok := toInt(v); ok {
			f.MinLength = n
		}
	}
	if v, ok := opts["max_length"]; ok {
		if n, ok := toInt(v); ok {
			f.MaxLength = n
		}
	}
	if v, ok := opts["pattern"]; ok {
		if s, ok := v.(string); ok {
			f.Pattern = s
		}
	}
}
