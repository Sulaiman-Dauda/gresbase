package collection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/filter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// All tests in this file are pure unit tests: they exercise validation,
// marshaling, SQL-literal building and rule semantics without a database.

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// ValidateCollectionDefinition (rule tri-state, identifiers, auth invariants)
// ---------------------------------------------------------------------------

func TestValidateCollectionDefinitionRules(t *testing.T) {
	svc := NewService(nil)

	base := func() *Collection {
		return &Collection{
			Name: "posts",
			Type: TypeBase,
			Schema: []SchemaField{
				{Name: "title", Type: FieldText},
			},
		}
	}

	if err := svc.ValidateCollectionDefinition(nil); err == nil {
		t.Error("nil collection should error")
	}

	// nil rule = locked, "" = public, expression = filtered: all valid
	c := base()
	c.ListRule = nil
	c.ViewRule = strPtr("")
	c.CreateRule = strPtr("title = 'x'")
	c.UpdateRule = strPtr("@request.auth.id != ''")
	c.DeleteRule = strPtr("")
	if err := svc.ValidateCollectionDefinition(c); err != nil {
		t.Errorf("tri-state rules should validate: %v", err)
	}

	// malformed rule expression must be rejected
	c = base()
	c.ListRule = strPtr("title = ")
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("malformed list_rule should error")
	}

	// invalid collection name
	c = base()
	c.Name = "1bad; DROP TABLE"
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("invalid collection name should error")
	}

	// invalid field name
	c = base()
	c.Schema = []SchemaField{{Name: "bad-name", Type: FieldText}}
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("invalid field name should error")
	}

	// duplicate field names are case-insensitive
	c = base()
	c.Schema = []SchemaField{
		{Name: "Title", Type: FieldText},
		{Name: "title", Type: FieldText},
	}
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("case-insensitive duplicate field should error")
	}

	// reserved system field names
	for _, reserved := range []string{"id", "created_at", "updated_at"} {
		c = base()
		c.Schema = []SchemaField{{Name: reserved, Type: FieldText}}
		if err := svc.ValidateCollectionDefinition(c); err == nil {
			t.Errorf("reserved field %q should error", reserved)
		}
	}

	// bad default literal bubbles up
	c = base()
	c.Schema = []SchemaField{{Name: "n", Type: FieldNumber, Options: map[string]any{"default": "not-a-number"}}}
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("non-numeric default for number field should error")
	}
}

func TestValidateCollectionDefinitionAuth(t *testing.T) {
	svc := NewService(nil)

	// "verified" is reserved for auth collections
	c := &Collection{
		Name: "users",
		Type: TypeAuth,
		Schema: []SchemaField{
			{Name: "email", Type: FieldEmail},
			{Name: "verified", Type: FieldBool},
		},
	}
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("verified field on auth collection should error")
	}

	// auth collections need an identity field
	c = &Collection{
		Name:   "users",
		Type:   TypeAuth,
		Schema: []SchemaField{{Name: "nick", Type: FieldText}},
	}
	if err := svc.ValidateCollectionDefinition(c); err == nil {
		t.Error("auth collection without identity should error")
	}

	// email-typed field satisfies the identity requirement
	c.Schema = []SchemaField{{Name: "email", Type: FieldEmail}}
	if err := svc.ValidateCollectionDefinition(c); err != nil {
		t.Errorf("email identity should validate: %v", err)
	}

	// a field literally named "username" also satisfies it
	c.Schema = []SchemaField{{Name: "username", Type: FieldText}}
	if err := svc.ValidateCollectionDefinition(c); err != nil {
		t.Errorf("username identity should validate: %v", err)
	}
}

func TestValidateViewQuery(t *testing.T) {
	cases := []struct {
		query string
		ok    bool
	}{
		{"", false},
		{"   ", false},
		{"SELECT 1; DROP TABLE x", false},
		{"DELETE FROM x", false},
		{"SELECT id FROM posts", true},
		{"WITH t AS (SELECT 1) SELECT * FROM t", true},
		{"  select id from posts", true}, // case-insensitive
	}
	for _, c := range cases {
		err := validateViewQuery(c.query)
		if c.ok && err != nil {
			t.Errorf("validateViewQuery(%q): unexpected error %v", c.query, err)
		}
		if !c.ok && err == nil {
			t.Errorf("validateViewQuery(%q): expected error", c.query)
		}
	}

	// integrated through ValidateCollectionDefinition for view collections
	svc := NewService(nil)
	v := &Collection{Name: "v", Type: TypeView, ViewQuery: "DELETE FROM x"}
	if err := svc.ValidateCollectionDefinition(v); err == nil {
		t.Error("view with non-SELECT query should error")
	}
}

// ---------------------------------------------------------------------------
// ValidateRecord / ValidateRecordPatch
// ---------------------------------------------------------------------------

