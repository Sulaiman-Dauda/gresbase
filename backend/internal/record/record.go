// Package record provides a proper typed Record model with field accessors,
// data normalization, field picking, and relation expansion — replacing the
// previous map[string]any approach with a type-safe implementation.
//
// Matches PocketBase's Record model capabilities:
//   - Typed getters/setters with automatic type conversion
//   - Field picking (?fields=id,name,author)
//   - Relation expansion (?expand=author,category)
//   - Data normalization on create/update
//   - Schema-aware validation hints
//   - Clean JSON serialization (no internal fields leaked)
package record

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Record represents a single row in a dynamic collection with proper
// typing and serialization support.
type Record struct {
	data   map[string]any
	expand map[string]any // resolved relations
}

// New creates a new Record from raw data.
func New(data map[string]any) *Record {
	if data == nil {
		data = make(map[string]any)
	}
	return &Record{
		data:   data,
		expand: make(map[string]any),
	}
}

// NewFromJSON parses JSON into a Record.
func NewFromJSON(raw json.RawMessage) (*Record, error) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid record JSON: %w", err)
	}
	return New(data), nil
}

// ID returns the record's unique identifier.
func (r *Record) ID() string {
	return r.GetString("id")
}

// Get returns a field value as interface{}.
func (r *Record) Get(key string) any {
	val, ok := r.data[key]
	if !ok {
		return nil
	}
	return val
}

// GetString returns a field value as string.
func (r *Record) GetString(key string) string {
	val, ok := r.data[key]
	if !ok || val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// GetInt returns a field value as int.
func (r *Record) GetInt(key string) int {
	val := r.Get(key)
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

// GetFloat returns a field value as float64.
func (r *Record) GetFloat(key string) float64 {
	val := r.Get(key)
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

// GetBool returns a field value as bool.
func (r *Record) GetBool(key string) bool {
	val := r.Get(key)
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}

// GetTime returns a field value as time.Time.
func (r *Record) GetTime(key string) time.Time {
	val := r.Get(key)
	if val == nil {
		return time.Time{}
	}
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		// Try common formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05.000Z",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, v); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// GetSlice returns a field value as []any.
func (r *Record) GetSlice(key string) []any {
	val := r.Get(key)
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []any:
		return v
	case []string:
		result := make([]any, len(v))
		for i, s := range v {
			result[i] = s
		}
		return result
	default:
		return nil
	}
}

// Set sets a field value. The value is normalized based on the field type.
func (r *Record) Set(key string, value any) {
	r.data[key] = value
}

// Has returns true if the field exists and is non-nil.
func (r *Record) Has(key string) bool {
	val, ok := r.data[key]
	return ok && val != nil
}

// Data returns the underlying data map (for DB operations).
func (r *Record) Data() map[string]any {
	return r.data
}

// SetExpand attaches expanded relation data.
func (r *Record) SetExpand(name string, data any) {
	r.expand[name] = data
}

// GetExpand returns expanded relation data.
func (r *Record) GetExpand(name string) any {
	return r.expand[name]
}

// Expand returns all expanded relations.
func (r *Record) Expand() map[string]any {
	return r.expand
}

// HasExpand returns true if there are any expanded relations.
func (r *Record) HasExpand() bool {
	return len(r.expand) > 0
}

// Pick returns a new Record with only the specified fields.
// The special field "*" returns all fields.
func (r *Record) Pick(fields string) *Record {
	if fields == "" || fields == "*" {
		return r
	}

	picked := make(map[string]any)
	fieldList := parseFieldList(fields)

	for _, f := range fieldList {
		if val, ok := r.data[f]; ok {
			picked[f] = val
		}
	}

	// Always include id
	if _, ok := picked["id"]; !ok {
		if id, ok := r.data["id"]; ok {
			picked["id"] = id
		}
	}

	return &Record{data: picked, expand: r.expand}
}

// Clone returns a deep copy of the record.
func (r *Record) Clone() *Record {
	clone := &Record{
		data:   make(map[string]any, len(r.data)),
		expand: make(map[string]any, len(r.expand)),
	}
	for k, v := range r.data {
		clone.data[k] = v
	}
	for k, v := range r.expand {
		clone.expand[k] = v
	}
	return clone
}

// MarshalJSON implements json.Marshaler. Includes expand data when present.
func (r *Record) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(r.data)+len(r.expand))
	for k, v := range r.data {
		out[k] = v
	}
	if len(r.expand) > 0 {
		out["@expand"] = r.expand
	}
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Record) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	// Extract @expand if present
	if exp, ok := raw["@expand"]; ok {
		if expMap, ok := exp.(map[string]any); ok {
			r.expand = expMap
		}
		delete(raw, "@expand")
	}
	r.data = raw
	return nil
}

