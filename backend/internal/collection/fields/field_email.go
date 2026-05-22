package fields

import (
	"fmt"
	"net/mail"
	"strings"
)

type EmailField struct {
	BaseField
	ExceptDomains []string `json:"except_domains,omitempty"`
	OnlyDomains   []string `json:"only_domains,omitempty"`
}

func NewEmailField(name string) *EmailField {
	return &EmailField{BaseField: NewBaseField(name, TypeEmail)}
}

func (f *EmailField) PGType() string  { return "TEXT" }
func (f *EmailField) PGDefault() string { return "" }

func (f *EmailField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.commonColumnDef("TEXT", "")
}

func (f *EmailField) commonColumnDef(pgType, pgDefault string) string {
	return f.BaseField.commonColumnDef(pgType, pgDefault)
}

func (f *EmailField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		if b, ok := raw.([]byte); ok { s = string(b) } else {
			return nil, fmt.Errorf("field %q: expected email string, got %T", f.name, raw)
		}
	}
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		if f.required { return nil, fmt.Errorf("field %q is required", f.name) }
		return nil, nil
	}

	addr, err := mail.ParseAddress(s)
	if err != nil {
		return nil, fmt.Errorf("field %q: invalid email address: %w", f.name, err)
	}

	email := addr.Address
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("field %q: invalid email format", f.name)
	}
	domain := parts[1]

	for _, d := range f.ExceptDomains {
		if strings.EqualFold(d, domain) {
			return nil, fmt.Errorf("field %q: domain %q is not allowed", f.name, domain)
		}
	}
	if len(f.OnlyDomains) > 0 {
		found := false
		for _, d := range f.OnlyDomains {
			if strings.EqualFold(d, domain) { found = true; break }
		}
		if !found {
			return nil, fmt.Errorf("field %q: domain %q is not in allowed list", f.name, domain)
		}
	}

	return email, nil
}

func (f *EmailField) Marshal(value any) (any, error)   { return f.Validate(value) }
func (f *EmailField) Unmarshal(raw any) (any, error) {
	if raw == nil { return "", nil }
	switch v := raw.(type) {
	case string: return v, nil
	case []byte: return string(v), nil
	default: return fmt.Sprintf("%v", v), nil
	}
}

func (f *EmailField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options { clone.options[k] = v }
	return &clone
}

func (f *EmailField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["except_domains"]; ok {
		if arr, ok := v.([]any); ok {
			f.ExceptDomains = make([]string, len(arr))
			for i, a := range arr { f.ExceptDomains[i] = fmt.Sprintf("%v", a) }
		}
	}
	if v, ok := opts["only_domains"]; ok {
		if arr, ok := v.([]any); ok {
			f.OnlyDomains = make([]string, len(arr))
			for i, a := range arr { f.OnlyDomains[i] = fmt.Sprintf("%v", a) }
		}
	}
}