func TestValidateRecordSemantics(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "posts",
		Type: TypeBase,
		Schema: []SchemaField{
			{Name: "title", Type: FieldText, Required: true},
			{Name: "views", Type: FieldNumber},
			{Name: "secret", Type: FieldText, System: true},
		},
	}

	if err := svc.ValidateRecord(nil, map[string]any{}); err == nil {
		t.Error("nil collection should error")
	}

	// unknown field
	if err := svc.ValidateRecord(coll, map[string]any{"title": "a", "nope": 1}); err == nil {
		t.Error("unknown field should error")
	}
	// system fields are not writable (treated as unknown)
	if err := svc.ValidateRecord(coll, map[string]any{"title": "a", "secret": "x"}); err == nil {
		t.Error("system field should be rejected")
	}
	// reserved fields are read-only
	for _, reserved := range []string{"id", "created_at", "updated_at"} {
		if err := svc.ValidateRecord(coll, map[string]any{"title": "a", reserved: "x"}); err == nil {
			t.Errorf("reserved field %q should be rejected", reserved)
		}
	}

	// required: missing on full create
	if err := svc.ValidateRecord(coll, map[string]any{"views": float64(1)}); err == nil {
		t.Error("missing required field should error on create")
	}
	// required: missing on patch is fine
	if err := svc.ValidateRecordPatch(coll, map[string]any{"views": float64(1)}); err != nil {
		t.Errorf("patch without required field should pass: %v", err)
	}
	// required: explicitly blank or nil is rejected even on patch
	if err := svc.ValidateRecordPatch(coll, map[string]any{"title": "   "}); err == nil {
		t.Error("blank required field should error")
	}
	if err := svc.ValidateRecordPatch(coll, map[string]any{"title": nil}); err == nil {
		t.Error("nil required field should error")
	}

	// happy path
	if err := svc.ValidateRecord(coll, map[string]any{"title": "hello", "views": float64(2)}); err != nil {
		t.Errorf("valid record: %v", err)
	}

	// auth collections: "verified" is read-only through the API
	auth := &Collection{
		Name:   "users",
		Type:   TypeAuth,
		Schema: []SchemaField{{Name: "email", Type: FieldEmail}},
	}
	if err := svc.ValidateRecordPatch(auth, map[string]any{"verified": true}); err == nil {
		t.Error("verified should be read-only for auth collections")
	}
}

func TestValidateFieldPerType(t *testing.T) {
	check := func(field SchemaField, val any, wantErr bool) {
		t.Helper()
		err := validateField(field, val)
		if wantErr && err == nil {
			t.Errorf("%s %#v: expected error", field.Type, val)
		}
		if !wantErr && err != nil {
			t.Errorf("%s %#v: unexpected error %v", field.Type, val, err)
		}
	}

	text := SchemaField{Name: "t", Type: FieldText, Options: map[string]any{
		"min": float64(2), "max": float64(4), "pattern": "^[a-z]+$",
	}}
	check(text, "abc", false)
	check(text, "a", true)                                                                       // below min
	check(text, "abcde", true)                                                                   // above max
	check(text, "ABC", true)                                                                     // pattern mismatch
	check(text, 42, true)                                                                        // not a string
	check(SchemaField{Type: FieldText, Options: map[string]any{"pattern": "([bad"}}, "x", false) // invalid pattern is skipped

	num := SchemaField{Name: "n", Type: FieldNumber, Options: map[string]any{"min": float64(0), "max": float64(10)}}
	check(num, float64(5), false)
	check(num, float64(-1), true)
	check(num, float64(11), true)
	check(num, 5, false)        // int coerces
	check(num, int64(5), false) // int64 coerces
	check(num, "5", true)       // string rejected

	check(SchemaField{Type: FieldBool}, true, false)
	check(SchemaField{Type: FieldBool}, "true", true)

	check(SchemaField{Type: FieldEmail}, "a@b.com", false)
	check(SchemaField{Type: FieldEmail}, "not-an-email", true)
	check(SchemaField{Type: FieldEmail}, 5, true)

	check(SchemaField{Type: FieldURL}, "https://example.com", false)
	check(SchemaField{Type: FieldURL}, "ftp://example.com", true)
	check(SchemaField{Type: FieldURL}, "https://", true)
	check(SchemaField{Type: FieldURL}, 5, true)

	sel := SchemaField{Type: FieldSelect, Options: map[string]any{"values": []any{"a", "b"}}}
	check(sel, "a", false)
	check(sel, "z", true)
	check(sel, 5, true)
	check(SchemaField{Type: FieldSelect}, "anything", false) // no constraint

	check(SchemaField{Type: FieldJSON}, map[string]any{"x": 1}, false)
	check(SchemaField{Type: FieldFile}, "whatever", false)
	check(SchemaField{Type: FieldEditor}, "<b>x</b>", false)
	check(SchemaField{Type: FieldGeoPoint}, map[string]any{"lat": 1}, false)
	check(SchemaField{Type: FieldAutoDate}, "anything", false)

	pw := SchemaField{Type: FieldPassword, Options: map[string]any{"max": float64(12)}}
	check(pw, "longenough", false)
	check(pw, "short", true)
	check(pw, "waytoolongpassword", true)
	check(pw, 42, true)

	check(SchemaField{Type: FieldDate}, "2024-01-01", false)
	check(SchemaField{Type: FieldDate}, float64(1700000000), false)
	check(SchemaField{Type: FieldDate}, int64(1700000000), false)
	check(SchemaField{Type: FieldDate}, true, true)

	vec := SchemaField{Type: FieldVector, Options: map[string]any{"dimensions": float64(2)}}
	check(vec, []any{1.0, 2.0}, false)
	check(vec, []any{1.0, 2.0, 3.0}, true) // wrong dims
	check(vec, []any{"x", 2.0}, true)      // non-numeric element
	check(vec, "[1,2]", false)             // preformatted string
	check(vec, "garbage", true)
	check(vec, 42, true)

	rel := SchemaField{Type: FieldRelation}
	check(rel, "rec-id", false)
	check(rel, "", false) // empty + not required
	check(rel, []any{"a", "b"}, false)
	check(rel, 42, true)

	// nil + not required short-circuits for every type
	check(SchemaField{Type: FieldText}, nil, false)
}

// ---------------------------------------------------------------------------
// Record normalization helpers
// ---------------------------------------------------------------------------

