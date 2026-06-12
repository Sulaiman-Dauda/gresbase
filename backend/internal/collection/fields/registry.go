package fields

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Registry maps field type strings to their constructor.
type Registry struct {
	factories map[FieldType]func(name string) Field
}

// NewRegistry creates a field registry with all built-in types registered.
func NewRegistry() *Registry {
	r := &Registry{factories: make(map[FieldType]func(name string) Field)}
	r.RegisterDefaults()
	return r
}

// Register adds a custom field type.
func (r *Registry) Register(ft FieldType, factory func(name string) Field) {
	r.factories[ft] = factory
}

// RegisterDefaults registers all built-in field types.
func (r *Registry) RegisterDefaults() {
	r.Register(TypeText, func(name string) Field { return NewTextField(name) })
	r.Register(TypeNumber, func(name string) Field { return NewNumberField(name) })
	r.Register(TypeBool, func(name string) Field { return NewBoolField(name) })
	r.Register(TypeEmail, func(name string) Field { return NewEmailField(name) })
	r.Register(TypeURL, func(name string) Field { return NewURLField(name) })
	r.Register(TypeDate, func(name string) Field { return NewDateField(name) })
	r.Register(TypeSelect, func(name string) Field { return NewSelectField(name) })
	r.Register(TypeJSON, func(name string) Field { return NewJSONField(name) })
	r.Register(TypeFile, func(name string) Field { return NewFileField(name) })
	r.Register(TypeRelation, func(name string) Field { return NewRelationField(name) })
	r.Register(TypePassword, func(name string) Field { return NewPasswordField(name) })
	r.Register(TypeEditor, func(name string) Field { return NewEditorField(name) })
	r.Register(TypeGeoPoint, func(name string) Field { return NewGeoPointField(name) })
	r.Register(TypeAutoDate, func(name string) Field { return NewAutoDateField(name) })
}

// Create instantiates a new field of the given type.
func (r *Registry) Create(ft FieldType, name string) (Field, error) {
	factory, ok := r.factories[ft]
	if !ok {
		return nil, fmt.Errorf("unknown field type: %q", ft)
	}
	return factory(name), nil
}

// CreateFromSchema creates a field from a schema definition.
func (r *Registry) CreateFromSchema(def SchemaField) (Field, error) {
	f, err := r.Create(def.Type, def.Name)
	if err != nil {
		return nil, err
	}

	// Apply base properties
	bf, ok := f.(interface {
		SetRequired(bool)
		SetUnique(bool)
		SetSystem(bool)
		SetID(string)
	})
	if !ok {
		return nil, fmt.Errorf("field type %q does not support base properties", def.Type)
	}
	bf.SetRequired(def.Required)
	bf.SetUnique(def.Unique)
	bf.SetSystem(def.System)
	bf.SetID(def.ID)

	// Apply type-specific options
	f.SetOptions(def.Options)

	return f, nil
}

// SchemaField is a lightweight representation used by the registry.
type SchemaField struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     FieldType      `json:"type"`
	System   bool           `json:"system"`
	Required bool           `json:"required"`
	Unique   bool           `json:"unique"`
	Options  map[string]any `json:"options"`
}

// FieldsList is a list of Field implementations.
type FieldsList []Field

// NewFieldsList creates a list from schema definitions.
func NewFieldsList(schemas []SchemaField) (FieldsList, error) {
	r := NewRegistry()
	list := make(FieldsList, len(schemas))
	for i, s := range schemas {
		f, err := r.CreateFromSchema(s)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", s.Name, err)
		}
		list[i] = f
	}
	return list, nil
}

// Get returns a field by name.
func (fl FieldsList) Get(name string) Field {
	for _, f := range fl {
		if f.Name() == name {
			return f
		}
	}
	return nil
}

// GetByID returns a field by ID.
func (fl FieldsList) GetByID(id string) Field {
	for _, f := range fl {
		if f.ID() == id {
			return f
		}
	}
	return nil
}

// Names returns all field names.
func (fl FieldsList) Names() []string {
	names := make([]string, len(fl))
	for i, f := range fl {
		names[i] = f.Name()
	}
	return names
}

// ColumnDefs returns PostgreSQL column definitions for all fields.
func (fl FieldsList) ColumnDefs() []string {
	defs := make([]string, len(fl))
	for i, f := range fl {
		defs[i] = f.ColumnDef()
	}
	return defs
}

// CreateTableSQL generates a CREATE TABLE SQL statement for the fields list.
func (fl FieldsList) CreateTableSQL(tableName string) string {
	var cols []string

	// Always add id column
	cols = append(cols, `"id" TEXT PRIMARY KEY DEFAULT gen_random_uuid()`)

	for _, f := range fl {
		cols = append(cols, f.ColumnDef())
	}

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", QuoteIdent(tableName), strings.Join(cols, ",\n  "))
}

// ValidateRecord validates a complete record against the fields list.
func (fl FieldsList) ValidateRecord(record map[string]any) (map[string]any, error) {
	cleaned := make(map[string]any, len(record))

	for _, f := range fl {
		raw, exists := record[f.Name()]
		if !exists {
			if f.Required() {
				return nil, fmt.Errorf("field %q is required", f.Name())
			}
			continue
		}

		val, err := f.Validate(raw)
		if err != nil {
			return nil, err
		}
		if val != nil {
			cleaned[f.Name()] = val
		}
	}

	return cleaned, nil
}

// MarshalRecord marshals a record for storage.
func (fl FieldsList) MarshalRecord(record map[string]any) (map[string]any, error) {
	marshaled := make(map[string]any, len(record))

	for _, f := range fl {
		raw, exists := record[f.Name()]
		if !exists {
			continue
		}
		val, err := f.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name(), err)
		}
		if val != nil {
			marshaled[f.Name()] = val
		}
	}

	return marshaled, nil
}

// ToJSON serializes the fields list to JSON (for storage in _collections).
func (fl FieldsList) ToJSON() ([]byte, error) {
	var schemas []SchemaField
	for _, f := range fl {
		schemas = append(schemas, SchemaField{
			ID:       f.ID(),
			Name:     f.Name(),
			Type:     f.Type(),
			System:   f.System(),
			Required: f.Required(),
			Unique:   f.Unique(),
			Options:  f.Options(),
		})
	}
	return json.Marshal(schemas)
}

// Clone returns a deep copy of the fields list.
func (fl FieldsList) Clone() FieldsList {
	clone := make(FieldsList, len(fl))
	for i, f := range fl {
		clone[i] = f.Clone()
	}
	return clone
}
