package fields

import "fmt"

type BoolField struct{ BaseField }

func NewBoolField(name string) *BoolField {
	return &BoolField{BaseField: NewBaseField(name, TypeBool)}
}

func (f *BoolField) PGType() string  { return "BOOLEAN" }
func (f *BoolField) PGDefault() string { return "" }

func (f *BoolField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.commonColumnDef("BOOLEAN", "")
}

func (f *BoolField) commonColumnDef(pgType, pgDefault string) string {
	return f.BaseField.commonColumnDef(pgType, pgDefault)
}

func (f *BoolField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}
	switch v := raw.(type) {
	case bool: return v, nil
	case string:
		switch v {
		case "true", "1", "yes", "on": return true, nil
		case "false", "0", "no", "off", "": return false, nil
		default: return nil, fmt.Errorf("field %q: invalid boolean value %q", f.name, v)
		}
	case float64: return v != 0, nil
	case int64: return v != 0, nil
	case int: return v != 0, nil
	default: return nil, fmt.Errorf("field %q: expected boolean, got %T", f.name, raw)
	}
}

func (f *BoolField) Marshal(value any) (any, error)   { return f.Validate(value) }
func (f *BoolField) Unmarshal(raw any) (any, error) {
	if raw == nil { return false, nil }
	switch v := raw.(type) {
	case bool: return v, nil
	case string: return v == "true" || v == "1", nil
	case float64: return v != 0, nil
	case int64: return v != 0, nil
	default: return false, nil
	}
}

func (f *BoolField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}