func TestDecodeMaybeJSONValue(t *testing.T) {
	if v := decodeMaybeJSONValue([]byte(`{"a":1}`)); v.(map[string]any)["a"] != float64(1) {
		t.Errorf("json bytes = %#v", v)
	}
	if v := decodeMaybeJSONValue([]byte("plain")); v != "plain" {
		t.Errorf("plain bytes = %#v", v)
	}
	if v := decodeMaybeJSONValue(`[1,2]`); len(v.([]any)) != 2 {
		t.Errorf("json array string = %#v", v)
	}
	if v := decodeMaybeJSONValue(`"quoted"`); v != "quoted" {
		t.Errorf("quoted string = %#v", v)
	}
	if v := decodeMaybeJSONValue("plain text"); v != "plain text" {
		t.Errorf("plain string = %#v", v)
	}
	if v := decodeMaybeJSONValue(`{broken`); v != `{broken` {
		t.Errorf("broken json = %#v", v)
	}
	if v := decodeMaybeJSONValue(42); v != 42 {
		t.Errorf("other = %#v", v)
	}
}

func TestNormalizeFieldValue(t *testing.T) {
	ts := time.Date(2024, 5, 1, 12, 0, 0, 0, time.FixedZone("X", 3600))
	if v := normalizeFieldValue(SchemaField{Type: FieldDate}, ts); v != "2024-05-01T11:00:00Z" {
		t.Errorf("date time = %#v", v)
	}
	if v := normalizeFieldValue(SchemaField{Type: FieldAutoDate}, []byte("2024-05-01")); v != "2024-05-01" {
		t.Errorf("date bytes = %#v", v)
	}
	if v := normalizeFieldValue(SchemaField{Type: FieldDate}, "passthrough"); v != "passthrough" {
		t.Errorf("date other = %#v", v)
	}
	if v := normalizeFieldValue(SchemaField{Type: FieldJSON}, []byte(`{"a":1}`)); v.(map[string]any)["a"] != float64(1) {
		t.Errorf("json = %#v", v)
	}
	if v := normalizeFieldValue(SchemaField{Type: FieldText}, []byte("txt")); v != "txt" {
		t.Errorf("default bytes = %#v", v)
	}
	if v := normalizeFieldValue(SchemaField{Type: FieldText}, "x"); v != "x" {
		t.Errorf("default passthrough = %#v", v)
	}
}

func TestNormalizeRecord(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "c",
		Schema: []SchemaField{
			{Name: "meta", Type: FieldJSON},
			{Name: "title", Type: FieldText},
		},
	}
	rec := Record{
		"meta":    []byte(`{"k":"v"}`),
		"title":   []byte("hello"),
		"unknown": []byte("left-alone"),
	}
	got := svc.normalizeRecord(coll, rec)
	if got["meta"].(map[string]any)["k"] != "v" {
		t.Errorf("meta = %#v", got["meta"])
	}
	if got["title"] != "hello" {
		t.Errorf("title = %#v", got["title"])
	}
	if string(got["unknown"].([]byte)) != "left-alone" {
		t.Errorf("unknown field should be untouched: %#v", got["unknown"])
	}

	if svc.normalizeRecord(nil, rec) == nil {
		t.Error("nil collection should return the record unchanged")
	}
	if svc.normalizeRecord(coll, nil) != nil {
		t.Error("nil record should stay nil")
	}
}

func TestNormalizeSort(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "posts",
		Type: TypeBase,
		Schema: []SchemaField{
			{Name: "title", Type: FieldText},
		},
	}

	if got := svc.normalizeSort(coll, ""); got != `"created_at" DESC` {
		t.Errorf("empty sort = %q", got)
	}
	if got := svc.normalizeSort(coll, "-title, +id ,created_at"); got != `"title" DESC, "id" ASC, "created_at" ASC` {
		t.Errorf("multi sort = %q", got)
	}
	// unknown fields are dropped; injection attempts never reach SQL
	if got := svc.normalizeSort(coll, "evil; DROP TABLE x,hax"); got != `"created_at" DESC` {
		t.Errorf("unknown fields = %q", got)
	}
	// auth collections may sort on verified
	auth := &Collection{Name: "users", Type: TypeAuth, Schema: []SchemaField{{Name: "email", Type: FieldEmail}}}
	if got := svc.normalizeSort(auth, "verified"); got != `"verified" ASC` {
		t.Errorf("auth verified sort = %q", got)
	}
	// schema fields with unsafe identifiers are skipped even though allowed
	weird := &Collection{Name: "w", Schema: []SchemaField{{Name: "bad-name", Type: FieldText}}}
	if got := svc.normalizeSort(weird, "bad-name"); got != `"created_at" DESC` {
		t.Errorf("unsafe identifier = %q", got)
	}
	if got := svc.normalizeSort(nil, "id"); got != `"id" ASC` {
		t.Errorf("nil collection = %q", got)
	}
}

// ---------------------------------------------------------------------------
// marshalRecordData / valuePlaceholder
// ---------------------------------------------------------------------------

func TestMarshalRecordData(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "docs",
		Schema: []SchemaField{
			{Name: "title", Type: FieldText},
			{Name: "emb", Type: FieldVector, Options: map[string]any{"dimensions": 2}},
		},
	}

	out, err := svc.marshalRecordData(coll, map[string]any{
		"title": "  hi  ",
		"emb":   []any{1.0, 2.0},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if out["title"] != "hi" {
		t.Errorf("title = %#v (text fields trim)", out["title"])
	}
	if out["emb"] != "[1,2]" {
		t.Errorf("emb = %#v, want pgvector text format", out["emb"])
	}

	// vector encode failure surfaces with the field name
	if _, err := svc.marshalRecordData(coll, map[string]any{"emb": 42}); err == nil || !strings.Contains(err.Error(), "emb") {
		t.Errorf("bad vector should error with field name, got %v", err)
	}

	// empty vector value becomes NULL
	out, err = svc.marshalRecordData(coll, map[string]any{"emb": ""})
	if err != nil {
		t.Fatalf("empty vector: %v", err)
	}
	if out["emb"] != nil {
		t.Errorf("empty vector = %#v, want nil", out["emb"])
	}

	// pass-through cases
	data := map[string]any{"x": 1}
	if got, err := svc.marshalRecordData(nil, data); err != nil || len(got) != 1 {
		t.Errorf("nil collection = %#v, %v", got, err)
	}
	if got, err := svc.marshalRecordData(&Collection{Name: "e"}, data); err != nil || len(got) != 1 {
		t.Errorf("empty schema = %#v, %v", got, err)
	}
	if got, err := svc.marshalRecordData(coll, map[string]any{}); err != nil || len(got) != 0 {
		t.Errorf("empty data = %#v, %v", got, err)
	}

	// unknown field type in schema fails fast
	bad := &Collection{Name: "b", Schema: []SchemaField{{Name: "z", Type: FieldType("mystery")}}}
	if _, err := svc.marshalRecordData(bad, map[string]any{"z": 1}); err == nil {
		t.Error("unknown field type should error")
	}
}

func TestValuePlaceholder(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "docs",
		Schema: []SchemaField{
			{Name: "emb", Type: FieldVector},
			{Name: "title", Type: FieldText},
		},
	}
	if got := svc.valuePlaceholder(coll, "emb", 1); got != "$1::vector" {
		t.Errorf("vector placeholder = %q", got)
	}
	if got := svc.valuePlaceholder(coll, "title", 2); got != "$2" {
		t.Errorf("text placeholder = %q", got)
	}
	if got := svc.valuePlaceholder(nil, "emb", 3); got != "$3" {
		t.Errorf("nil coll placeholder = %q", got)
	}
}

