package fields

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FileField handles file upload references.
// Stores the filename or a JSON array of filenames for multiple files.
type FileField struct {
	BaseField
	MaxSelect int      `json:"max_select,omitempty"`
	MaxSize   int64    `json:"max_size,omitempty"` // bytes
	MimeTypes []string `json:"mime_types,omitempty"`
	Thumbs    []string `json:"thumbs,omitempty"`    // thumbnail sizes e.g. "100x100"
	Protected bool     `json:"protected,omitempty"` // require auth to download
}

func NewFileField(name string) *FileField {
	return &FileField{
		BaseField: NewBaseField(name, TypeFile),
		MaxSelect: 1,
		MaxSize:   10 * 1024 * 1024, // 10MB default
	}
}

func (f *FileField) PGType() string    { return "TEXT" }
func (f *FileField) PGDefault() string { return "''" }
func (f *FileField) ColumnDef() string {
	def := "TEXT"
	if !f.required {
		def += " DEFAULT ''"
	}
	if f.required {
		def += " NOT NULL"
	}
	return QuoteIdent(f.name) + " " + def
}

func (f *FileField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required {
			return nil, fmt.Errorf("file field %q is required", f.name)
		}
		if f.MaxSelect > 1 {
			return []string{}, nil
		}
		return "", nil
	}

	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			if f.required {
				return nil, fmt.Errorf("file field %q is required", f.name)
			}
			return "", nil
		}
		return v, nil
	case []string:
		if len(v) == 0 {
			if f.required {
				return nil, fmt.Errorf("file field %q is required", f.name)
			}
			return []string{}, nil
		}
		if f.MaxSelect > 0 && len(v) > f.MaxSelect {
			return nil, fmt.Errorf("file field %q: max %d files allowed, got %d", f.name, f.MaxSelect, len(v))
		}
		return v, nil
	default:
		return nil, fmt.Errorf("file field %q: expected string or []string, got %T", f.name, raw)
	}
}

func (f *FileField) Marshal(value any) (any, error) {
	v, err := f.Validate(value)
	if err != nil {
		return nil, err
	}

	if f.MaxSelect > 1 {
		if files, ok := v.([]string); ok {
			b, err := json.Marshal(files)
			if err != nil {
				return nil, err
			}
			return b, nil
		}
	}

	if fileName, ok := v.(string); ok {
		b, err := json.Marshal(fileName)
		if err != nil {
			return nil, err
		}
		return b, nil
	}

	return v, nil
}

func (f *FileField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		if f.MaxSelect > 1 {
			return []string{}, nil
		}
		return "", nil
	}

	if f.MaxSelect > 1 {
		switch v := raw.(type) {
		case []byte:
			var arr []string
			if err := json.Unmarshal(v, &arr); err == nil {
				return arr, nil
			}
			return []string{}, nil
		case string:
			var arr []string
			if err := json.Unmarshal([]byte(v), &arr); err == nil {
				return arr, nil
			}
			if strings.TrimSpace(v) == "" {
				return []string{}, nil
			}
			return []string{v}, nil
		case []string:
			return v, nil
		}
	}

	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			if s, ok := decoded.(string); ok {
				return s, nil
			}
		}
		return v, nil
	case []byte:
		var decoded any
		if err := json.Unmarshal(v, &decoded); err == nil {
			if s, ok := decoded.(string); ok {
				return s, nil
			}
		}
		return string(v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func (f *FileField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	return &clone
}

func (f *FileField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["max_select"]; ok {
		if n, ok := toInt(v); ok {
			f.MaxSelect = n
		}
	}
	if v, ok := opts["max_size"]; ok {
		if n, ok := toFloat(v); ok {
			f.MaxSize = int64(n)
		}
	}
	if v, ok := opts["mime_types"]; ok {
		if arr, ok := v.([]any); ok {
			f.MimeTypes = make([]string, len(arr))
			for i, a := range arr {
				f.MimeTypes[i] = fmt.Sprintf("%v", a)
			}
		}
	}
	if v, ok := opts["thumbs"]; ok {
		if arr, ok := v.([]any); ok {
			f.Thumbs = make([]string, len(arr))
			for i, a := range arr {
				f.Thumbs[i] = fmt.Sprintf("%v", a)
			}
		}
	}
	if v, ok := opts["protected"]; ok {
		f.Protected, _ = v.(bool)
	}
}
