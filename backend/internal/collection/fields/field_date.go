package fields

import (
	"fmt"
	"time"
)

type DateField struct {
	BaseField
	Min time.Time `json:"min,omitempty"`
	Max time.Time `json:"max,omitempty"`
}

func NewDateField(name string) *DateField {
	return &DateField{BaseField: NewBaseField(name, TypeDate)}
}

func (f *DateField) PGType() string  { return "TIMESTAMPTZ" }
func (f *DateField) PGDefault() string { return "" }
func (f *DateField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef("TIMESTAMPTZ", "")
}

func (f *DateField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}
	t, err := ParseDate(raw)
	if err != nil {
		return nil, fmt.Errorf("field %q: %w", f.name, err)
	}
	if !f.Min.IsZero() && t.Before(f.Min) {
		return nil, fmt.Errorf("field %q: date %v is before minimum %v", f.name, t, f.Min)
	}
	if !f.Max.IsZero() && t.After(f.Max) {
		return nil, fmt.Errorf("field %q: date %v is after maximum %v", f.name, t, f.Max)
	}
	return t, nil
}

func (f *DateField) Marshal(value any) (any, error)   { return f.Validate(value) }
func (f *DateField) Unmarshal(raw any) (any, error) {
	if raw == nil { return nil, nil }
	switch v := raw.(type) {
	case time.Time: return v, nil
	case string: t, _ := ParseDate(v); return t, nil
	default: return nil, nil
	}
}

func (f *DateField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}

func (f *DateField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["min"]; ok {
		if t, err := ParseDate(v); err == nil { f.Min = t }
	}
	if v, ok := opts["max"]; ok {
		if t, err := ParseDate(v); err == nil { f.Max = t }
	}
}
