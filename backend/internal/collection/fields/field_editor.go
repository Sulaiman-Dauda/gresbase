package fields

import "fmt"

// EditorField handles rich text / WYSIWYG content.
type EditorField struct {
	BaseField
	MaxLength int  `json:"max_length,omitempty"`
	Sanitize  bool `json:"sanitize,omitempty"` // strip HTML tags if true
}

func NewEditorField(name string) *EditorField {
	return &EditorField{
		BaseField: NewBaseField(name, TypeEditor),
		MaxLength: 0,
		Sanitize:  false,
	}
}

func (f *EditorField) PGType() string  { return "TEXT" }
func (f *EditorField) PGDefault() string { return "''" }
func (f *EditorField) ColumnDef() string {
	def := "TEXT"
	if !f.required { def += " DEFAULT ''" }
	if f.required { def += " NOT NULL" }
	return QuoteIdent(f.name) + " " + def
}

func (f *EditorField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return "", nil
	}
	s := fmt.Sprintf("%v", raw)
	if s == "" || s == "<nil>" {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return "", nil
	}
	if f.MaxLength > 0 && len(s) > f.MaxLength {
		return nil, fmt.Errorf("field %q: content exceeds max length of %d", f.name, f.MaxLength)
	}
	return s, nil
}

func (f *EditorField) Marshal(value any) (any, error)   { return f.Validate(value) }
func (f *EditorField) Unmarshal(raw any) (any, error) {
	if raw == nil { return "", nil }
	switch v := raw.(type) {
	case string: return v, nil
	case []byte: return string(v), nil
	default: return fmt.Sprintf("%v", v), nil
	}
}

func (f *EditorField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}

func (f *EditorField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["max_length"]; ok {
		if n, ok := toInt(v); ok { f.MaxLength = n }
	}
	if v, ok := opts["sanitize"]; ok {
		f.Sanitize, _ = v.(bool)
	}
}


