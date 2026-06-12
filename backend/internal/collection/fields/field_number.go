package fields

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// NumberField handles numeric values (stored as DOUBLE PRECISION).
type NumberField struct {
	BaseField
	Min           *float64 `json:"min,omitempty"`
	Max           *float64 `json:"max,omitempty"`
	OnlyInt       bool     `json:"only_int,omitempty"`
	DecimalPlaces int      `json:"decimal_places,omitempty"`
}

func NewNumberField(name string) *NumberField {
	return &NumberField{
		BaseField: NewBaseField(name, TypeNumber),
	}
}

func (f *NumberField) PGType() string    { return "DOUBLE PRECISION" }
func (f *NumberField) PGDefault() string { return "" }

func (f *NumberField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.commonColumnDef("DOUBLE PRECISION", "")
}

func (f *NumberField) commonColumnDef(pgType, pgDefault string) string {
	return f.BaseField.commonColumnDef(pgType, pgDefault)
}

func (f *NumberField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required {
			return nil, fmt.Errorf("field %q is required", f.name)
		}
		return nil, nil
	}

	var n float64
	switch v := raw.(type) {
	case float64:
		n = v
	case float32:
		n = float64(v)
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	case int32:
		n = float64(v)
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("field %q: invalid number: %s", f.name, v)
		}
		n = parsed
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return nil, fmt.Errorf("field %q: invalid number: %s", f.name, v.String())
		}
		n = parsed
	default:
		return nil, fmt.Errorf("field %q: expected number, got %T", f.name, raw)
	}

	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("field %q: invalid number (NaN/Inf)", f.name)
	}

	if f.Min != nil && n < *f.Min {
		return nil, fmt.Errorf("field %q: value %v is below minimum %v", f.name, n, *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return nil, fmt.Errorf("field %q: value %v exceeds maximum %v", f.name, n, *f.Max)
	}
	if f.OnlyInt && n != math.Trunc(n) {
		return nil, fmt.Errorf("field %q: expected integer, got %v", f.name, n)
	}

	return n, nil
}

func (f *NumberField) Marshal(value any) (any, error) { return f.Validate(value) }
func (f *NumberField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		return float64(0), nil
	}
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n, nil
	default:
		return float64(0), nil
	}
}

func (f *NumberField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	if f.Min != nil {
		v := *f.Min
		clone.Min = &v
	}
	if f.Max != nil {
		v := *f.Max
		clone.Max = &v
	}
	return &clone
}

func (f *NumberField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["min"]; ok {
		if n, ok := toFloat(v); ok {
			f.Min = &n
		}
	}
	if v, ok := opts["max"]; ok {
		if n, ok := toFloat(v); ok {
			f.Max = &n
		}
	}
	if v, ok := opts["only_int"]; ok {
		f.OnlyInt, _ = v.(bool)
	}
	if v, ok := opts["decimal_places"]; ok {
		if n, ok := toInt(v); ok {
			f.DecimalPlaces = n
		}
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