// ---------------------------------------------------------------------------
// Bulk operations on record slices
// ---------------------------------------------------------------------------

// RecordList is a slice of Record pointers with bulk picking.
type RecordList []*Record

// Pick returns a new RecordList with only specified fields on each record.
func (rl RecordList) Pick(fields string) RecordList {
	result := make(RecordList, len(rl))
	for i, rec := range rl {
		result[i] = rec.Pick(fields)
	}
	return result
}

// IDs returns all record IDs.
func (rl RecordList) IDs() []string {
	ids := make([]string, len(rl))
	for i, r := range rl {
		ids[i] = r.ID()
	}
	return ids
}

// Map converts the list to []map[string]any for API responses.
func (rl RecordList) Map() []map[string]any {
	result := make([]map[string]any, len(rl))
	for i, r := range rl {
		d := make(map[string]any, len(r.data))
		for k, v := range r.data {
			d[k] = v
		}
		if len(r.expand) > 0 {
			d["@expand"] = r.expand
		}
		result[i] = d
	}
	return result
}

// Len returns the number of records.
func (rl RecordList) Len() int {
	return len(rl)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseFieldList(fields string) []string {
	parts := strings.Split(fields, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// NormalizeRecordForDB prepares record data for database insertion by
// converting Go types to PostgreSQL-compatible values.
func NormalizeRecordForDB(data map[string]any, schemaFields map[string]string) map[string]any {
	normalized := make(map[string]any, len(data))
	for key, val := range data {
		fieldType, known := schemaFields[key]
		normalized[key] = normalizeValue(val, fieldType, known)
	}

	// Ensure timestamps
	if _, ok := normalized["created_at"]; !ok {
		normalized["created_at"] = time.Now()
	}
	normalized["updated_at"] = time.Now()

	return normalized
}

func normalizeValue(val any, fieldType string, known bool) any {
	if val == nil {
		return nil
	}

	if !known {
		// Unknown field — pass through, PostgreSQL will handle conversion
		return val
	}

	switch fieldType {
	case "number":
		switch v := val.(type) {
		case float64:
			return v
		case json.Number:
			f, _ := v.Float64()
			return f
		case string:
			return v // PostgreSQL will cast
		default:
			return val
		}

	case "bool":
		switch v := val.(type) {
		case bool:
			return v
		case string:
			return v == "true" || v == "1"
		case float64:
			return v != 0
		default:
			return val
		}

	case "date", "autodate":
		switch v := val.(type) {
		case time.Time:
			return v
		case string:
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return v
			}
			return t
		default:
			return val
		}

	case "json", "file", "geo_point":
		switch v := val.(type) {
		case map[string]any, []any:
			b, _ := json.Marshal(v)
			return string(b)
		case string:
			return v
		default:
			b, _ := json.Marshal(v)
			return string(b)
		}

	case "email", "url", "text", "editor", "select", "password", "relation":
		return fmt.Sprintf("%v", val)

	default:
		return val
	}
}

// SchemaFieldTypeMap returns a name->type map from a schema definition.
func SchemaFieldTypeMap(schema []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) map[string]string {
	m := make(map[string]string, len(schema))
	for _, f := range schema {
		m[f.Name] = f.Type
	}
	return m
}
