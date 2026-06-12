package fields

import (
	"encoding/json"
	"fmt"
)

// RelationField defines a relationship to another collection.
type RelationField struct {
	BaseField
	CollectionID  string   `json:"collection_id,omitempty"`
	CascadeDelete bool     `json:"cascade_delete,omitempty"`
	MaxSelect     int      `json:"max_select,omitempty"`
	DisplayFields []string `json:"display_fields,omitempty"`
}

func NewRelationField(name string) *RelationField {
	return &RelationField{
		BaseField: NewBaseField(name, TypeRelation),
		MaxSelect: 1,
	}
}

func (f *RelationField) PGType() string {
	if f.MaxSelect != 1 {
		return "JSONB"
	}
	return "TEXT"
}

func (f *RelationField) PGDefault() string { return "" }
func (f *RelationField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef(f.PGType(), "")
}

func (f *RelationField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required {
			return nil, fmt.Errorf("relation field %q is required", f.name)
		}
		if f.MaxSelect != 1 {
			return []string{}, nil
		}
		return nil, nil
	}

	// Single relation
	if f.MaxSelect == 1 {
		id := fmt.Sprintf("%v", raw)
		if id == "" || id == "<nil>" {
			if f.required {
				return nil, fmt.Errorf("relation field %q is required", f.name)
			}
			return nil, nil
		}
		return id, nil
	}

	// Multiple relations
	var ids []string
	switch v := raw.(type) {
	case []string:
		ids = v
	case []any:
		for _, item := range v {
			ids = append(ids, fmt.Sprintf("%v", item))
		}
	default:
		return nil, fmt.Errorf("relation field %q: expected array of IDs", f.name)
	}

	if len(ids) == 0 && f.required {
		return nil, fmt.Errorf("relation field %q is required", f.name)
	}
	if len(ids) > f.MaxSelect {
		return nil, fmt.Errorf("relation field %q: max %d relations allowed", f.name, f.MaxSelect)
	}

	// Deduplicate
	seen := make(map[string]bool)
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique, nil
}

func (f *RelationField) Marshal(value any) (any, error) {
	v, err := f.Validate(value)
	if err != nil {
		return nil, err
	}

	if f.MaxSelect != 1 {
		if arr, ok := v.([]string); ok {
			b, err := json.Marshal(arr)
			if err != nil {
				return nil, err
			}
			return b, nil
		}
	}
	return v, nil
}

func (f *RelationField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		return nil, nil
	}
	if f.MaxSelect != 1 {
		switch v := raw.(type) {
		case []byte:
			var arr []string
			if err := json.Unmarshal(v, &arr); err == nil {
				return arr, nil
			}
			return []string{}, nil
		default:
			return []string{}, nil
		}
	}
	return fmt.Sprintf("%v", raw), nil
}

func (f *RelationField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	return &clone
}

func (f *RelationField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["collection_id"]; ok {
		f.CollectionID = fmt.Sprintf("%v", v)
	}
	if v, ok := opts["cascade_delete"]; ok {
		f.CascadeDelete, _ = v.(bool)
	}
	if v, ok := opts["max_select"]; ok {
		if n, ok := toInt(v); ok {
			f.MaxSelect = n
		}
	}
}