// ---------------------------------------------------------------------------
// Column/DDL builders
// ---------------------------------------------------------------------------

func TestMaxSelectOption(t *testing.T) {
	cases := []struct {
		opts map[string]any
		want int
		ok   bool
	}{
		{nil, 0, false},
		{map[string]any{}, 0, false},
		{map[string]any{"max_select": 3}, 3, true},
		{map[string]any{"max_select": int64(4)}, 4, true},
		{map[string]any{"max_select": float64(5)}, 5, true},
		{map[string]any{"max_select": "6"}, 0, false},
	}
	for _, c := range cases {
		got, ok := maxSelectOption(c.opts)
		if got != c.want || ok != c.ok {
			t.Errorf("maxSelectOption(%v) = (%d,%v), want (%d,%v)", c.opts, got, ok, c.want, c.ok)
		}
	}
}

func TestDefaultSQLLiteral(t *testing.T) {
	svc := NewService(nil)

	lit := func(f SchemaField) (string, error) { return svc.defaultSQLLiteral(f) }

	// no default at all
	if got, err := lit(SchemaField{Type: FieldText}); err != nil || got != "" {
		t.Errorf("no default = %q, %v", got, err)
	}

	// string-ish types quote and escape
	if got, _ := lit(SchemaField{Type: FieldText, Options: map[string]any{"default": "it's"}}); got != "'it''s'" {
		t.Errorf("text default = %q", got)
	}
	if _, err := lit(SchemaField{Type: FieldText, Options: map[string]any{"default": 5}}); err == nil {
		t.Error("non-string text default should error")
	}

	// relations: single is a string, multi becomes jsonb
	if got, _ := lit(SchemaField{Type: FieldRelation, Options: map[string]any{"default": "rid"}}); got != "'rid'" {
		t.Errorf("single relation default = %q", got)
	}
	if _, err := lit(SchemaField{Type: FieldRelation, Options: map[string]any{"default": 5}}); err == nil {
		t.Error("non-string single relation default should error")
	}
	multi := SchemaField{Type: FieldRelation, Options: map[string]any{"default": []any{"a"}, "max_select": 2}}
	if got, err := lit(multi); err != nil || got != `'["a"]'::jsonb` {
		t.Errorf("multi relation default = %q, %v", got, err)
	}

	// numbers
	numCases := []struct {
		val  any
		want string
	}{
		{float64(1.5), "1.5"},
		{float32(2.5), "2.5"},
		{3, "3"},
		{int64(4), "4"},
		{uint(5), "5"},
	}
	for _, c := range numCases {
		got, err := lit(SchemaField{Type: FieldNumber, Options: map[string]any{"default": c.val}})
		if err != nil || got != c.want {
			t.Errorf("number default %#v = %q, %v; want %q", c.val, got, err, c.want)
		}
	}
	if _, err := lit(SchemaField{Type: FieldNumber, Options: map[string]any{"default": "x"}}); err == nil {
		t.Error("string number default should error")
	}

	// booleans
	if got, _ := lit(SchemaField{Type: FieldBool, Options: map[string]any{"default": true}}); got != "TRUE" {
		t.Errorf("bool true = %q", got)
	}
	if got, _ := lit(SchemaField{Type: FieldBool, Options: map[string]any{"default": false}}); got != "FALSE" {
		t.Errorf("bool false = %q", got)
	}
	if _, err := lit(SchemaField{Type: FieldBool, Options: map[string]any{"default": "y"}}); err == nil {
		t.Error("non-bool default should error")
	}

	// JSON-ish types
	if got, err := lit(SchemaField{Type: FieldJSON, Options: map[string]any{"default": map[string]any{"a": 1}}}); err != nil || got != `'{"a":1}'::jsonb` {
		t.Errorf("json default = %q, %v", got, err)
	}

	// vectors never get SQL defaults
	if got, err := lit(SchemaField{Type: FieldVector, Options: map[string]any{"default": "[1]"}}); err != nil || got != "" {
		t.Errorf("vector default = %q, %v", got, err)
	}

	// dates
	if got, _ := lit(SchemaField{Type: FieldDate, Options: map[string]any{"default": "2024-01-01"}}); got != "'2024-01-01'" {
		t.Errorf("date string default = %q", got)
	}
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if got, _ := lit(SchemaField{Type: FieldAutoDate, Options: map[string]any{"default": ts}}); got != "'2024-01-02T03:04:05Z'" {
		t.Errorf("date time default = %q", got)
	}
	if _, err := lit(SchemaField{Type: FieldDate, Options: map[string]any{"default": 5}}); err == nil {
		t.Error("numeric date default should error")
	}

	// unknown types fall back to a quoted string
	if got, _ := lit(SchemaField{Type: FieldType("custom"), Options: map[string]any{"default": 7}}); got != "'7'" {
		t.Errorf("fallback default = %q", got)
	}
}

