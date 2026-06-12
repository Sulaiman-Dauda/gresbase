package fields

import (
	"fmt"
	"time"
)

// AutoDateField handles automatic timestamp fields (created_at, updated_at).
// These are system-managed and not editable by users.
type AutoDateField struct {
	BaseField
	OnCreate bool `json:"on_create,omitempty"` // set on record creation
	OnUpdate bool `json:"on_update,omitempty"` // set on record update
}

func NewAutoDateField(name string) *AutoDateField {
	return &AutoDateField{
		BaseField: NewBaseField(name, TypeAutoDate),
		OnCreate:  name == "created_at",
		OnUpdate:  name == "updated_at",
	}
}

func NewCreatedAtField() *AutoDateField {
	f := NewAutoDateField("created_at")
	f.OnCreate = true
	f.OnUpdate = false
	f.system = true
	return f
}

func NewUpdatedAtField() *AutoDateField {
	f := NewAutoDateField("updated_at")
	f.OnCreate = true
	f.OnUpdate = true
	f.system = true
	return f
}

func (f *AutoDateField) PGType() string    { return "TIMESTAMPTZ" }
func (f *AutoDateField) PGDefault() string { return "NOW()" }
func (f *AutoDateField) ColumnDef() string {
	def := fmt.Sprintf("TIMESTAMPTZ NOT NULL DEFAULT NOW()")
	return QuoteIdent(f.name) + " " + def
}

// Validate is a no-op for autodate fields — they're always system-generated.
func (f *AutoDateField) Validate(raw any) (any, error) {
	if raw == nil {
		return time.Now(), nil
	}
	t, err := ParseDate(raw)
	if err != nil {
		return time.Now(), nil
	}
	return t, nil
}

func (f *AutoDateField) Marshal(value any) (any, error) {
	t, err := f.Validate(value)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (f *AutoDateField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		t, _ := ParseDate(v)
		return t, nil
	default:
		return nil, nil
	}
}

func (f *AutoDateField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	return &clone
}
