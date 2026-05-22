package fields

import (
	"encoding/json"
	"fmt"
)

// JSONField handles arbitrary JSON values stored as JSONB.
type JSONField struct {
	BaseField
	MaxSize    int    `json:"max_size,omitempty"` // max bytes
	Schema     string `json:"schema,omitempty"`   // optional JSON schema for validation
}

func NewJSONField(name string) *JSONField {
	return &JSONField{BaseField: NewBaseField(name, TypeJSON), MaxSize: 1048576}
}

func (f *JSONField) PGType() string  { return "JSONB" }
func (f *JSONField) PGDefault() string { return "'{}'::jsonb" }
func (f *JSONField) ColumnDef() string {
	def := "JSONB"
	if !f.required { def += " DEFAULT '{}'::jsonb" }
	if f.required { def += " NOT NULL" }
	return QuoteIdent(f.name) + " " + def
}

func (f *JSONField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return map[string]any{}, nil
	}
	b, err := EnsureJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("field %q: %w", f.name, err)
	}
	if f.MaxSize > 0 && len(b) > f.MaxSize {
		return nil, fmt.Errorf("field %q: JSON exceeds max size of %d bytes", f.name, f.MaxSize)
	}
	return b, nil
}

func (f *JSONField) Marshal(value any) (any, error)   { return f.Validate(value) }
func (f *JSONField) Unmarshal(raw any) (any, error) {
	if raw == nil { return map[string]any{}, nil }
	switch v := raw.(type) {
	case []byte: return v, nil
	case string: return []byte(v), nil
	default:
		b, err := json.Marshal(v)
		if err != nil { return []byte("{}"), nil }
		return b, nil
	}
}

func (f *JSONField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}

func (f *JSONField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["max_size"]; ok {
		if n, ok := toInt(v); ok { f.MaxSize = n }
	}
}