func TestQuoteSQLString(t *testing.T) {
	if got := quoteSQLString("a'b"); got != "'a''b'" {
		t.Errorf("quoteSQLString = %q", got)
	}
}

func TestColumnTypeForField(t *testing.T) {
	svc := NewService(nil)

	cases := []struct {
		field SchemaField
		want  string
	}{
		{SchemaField{Type: FieldRelation}, "TEXT"},
		{SchemaField{Type: FieldRelation, Options: map[string]any{"max_select": 2}}, "JSONB"},
		{SchemaField{Type: FieldFile}, "JSONB"},
		{SchemaField{Type: FieldVector, Options: map[string]any{"dimensions": 3}}, "vector(3)"},
		{SchemaField{Type: FieldVector}, "vector"},
		{SchemaField{Type: FieldText}, "TEXT"},
	}
	for _, c := range cases {
		if got := svc.columnTypeForField(c.field); got != c.want {
			t.Errorf("columnTypeForField(%s) = %q, want %q", c.field.Type, got, c.want)
		}
	}
}

func TestFieldToColumnDef(t *testing.T) {
	svc := NewService(nil)
	def := svc.fieldToColumnDef(SchemaField{
		Name:     "title",
		Type:     FieldText,
		Required: true,
		Options:  map[string]any{"default": "x"},
	})
	if def != `"title" TEXT NOT NULL DEFAULT 'x'` {
		t.Errorf("column def = %q", def)
	}
}

func TestBuildColumnDefsForAuthType(t *testing.T) {
	svc := NewService(nil)
	cols := svc.buildColumnDefsForType(TypeAuth, []SchemaField{
		{Name: "id", Type: FieldText}, // skipped: id is always the PK
		{Name: "email", Type: FieldEmail},
	})
	joined := strings.Join(cols, "\n")
	if !strings.Contains(joined, "verified BOOLEAN NOT NULL DEFAULT FALSE") {
		t.Errorf("auth collections must get a verified column:\n%s", joined)
	}
	if strings.Count(joined, `"id"`) != 0 || !strings.HasPrefix(cols[0], "id TEXT PRIMARY KEY") {
		t.Errorf("id handling wrong:\n%s", joined)
	}
	if !strings.Contains(joined, "created_at") || !strings.Contains(joined, "updated_at") {
		t.Errorf("system timestamps missing:\n%s", joined)
	}

	// nil collection falls back to base defaults
	base := svc.buildColumnDefsForCollection(nil)
	if len(base) != 3 {
		t.Errorf("nil collection defs = %v", base)
	}
	withColl := svc.buildColumnDefsForCollection(&Collection{Type: TypeAuth, Schema: []SchemaField{{Name: "email", Type: FieldEmail}}})
	if !strings.Contains(strings.Join(withColl, "\n"), "verified") {
		t.Error("buildColumnDefsForCollection lost the auth type")
	}
}

// ---------------------------------------------------------------------------
// Relation label / auth methods
// ---------------------------------------------------------------------------

func TestBuildRelationLabel(t *testing.T) {
	rec := Record{"id": "r1", "title": "My Post", "email": "a@b.com", "custom": "C"}

	// explicit display fields in their various encodings
	if got := buildRelationLabel(rec, SchemaField{Options: map[string]any{"display_fields": []string{"custom"}}}); got != "C" {
		t.Errorf("[]string = %q", got)
	}
	if got := buildRelationLabel(rec, SchemaField{Options: map[string]any{"display_fields": []any{"custom", "title"}}}); got != "C · My Post" {
		t.Errorf("[]any = %q", got)
	}
	if got := buildRelationLabel(rec, SchemaField{Options: map[string]any{"display_fields": "custom, title"}}); got != "C · My Post" {
		t.Errorf("csv = %q", got)
	}
	// display fields that resolve to nothing fall back to id
	if got := buildRelationLabel(rec, SchemaField{Options: map[string]any{"display_fields": []string{"missing"}}}); got != "r1" {
		t.Errorf("missing display fields = %q", got)
	}
	// no display fields: well-known candidates in priority order
	if got := buildRelationLabel(rec, SchemaField{}); got != "My Post" {
		t.Errorf("candidate fallback = %q", got)
	}
	if got := buildRelationLabel(Record{"id": "r2", "email": "x@y.com"}, SchemaField{}); got != "x@y.com" {
		t.Errorf("email fallback = %q", got)
	}
	if got := buildRelationLabel(Record{"id": "r3"}, SchemaField{}); got != "r3" {
		t.Errorf("id fallback = %q", got)
	}
}

func TestGetAuthMethods(t *testing.T) {
	svc := NewService(nil)

	coll := &Collection{
		Type: TypeAuth,
		Schema: []SchemaField{
			{Name: "email", Type: FieldEmail},
			{Name: "password", Type: FieldPassword},
		},
		Options: map[string]any{"oauthProviders": []any{"google"}},
	}
	m := svc.GetAuthMethods(coll)
	if !m["password"] || !m["otp"] || !m["oauth2"] {
		t.Errorf("methods = %v", m)
	}

	bare := &Collection{Type: TypeAuth, Schema: []SchemaField{{Name: "nick", Type: FieldText}}}
	m = svc.GetAuthMethods(bare)
	if m["password"] || m["otp"] || m["oauth2"] {
		t.Errorf("bare methods = %v", m)
	}

	empty := &Collection{Type: TypeAuth, Schema: []SchemaField{{Name: "email", Type: FieldEmail}}, Options: map[string]any{"oauthProviders": []any{}}}
	if svc.GetAuthMethods(empty)["oauth2"] {
		t.Error("empty provider list should not enable oauth2")
	}
}

