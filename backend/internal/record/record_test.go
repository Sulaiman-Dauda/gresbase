package record

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	r := New(nil)
	if r == nil {
		t.Fatal("expected non-nil Record")
	}
	if r.Data() == nil {
		t.Error("expected non-nil data map")
	}

	r2 := New(map[string]any{"name": "test"})
	if r2.GetString("name") != "test" {
		t.Errorf("expected name=test")
	}
}

func TestNewFromJSON(t *testing.T) {
	raw := json.RawMessage(`{"id":"abc123","title":"Hello","views":42}`)
	r, err := NewFromJSON(raw)
	if err != nil {
		t.Fatalf("NewFromJSON: %v", err)
	}
	if r.ID() != "abc123" {
		t.Errorf("expected ID abc123, got %s", r.ID())
	}
	if r.GetString("title") != "Hello" {
		t.Errorf("expected title Hello")
	}
	if r.GetInt("views") != 42 {
		t.Errorf("expected views 42, got %d", r.GetInt("views"))
	}
}

func TestNewFromJSON_Invalid(t *testing.T) {
	_, err := NewFromJSON(json.RawMessage(`{invalid}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRecord_Get(t *testing.T) {
	r := New(map[string]any{
		"name": "Alice",
		"age":  float64(30),
	})

	if r.Get("name") != "Alice" {
		t.Errorf("expected name Alice")
	}
	if r.Get("nonexistent") != nil {
		t.Errorf("expected nil for nonexistent field")
	}
}

func TestRecord_GetString(t *testing.T) {
	r := New(map[string]any{
		"text":  "hello",
		"bytes": []byte("world"),
		"num":   42,
	})

	if r.GetString("text") != "hello" {
		t.Errorf("expected hello")
	}
	if r.GetString("bytes") != "world" {
		t.Errorf("expected world")
	}
	if r.GetString("num") != "42" {
		t.Errorf("expected 42")
	}
	if r.GetString("missing") != "" {
		t.Errorf("expected empty string for missing")
	}
}

func TestRecord_GetInt(t *testing.T) {
	r := New(map[string]any{
		"float":  float64(42.0),
		"int":    100,
		"int64":  int64(200),
		"jn":     json.Number("300"),
		"string": "not a number",
	})

	if r.GetInt("float") != 42 {
		t.Errorf("expected 42")
	}
	if r.GetInt("int") != 100 {
		t.Errorf("expected 100")
	}
	if r.GetInt("int64") != 200 {
		t.Errorf("expected 200")
	}
	if r.GetInt("jn") != 300 {
		t.Errorf("expected 300")
	}
	if r.GetInt("string") != 0 {
		t.Errorf("expected 0 for non-number string")
	}
	if r.GetInt("missing") != 0 {
		t.Errorf("expected 0 for missing")
	}
}

func TestRecord_GetFloat(t *testing.T) {
	r := New(map[string]any{
		"f":     float64(3.14),
		"i":     42,
		"i64":   int64(100),
		"jn":    json.Number("2.718"),
	})

	if r.GetFloat("f") != 3.14 {
		t.Errorf("expected 3.14")
	}
	if r.GetFloat("i") != 42.0 {
		t.Errorf("expected 42.0")
	}
	if r.GetFloat("i64") != 100.0 {
		t.Errorf("expected 100.0")
	}
	if r.GetFloat("jn") != 2.718 {
		t.Errorf("expected 2.718, got %f", r.GetFloat("jn"))
	}
}

func TestRecord_GetBool(t *testing.T) {
	r := New(map[string]any{
		"b":      true,
		"str_t":  "true",
		"str_1":  "1",
		"num_1":  float64(1),
		"num_0":  float64(0),
		"str_f":  "false",
	})

	if !r.GetBool("b") {
		t.Error("expected true")
	}
	if !r.GetBool("str_t") {
		t.Error("expected true for string true")
	}
	if !r.GetBool("str_1") {
		t.Error("expected true for string 1")
	}
	if !r.GetBool("num_1") {
		t.Error("expected true for float64 1")
	}
	if r.GetBool("num_0") {
		t.Error("expected false for float64 0")
	}
	if r.GetBool("str_f") {
		t.Error("expected false for string false")
	}
	if r.GetBool("missing") {
		t.Error("expected false for missing")
	}
}

func TestRecord_GetTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	r := New(map[string]any{
		"ts":      now,
		"rfc3339": now.Format(time.RFC3339),
		"date":    "2024-01-15",
	})

	got := r.GetTime("ts")
	if !got.Equal(now) {
		t.Errorf("expected %v, got %v", now, got)
	}

	got2 := r.GetTime("rfc3339")
	if !got2.Equal(now) {
		t.Errorf("RFC3339 parse failed: expected %v, got %v", now, got2)
	}

	got3 := r.GetTime("date")
	if got3.IsZero() {
		t.Error("expected non-zero for date string")
	}
	if got3.Year() != 2024 || got3.Month() != 1 || got3.Day() != 15 {
		t.Errorf("expected 2024-01-15, got %v", got3)
	}

	if !r.GetTime("missing").IsZero() {
		t.Error("expected zero time for missing")
	}
}

func TestRecord_GetSlice(t *testing.T) {
	r := New(map[string]any{
		"arr":      []any{"a", "b", "c"},
		"str_arr":  []string{"x", "y"},
	})

	sl := r.GetSlice("arr")
	if len(sl) != 3 {
		t.Errorf("expected 3 items, got %d", len(sl))
	}

	sl2 := r.GetSlice("str_arr")
	if len(sl2) != 2 {
		t.Errorf("expected 2 items, got %d", len(sl2))
	}
	if sl2[0] != "x" {
		t.Errorf("expected x")
	}

	if r.GetSlice("missing") != nil {
		t.Error("expected nil for missing")
	}
}

func TestRecord_Set(t *testing.T) {
	r := New(map[string]any{"name": "old"})
	r.Set("name", "new")
	if r.GetString("name") != "new" {
		t.Errorf("expected new, got %s", r.GetString("name"))
	}
	r.Set("email", "test@test.com")
	if r.GetString("email") != "test@test.com" {
		t.Errorf("expected test@test.com")
	}
}

func TestRecord_Has(t *testing.T) {
	r := New(map[string]any{
		"name":  "Alice",
		"empty": nil,
	})

	if !r.Has("name") {
		t.Error("expected has name")
	}
	if r.Has("empty") {
		t.Error("expected not has empty (nil value)")
	}
	if r.Has("missing") {
		t.Error("expected not has missing")
	}
}

func TestRecord_Expand(t *testing.T) {
	r := New(map[string]any{"id": "post1", "title": "Test"})

	r.SetExpand("author", map[string]any{"id": "user1", "name": "Alice"})

	if !r.HasExpand() {
		t.Error("expected has expand")
	}
	if r.GetExpand("author") == nil {
		t.Error("expected non-nil author expand")
	}
	exp := r.Expand()
	if len(exp) != 1 {
		t.Errorf("expected 1 expand, got %d", len(exp))
	}
}

func TestRecord_Pick(t *testing.T) {
	r := New(map[string]any{
		"id":    "rec1",
		"title": "Hello",
		"body":  "World",
		"views": 42,
	})

	picked := r.Pick("title,views")
	if picked.GetString("title") != "Hello" {
		t.Errorf("expected title Hello")
	}
	if picked.GetInt("views") != 42 {
		t.Errorf("expected views 42")
	}
	if picked.GetString("body") != "" {
		t.Error("expected body to be excluded")
	}
	if picked.ID() != "rec1" {
		t.Error("expected id always included")
	}
}

func TestRecord_Pick_Star(t *testing.T) {
	r := New(map[string]any{"id": "rec1", "title": "Hello", "body": "World"})
	picked := r.Pick("*")
	if picked.GetString("title") != "Hello" {
		t.Errorf("expected all fields with *")
	}
	if picked.GetString("body") != "World" {
		t.Errorf("expected all fields with *")
	}
}

func TestRecord_Pick_Empty(t *testing.T) {
	r := New(map[string]any{"id": "rec1", "title": "Hello"})
	picked := r.Pick("")
	if picked.GetString("title") != "Hello" {
		t.Errorf("empty pick should return all")
	}
}

func TestRecord_Clone(t *testing.T) {
	r := New(map[string]any{
		"id":    "rec1",
		"title": "Original",
	})
	r.SetExpand("author", map[string]any{"id": "user1"})

	clone := r.Clone()
	clone.Set("title", "Modified")

	if r.GetString("title") != "Original" {
		t.Error("original should not be modified")
	}
	if clone.GetString("title") != "Modified" {
		t.Error("clone should be modified")
	}
	if !clone.HasExpand() {
		t.Error("clone should have expand")
	}
}

func TestRecord_JSON(t *testing.T) {
	r := New(map[string]any{
		"id":    "rec1",
		"title": "Hello",
	})
	r.SetExpand("author", map[string]any{"name": "Alice"})

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result["id"] != "rec1" {
		t.Errorf("expected id rec1")
	}
	if result["title"] != "Hello" {
		t.Errorf("expected title Hello")
	}
	if result["@expand"] == nil {
		t.Error("expected @expand in JSON")
	}
}

func TestRecord_UnmarshalJSON(t *testing.T) {
	data := []byte(`{"id":"rec1","title":"Hello","@expand":{"author":{"name":"Alice"}}}`)
	r := &Record{}
	if err := json.Unmarshal(data, r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ID() != "rec1" {
		t.Errorf("expected id rec1")
	}
	if r.GetString("title") != "Hello" {
		t.Errorf("expected title Hello")
	}
	if r.GetExpand("author") == nil {
		t.Error("expected author expand")
	}
}

func TestRecordList_Pick(t *testing.T) {
	list := RecordList{
		New(map[string]any{"id": "rec1", "title": "A", "body": "X"}),
		New(map[string]any{"id": "rec2", "title": "B", "body": "Y"}),
	}

	picked := list.Pick("title")
	if len(picked) != 2 {
		t.Errorf("expected 2 records")
	}
	if picked[0].GetString("body") != "" {
		t.Error("expected body excluded")
	}
	if picked[1].GetString("title") != "B" {
		t.Errorf("expected title B")
	}
}

func TestRecordList_IDs(t *testing.T) {
	list := RecordList{
		New(map[string]any{"id": "rec1"}),
		New(map[string]any{"id": "rec2"}),
		New(map[string]any{"id": "rec3"}),
	}

	ids := list.IDs()
	if len(ids) != 3 {
		t.Errorf("expected 3 ids")
	}
	if ids[0] != "rec1" {
		t.Errorf("expected rec1")
	}
}

func TestRecordList_Map(t *testing.T) {
	list := RecordList{
		New(map[string]any{"id": "rec1", "val": "a"}),
		New(map[string]any{"id": "rec2", "val": "b"}),
	}

	maps := list.Map()
	if len(maps) != 2 {
		t.Errorf("expected 2 maps")
	}
	if maps[0]["id"] != "rec1" {
		t.Errorf("expected rec1")
	}
}

func TestRecordList_Len(t *testing.T) {
	list := RecordList{New(nil), New(nil), New(nil)}
	if list.Len() != 3 {
		t.Errorf("expected len 3")
	}
}

func TestNormalizeRecordForDB(t *testing.T) {
	schema := map[string]string{
		"title": "text",
		"views": "number",
		"published": "bool",
		"tags":      "json",
	}

	data := map[string]any{
		"title":     "Hello",
		"views":     json.Number("42"),
		"published": true,
		"tags":      map[string]any{"foo": "bar"},
	}

	norm := NormalizeRecordForDB(data, schema)
	if norm["title"] != "Hello" {
		t.Errorf("expected title Hello")
	}
	if _, ok := norm["created_at"]; !ok {
		t.Error("expected created_at to be added")
	}
	if _, ok := norm["updated_at"]; !ok {
		t.Error("expected updated_at to be added")
	}
}

func TestSchemaFieldTypeMap(t *testing.T) {
	schema := []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}{
		{Name: "title", Type: "text"},
		{Name: "views", Type: "number"},
		{Name: "author", Type: "relation"},
	}

	m := SchemaFieldTypeMap(schema)
	if len(m) != 3 {
		t.Errorf("expected 3 entries, got %d", len(m))
	}
	if m["title"] != "text" {
		t.Errorf("expected title=text")
	}
	if m["views"] != "number" {
		t.Errorf("expected views=number")
	}
}

func TestID(t *testing.T) {
	// ID() uses GetString("id")
	r := New(map[string]any{"id": "custom_id"})
	if r.ID() != "custom_id" {
		t.Errorf("expected custom_id")
	}

	// No id field
	r2 := New(map[string]any{"name": "test"})
	if r2.ID() != "" {
		t.Errorf("expected empty string for missing id")
	}
}
