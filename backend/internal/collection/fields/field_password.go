package fields

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// PasswordField handles password values with automatic bcrypt hashing.
type PasswordField struct {
	BaseField
	MinLength  int  `json:"min_length,omitempty"`
	MaxLength  int  `json:"max_length,omitempty"`
	Cost       int  `json:"cost,omitempty"` // bcrypt cost (default 12)
	Pattern    string `json:"pattern,omitempty"`
}

func NewPasswordField(name string) *PasswordField {
	return &PasswordField{
		BaseField: NewBaseField(name, TypePassword),
		MinLength: 8,
		MaxLength: 128,
		Cost:      12,
	}
}

func (f *PasswordField) PGType() string  { return "TEXT" }
func (f *PasswordField) PGDefault() string { return "" }
func (f *PasswordField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef("TEXT", "")
}

func (f *PasswordField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("field %q: expected password string", f.name)
	}
	if s == "" {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return "", nil
	}
	if len(s) < f.MinLength {
		return nil, fmt.Errorf("field %q: minimum length is %d", f.name, f.MinLength)
	}
	if f.MaxLength > 0 && len(s) > f.MaxLength {
		return nil, fmt.Errorf("field %q: maximum length is %d", f.name, f.MaxLength)
	}
	return s, nil
}

// Marshal hashes the password with bcrypt before storage.
func (f *PasswordField) Marshal(value any) (any, error) {
	raw, err := f.Validate(value)
	if err != nil {
		return nil, err
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(s), f.Cost)
	if err != nil {
		return nil, fmt.Errorf("field %q: password hashing failed: %w", f.name, err)
	}
	return string(hash), nil
}

// Unmarshal just returns the hash as-is (never expose the raw password).
func (f *PasswordField) Unmarshal(raw any) (any, error) {
	if raw == nil { return "", nil }
	switch v := raw.(type) {
	case string: return v, nil
	case []byte: return string(v), nil
	default: return fmt.Sprintf("%v", v), nil
	}
}

func (f *PasswordField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}

func (f *PasswordField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["min_length"]; ok {
		if n, ok := toInt(v); ok { f.MinLength = n }
	}
	if v, ok := opts["max_length"]; ok {
		if n, ok := toInt(v); ok { f.MaxLength = n }
	}
	if v, ok := opts["cost"]; ok {
		if n, ok := toInt(v); ok && n >= 4 && n <= 31 { f.Cost = n }
	}
	if v, ok := opts["pattern"]; ok {
		if s, ok := v.(string); ok { f.Pattern = s }
	}
}