// ---------------------------------------------------------------------------
// View collections are read-only
// ---------------------------------------------------------------------------

func TestViewCollectionsRejectWrites(t *testing.T) {
	svc := NewService(nil)
	ctx := context.Background()
	view := &Collection{Name: "v", Type: TypeView}

	if _, err := svc.CreateRecord(ctx, view, map[string]any{"a": 1}); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("CreateRecord = %v, want ErrViewReadOnly", err)
	}
	if _, err := svc.CreateRecords(ctx, view, []map[string]any{{"a": 1}}); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("CreateRecords = %v, want ErrViewReadOnly", err)
	}
	if _, err := svc.CreateBatch(ctx, view, []map[string]any{{"a": 1}}); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("CreateBatch = %v, want ErrViewReadOnly", err)
	}
	if err := svc.UpdateRecord(ctx, view, "id", map[string]any{"a": 1}); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("UpdateRecord = %v, want ErrViewReadOnly", err)
	}
	if err := svc.DeleteRecord(ctx, view, "id"); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("DeleteRecord = %v, want ErrViewReadOnly", err)
	}
	if err := svc.UpdateBatch(ctx, view, map[string]map[string]any{"id": {"a": 1}}); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("UpdateBatch = %v, want ErrViewReadOnly", err)
	}
	if err := svc.DeleteBatch(ctx, view, []string{"id"}); !errors.Is(err, ErrViewReadOnly) {
		t.Errorf("DeleteBatch = %v, want ErrViewReadOnly", err)
	}
}

// ---------------------------------------------------------------------------
// Import guard
// ---------------------------------------------------------------------------

func TestImportCollectionsRejectsEmpty(t *testing.T) {
	svc := NewService(nil)
	if err := svc.ImportCollections(context.Background(), nil, false); err == nil {
		t.Error("empty import should error before touching the database")
	}
	if err := svc.ImportCollections(context.Background(), []map[string]any{}, true); err == nil {
		t.Error("empty import slice should error")
	}
}

// ---------------------------------------------------------------------------
// Vector helpers (pure parts)
// ---------------------------------------------------------------------------

func TestIsVectorFieldAndList(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "docs",
		Schema: []SchemaField{
			{Name: "emb", Type: FieldVector},
			{Name: "title", Type: FieldText},
		},
	}
	if !svc.isVectorField(coll, "emb") {
		t.Error("emb should be a vector field")
	}
	if svc.isVectorField(coll, "title") || svc.isVectorField(coll, "missing") || svc.isVectorField(nil, "emb") {
		t.Error("non-vector lookups should be false")
	}
	if got := vectorFields(coll); len(got) != 1 || got[0].Name != "emb" {
		t.Errorf("vectorFields = %v", got)
	}
}

func TestVectorOptionCoercions(t *testing.T) {
	if got := vectorDimensions(SchemaField{Options: map[string]any{"dimensions": 3}}); got != 3 {
		t.Errorf("int dims = %d", got)
	}
	if got := vectorDimensions(SchemaField{Options: map[string]any{"dimensions": int64(4)}}); got != 4 {
		t.Errorf("int64 dims = %d", got)
	}
	if got := vectorDimensions(SchemaField{Options: map[string]any{"dimensions": "5"}}); got != 5 {
		t.Errorf("string dims = %d", got)
	}
	if got := vectorDimensions(SchemaField{Options: map[string]any{"dimensions": "x"}}); got != 0 {
		t.Errorf("bad string dims = %d", got)
	}
	if got := vectorDimensions(SchemaField{}); got != 0 {
		t.Errorf("missing dims = %d", got)
	}

	if got := vectorDistanceOf(SchemaField{Options: map[string]any{"distance": " INNER "}}); got != DistanceInner {
		t.Errorf("inner distance = %q", got)
	}
	if got := vectorDistanceOf(SchemaField{Options: map[string]any{"distance": "cosine"}}); got != DistanceCosine {
		t.Errorf("cosine distance = %q", got)
	}
	if got := vectorDistanceOf(SchemaField{Options: map[string]any{"distance": "bogus"}}); got != DistanceCosine {
		t.Errorf("bogus distance should default to cosine, got %q", got)
	}

	if got := vectorIndexKind(SchemaField{Options: map[string]any{"index": "none"}}); got != "none" {
		t.Errorf("none index = %q", got)
	}
	if got := vectorIndexKind(SchemaField{Options: map[string]any{"index": ""}}); got != "hnsw" {
		t.Errorf("empty index = %q", got)
	}
	if got := vectorIndexKind(SchemaField{Options: map[string]any{"index": "HNSW"}}); got != "hnsw" {
		t.Errorf("hnsw index = %q", got)
	}
	if got := vectorIndexKind(SchemaField{Options: map[string]any{"index": "weird"}}); got != "hnsw" {
		t.Errorf("unknown index should default to hnsw, got %q", got)
	}
}

func TestEncodeVectorVariants(t *testing.T) {
	if got, err := encodeVector([]float32{1, 2}); err != nil || got != "[1,2]" {
		t.Errorf("float32 = %q, %v", got, err)
	}
	if got, err := encodeVector([]any{json.Number("1.5"), 2}); err != nil || got != "[1.5,2]" {
		t.Errorf("[]any = %q, %v", got, err)
	}
	if _, err := encodeVector([]any{"x"}); err == nil {
		t.Error("non-numeric element should error")
	}
	if got, err := encodeVector(nil); err != nil || got != "" {
		t.Errorf("nil = %q, %v", got, err)
	}
	if got, err := encodeVector("  "); err != nil || got != "" {
		t.Errorf("blank = %q, %v", got, err)
	}
	if _, err := encodeVector("1,2"); err == nil {
		t.Error("unbracketed string should error")
	}
	if _, err := encodeVector(map[string]any{}); err == nil {
		t.Error("unsupported type should error")
	}
}

