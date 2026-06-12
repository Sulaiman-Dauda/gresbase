package fields

import (
	"fmt"
	"net/url"
	"strings"
)

type URLField struct {
	BaseField
	ExceptDomains []string `json:"except_domains,omitempty"`
	OnlyDomains   []string `json:"only_domains,omitempty"`
}

func NewURLField(name string) *URLField {
	return &URLField{BaseField: NewBaseField(name, TypeURL)}
}

func (f *URLField) PGType() string    { return "TEXT" }
func (f *URLField) PGDefault() string { return "" }
func (f *URLField) ColumnDef() string {
	return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef("TEXT", "")
}
func (f *URLField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required {
			return nil, fmt.Errorf("field %q is required", f.name)
		}
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		if b, ok := raw.([]byte); ok {
			s = string(b)
		} else {
			return nil, fmt.Errorf("field %q: expected URL string", f.name)
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		if f.required {
			return nil, fmt.Errorf("field %q is required", f.name)
		}
		return nil, nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("field %q: invalid URL: %s", f.name, s)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("field %q: URL must have a host", f.name)
	}
	return s, nil
}

func (f *URLField) Marshal(value any) (any, error) { return f.Validate(value) }
func (f *URLField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		return "", nil
	}
	switch v := raw.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func (f *URLField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	return &clone
}

func (f *URLField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["except_domains"]; ok {
		if arr, ok := v.([]any); ok {
			f.ExceptDomains = make([]string, len(arr))
			for i, a := range arr {
				f.ExceptDomains[i] = fmt.Sprintf("%v", a)
			}
		}
	}
}
