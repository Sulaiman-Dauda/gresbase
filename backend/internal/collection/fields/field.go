// Package fields provides typed field definitions and validation for dynamic collections.
// Each field type knows how to map to PostgreSQL columns, validate values, and serialize/deserialize.
package fields

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// FieldType enumerates all supported field types.
type FieldType string

const (
	TypeText     FieldType = "text"
	TypeNumber   FieldType = "number"
	TypeBool     FieldType = "bool"
	TypeEmail    FieldType = "email"
	TypeURL      FieldType = "url"
	TypeDate     FieldType = "date"
	TypeSelect   FieldType = "select"
	TypeJSON     FieldType = "json"
	TypeFile     FieldType = "file"
	TypeRelation FieldType = "relation"
	TypePassword FieldType = "password"
	TypeEditor   FieldType = "editor"
	TypeGeoPoint FieldType = "geo_point"
	TypeAutoDate FieldType = "autodate"
)

// Field defines the interface all field types must implement.
type Field interface {
	// ID returns the field's unique identifier.
	ID() string
	// Name returns the column name.
	Name() string
	// Type returns the field type.
	Type() FieldType
	// System indicates whether this is a system-managed field.
	System() bool
	// Required indicates whether the field is required.
	Required() bool
	// Unique indicates whether the field values must be unique.
	Unique() bool
	// PGType returns the PostgreSQL column type for this field.
	PGType() string
	// PGDefault returns the PostgreSQL default value expression, if any.
	PGDefault() string
	// ColumnDef returns the full PostgreSQL column definition clause.
	ColumnDef() string
	// Validate validates a raw value for this field. Returns the cleaned value or error.
	Validate(raw any) (any, error)
	// Marshal serializes a value for storage.
	Marshal(value any) (any, error)
	// Unmarshal deserializes a value from storage.
	Unmarshal(raw any) (any, error)
	// Options returns field-specific options.
	Options() map[string]any
	// SetOptions applies field-specific options.
	SetOptions(opts map[string]any)
	// Clone returns a deep copy of the field.
	Clone() Field
}

// BaseField provides common field behavior.
type BaseField struct {
	id       string
	name     string
	typ      FieldType
	system   bool
	required bool
	unique   bool
	options  map[string]any
}

// NewBaseField creates a new BaseField with sensible defaults.
func NewBaseField(name string, typ FieldType) BaseField {
	return BaseField{
		id:      uuid.New().String(),
		name:    name,
		typ:     typ,
		options: make(map[string]any),
	}
}

func (f *BaseField) ID() string              { return f.id }
func (f *BaseField) Name() string            { return f.name }
func (f *BaseField) Type() FieldType         { return f.typ }
func (f *BaseField) System() bool            { return f.system }
func (f *BaseField) Required() bool          { return f.required }
func (f *BaseField) Unique() bool            { return f.unique }
func (f *BaseField) Options() map[string]any { return f.options }

func (f *BaseField) SetRequired(v bool) { f.required = v }
func (f *BaseField) SetUnique(v bool)   { f.unique = v }
func (f *BaseField) SetSystem(v bool)   { f.system = v }
func (f *BaseField) SetID(id string)    { f.id = id }

func (f *BaseField) SetOptions(opts map[string]any) {
	if opts == nil {
		f.options = make(map[string]any)
		return
	}
	f.options = opts
}

// commonColumnDef returns the shared column definition suffix for NOT NULL and UNIQUE.
func (f *BaseField) commonColumnDef(pgType, pgDefault string) string {
	parts := []string{QuoteIdent(f.name), pgType}
	if pgDefault != "" {
		parts = append(parts, "DEFAULT", pgDefault)
	}
	if f.required {
		parts = append(parts, "NOT NULL")
	}
	if f.unique {
		parts = append(parts, "UNIQUE")
	}
	return strings.Join(parts, " ")
}

// Common helpers

// QuoteIdent quotes a PostgreSQL identifier.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// PGLiteral escapes a string literal for PostgreSQL.
func PGLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// EnsureJSON ensures a value is valid JSON for JSONB columns.
func EnsureJSON(v any) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		// Validate it's valid JSON
		var js json.RawMessage
		if err := json.Unmarshal(val, &js); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return val, nil
	case string:
		var js json.RawMessage
		if err := json.Unmarshal([]byte(val), &js); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return []byte(val), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal to JSON: %w", err)
		}
		return b, nil
	}
}

// ParseDate parses various date/time formats into time.Time.
func ParseDate(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
			"2006-01-02T15:04:05.999Z",
			"2006-01-02T15:04:05.999999Z",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, v); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse date: %s", v)
	case pgtype.Timestamptz:
		return v.Time, nil
	case pgtype.Timestamp:
		return v.Time, nil
	default:
		return time.Time{}, fmt.Errorf("cannot parse date from type %T", raw)
	}
}

// toInt converts various numeric types to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// Ensure method implementations compile
var (
	_ Field = (*TextField)(nil)
	_ Field = (*NumberField)(nil)
	_ Field = (*BoolField)(nil)
	_ Field = (*EmailField)(nil)
	_ Field = (*URLField)(nil)
	_ Field = (*DateField)(nil)
	_ Field = (*SelectField)(nil)
	_ Field = (*JSONField)(nil)
	_ Field = (*FileField)(nil)
	_ Field = (*RelationField)(nil)
	_ Field = (*PasswordField)(nil)
	_ Field = (*EditorField)(nil)
	_ Field = (*GeoPointField)(nil)
	_ Field = (*AutoDateField)(nil)
)