func TestToFloatJSONNumber(t *testing.T) {
	if got, err := toFloat(json.Number("2.5")); err != nil || got != 2.5 {
		t.Errorf("json.Number = %v, %v", got, err)
	}
	if _, err := toFloat(json.Number("zz")); err == nil {
		t.Error("bad json.Number should error")
	}
	if got, err := toFloat(float32(1.5)); err != nil || got != 1.5 {
		t.Errorf("float32 = %v, %v", got, err)
	}
	if got, err := toFloat(int64(2)); err != nil || got != 2 {
		t.Errorf("int64 = %v, %v", got, err)
	}
	if _, err := toFloat("nope"); err == nil {
		t.Error("string should error")
	}
}

func TestVectorSearchValidation(t *testing.T) {
	svc := NewService(nil)
	ctx := context.Background()
	coll := &Collection{
		Name: "docs",
		Schema: []SchemaField{
			{Name: "emb", Type: FieldVector, Options: map[string]any{"dimensions": 2}},
			{Name: "title", Type: FieldText},
		},
	}

	if _, err := svc.VectorSearch(ctx, nil, VectorSearchOptions{}); err == nil {
		t.Error("nil collection should error")
	}
	if _, err := svc.VectorSearch(ctx, coll, VectorSearchOptions{Field: "title"}); err == nil {
		t.Error("non-vector field should error")
	}
	if _, err := svc.VectorSearch(ctx, coll, VectorSearchOptions{Field: "emb", Vector: []any{1.0, 2.0}, Distance: "bogus"}); err == nil {
		t.Error("unsupported distance should error")
	}
	if _, err := svc.VectorSearch(ctx, coll, VectorSearchOptions{Field: "emb", Vector: 42}); err == nil {
		t.Error("bad vector value should error")
	}
	if _, err := svc.VectorSearch(ctx, coll, VectorSearchOptions{Field: "emb", Vector: ""}); err == nil {
		t.Error("empty query vector should error")
	}
	if _, err := svc.VectorSearch(ctx, coll, VectorSearchOptions{Field: "emb", Vector: []any{1.0, 2.0, 3.0}}); err == nil {
		t.Error("dimension mismatch should error")
	}
}

// ---------------------------------------------------------------------------
// RLS pure helpers
// ---------------------------------------------------------------------------

func TestUnsupportedPlaceholderErrorMessage(t *testing.T) {
	err := &UnsupportedPlaceholderError{
		Collection:   "posts",
		Operation:    "list",
		Placeholders: []string{"@request.body.x", "@request.query.y"},
	}
	msg := err.Error()
	for _, want := range []string{"posts", "list", "@request.body.x", "@request.query.y"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
}

func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("dedupeStrings = %v", got)
	}
	if got := dedupeStrings(nil); len(got) != 0 {
		t.Errorf("nil input = %v", got)
	}
}

func TestCompileExprComparison(t *testing.T) {
	// ordering operators are always allowed
	for _, op := range []filter.Op{filter.OpEq, filter.OpNeq, filter.OpGt, filter.OpGte, filter.OpLt, filter.OpLte} {
		got, err := compileExprComparison("a", op, "b", false)
		if err != nil || !strings.Contains(got, string(op)) {
			t.Errorf("op %s = %q, %v", op, got, err)
		}
	}

	// non-ordering operators between two plain columns are a hard error
	if _, err := compileExprComparison("a", filter.OpLike, "b", false); err == nil {
		t.Error("LIKE between two columns must be rejected (would degrade to permissive)")
	}

	// macro-right comparisons compile to safe SQL shapes
	macroCases := []struct {
		op   filter.Op
		want string
	}{
		{filter.OpLike, "ILIKE"},
		{filter.OpNLike, "NOT ILIKE"},
		{filter.OpIn, "@>"},
		{filter.OpNIn, "NOT ("},
		{filter.OpILike, "EXISTS"},
		{filter.OpNILike, "NOT EXISTS"},
	}
	for _, c := range macroCases {
		got, err := compileExprComparison("col", c.op, "macro", true)
		if err != nil || !strings.Contains(got, c.want) {
			t.Errorf("macro op %s = %q, %v (want fragment %q)", c.op, got, err, c.want)
		}
	}

	if _, err := compileExprComparison("a", filter.Op("bogus"), "b", true); err == nil {
		t.Error("unknown operator should error")
	}
}

func TestSQLValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{true, "TRUE"},
		{false, "FALSE"},
		{float64(1.5), "1.5"},
		{"a'b", "'a''b'"},
		{42, "'42'"}, // non-float numbers go through the default branch
	}
	for _, c := range cases {
		if got := sqlValue(c.in); got != c.want {
			t.Errorf("sqlValue(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompileRuleExprOperators(t *testing.T) {
	cases := []struct {
		rule string
		want string
	}{
		// null semantics
		{"deleted = null", `"deleted" IS NULL`},
		{"deleted != null", `"deleted" IS NOT NULL`},
		// ordering against literals
		{"age > 18", `"age" > 18`},
		{"age >= 18", `"age" >= 18`},
		{"age < 65", `"age" < 65`},
		{"age <= 65", `"age" <= 65`},
		// contains / array operators against literals
		{"title ~ 'foo'", `"title" ILIKE '%foo%'`},
		{"title !~ 'foo'", `"title" NOT ILIKE '%foo%'`},
		{"tags ?= 'go'", `"tags"::jsonb @> to_jsonb('go'::text)`},
		{"tags ?!= 'go'", `NOT ("tags"::jsonb @> to_jsonb('go'::text))`},
		{"tags ?~ 'go'", `EXISTS (SELECT 1 FROM jsonb_array_elements_text("tags"::jsonb) elem WHERE elem ILIKE '%go%')`},
		{"tags ?!~ 'go'", `NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text("tags"::jsonb) elem WHERE elem ILIKE '%go%')`},
		// boolean literals and combinators
		{"published = true && views > 10", `("published" = TRUE AND "views" > 10)`},
		{"a = 1 || b = 2", `("a" = '1' OR "b" = '2')`},
		// macros on the right side support contains semantics
		{"members ?= @request.auth.id", `"members"::jsonb @> to_jsonb(current_setting('gresbase.auth_id', true)::text)`},
		{"title ~ @request.auth.email", `"title" ILIKE ('%' || current_setting('gresbase.auth_email', true) || '%')`},
	}
	for _, c := range cases {
		got, unsupported, err := compileRuleExpr(c.rule)
		if err != nil {
			t.Errorf("compileRuleExpr(%q): %v", c.rule, err)
			continue
		}
		if len(unsupported) != 0 {
			t.Errorf("compileRuleExpr(%q): unexpected unsupported %v", c.rule, unsupported)
			continue
		}
		// integer literals may compile as numbers or strings depending on the
		// parser; normalize the two known encodings before comparing.
		if got != c.want && got != strings.ReplaceAll(c.want, "'", "") {
			t.Errorf("compileRuleExpr(%q) = %q, want %q", c.rule, got, c.want)
		}
	}

	// unsupported placeholders are reported, and duplicates deduped
	_, unsupported, err := compileRuleExpr("@request.body.x = 'a' && @request.body.x = 'b'")
	if err != nil {
		t.Fatalf("unsupported placeholder rule: %v", err)
	}
	if len(unsupported) != 1 || unsupported[0] != "@request.body.x" {
		t.Errorf("unsupported = %v", unsupported)
	}

	// invalid rules are hard errors
	if _, _, err := compileRuleExpr("title ="); err == nil {
		t.Error("malformed rule should error")
	}
}

// ---------------------------------------------------------------------------
// Index guards (fake runner — no database)
// ---------------------------------------------------------------------------

// fakeRunner satisfies dbRunner and records executed SQL. Statements
// containing failOn (when set) return an error.
type fakeRunner struct {
	execs  []string
	failOn string
}

func (f *fakeRunner) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return pgconn.CommandTag{}, fmt.Errorf("simulated failure")
	}
	f.execs = append(f.execs, sql)
	return pgconn.CommandTag{}, nil
}

func (f *fakeRunner) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeRunner) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

func TestApplyUserIndexesOnlyAllowsCreateIndex(t *testing.T) {
	svc := NewService(nil)
	runner := &fakeRunner{}
	coll := &Collection{
		Name: "posts",
		Indexes: []string{
			"CREATE INDEX idx_a ON posts(a)",
			"create unique index idx_b on posts(b)",
			"DROP TABLE posts",                             // not an index: ignored
			"CREATE INDEX idx_c ON posts(c); DROP TABLE x", // multiple statements: ignored
			"",
			"   ",
		},
	}

	svc.applyUserIndexes(context.Background(), runner, coll)

	if len(runner.execs) != 2 {
		t.Fatalf("executed statements = %v, want exactly the two CREATE INDEX ones", runner.execs)
	}
	for _, sql := range runner.execs {
		upper := strings.ToUpper(sql)
		if !strings.HasPrefix(upper, "CREATE INDEX") && !strings.HasPrefix(upper, "CREATE UNIQUE INDEX") {
			t.Errorf("non-index statement executed: %q", sql)
		}
	}
}

func TestEnsureVectorIndexes(t *testing.T) {
	svc := NewService(nil)
	coll := &Collection{
		Name: "docs",
		Schema: []SchemaField{
			{Name: "skip", Type: FieldVector, Options: map[string]any{"index": "none"}},
			{Name: "default_idx", Type: FieldVector},
			{Name: "flat", Type: FieldVector, Options: map[string]any{"index": "ivfflat", "distance": "l2"}},
			{Name: "title", Type: FieldText},
		},
	}

	runner := &fakeRunner{}
	svc.ensureVectorIndexes(context.Background(), runner, coll)
	if len(runner.execs) != 2 {
		t.Fatalf("execs = %v, want hnsw + ivfflat", runner.execs)
	}
	if !strings.Contains(runner.execs[0], "USING hnsw") || !strings.Contains(runner.execs[0], "vector_cosine_ops") {
		t.Errorf("default index = %q, want hnsw + cosine opclass", runner.execs[0])
	}
	if !strings.Contains(runner.execs[1], "USING ivfflat") || !strings.Contains(runner.execs[1], "vector_l2_ops") {
		t.Errorf("ivfflat index = %q, want ivfflat + l2 opclass", runner.execs[1])
	}

	// older pgvector without hnsw: fall back to ivfflat
	fallback := &fakeRunner{failOn: "USING hnsw"}
	svc.ensureVectorIndexes(context.Background(), fallback, &Collection{
		Name:   "docs",
		Schema: []SchemaField{{Name: "emb", Type: FieldVector}},
	})
	if len(fallback.execs) != 1 || !strings.Contains(fallback.execs[0], "USING ivfflat") {
		t.Errorf("hnsw failure should fall back to ivfflat, got %v", fallback.execs)
	}
}

func TestEnsureVectorExtension(t *testing.T) {
	svc := NewService(nil)

	ok := &fakeRunner{}
	if err := svc.ensureVectorExtension(context.Background(), ok); err != nil {
		t.Errorf("extension create: %v", err)
	}
	if len(ok.execs) != 1 || !strings.Contains(ok.execs[0], "CREATE EXTENSION IF NOT EXISTS vector") {
		t.Errorf("execs = %v", ok.execs)
	}

	bad := &fakeRunner{failOn: "CREATE EXTENSION"}
	if err := svc.ensureVectorExtension(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "pgvector") {
		t.Errorf("missing extension should produce a friendly error, got %v", err)
	}
}
