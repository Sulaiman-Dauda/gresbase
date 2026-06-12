package fields

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// Shared helpers (field.go)
// ---------------------------------------------------------------------------

func TestBaseFieldAccessors(t *testing.T) {
	f := NewTextField("title")

	if f.ID() == "" {
		t.Error("new field should get a generated ID")
	}
	if f.Type() != TypeText {
		t.Errorf("Type() = %q, want %q", f.Type(), TypeText)
	}
	if f.Unique() || f.System() || f.Required() {
		t.Error("flags should default to false")
	}

	f.SetUnique(true)
	f.SetSystem(true)
	f.SetRequired(true)
	f.SetID("fixed-id")
	if !f.Unique() || !f.System() || !f.Required() {
		t.Error("setters did not stick")
	}
	if f.ID() != "fixed-id" {
		t.Errorf("ID() = %q, want fixed-id", f.ID())
	}

	if f.Options() == nil {
		t.Error("Options() should never be nil after construction")
	}
	f.BaseField.SetOptions(map[string]any{"k": "v"})
	if f.Options()["k"] != "v" {
		t.Error("SetOptions did not store options")
	}
	f.BaseField.SetOptions(nil)
	if f.Options() == nil {
		t.Error("SetOptions(nil) should reset to an empty map, not nil")
	}
}

func TestPGLiteral(t *testing.T) {
	if got := PGLiteral("O'Brien"); got != "'O''Brien'" {
		t.Errorf("PGLiteral = %q", got)
	}
	if got := PGLiteral("plain"); got != "'plain'" {
		t.Errorf("PGLiteral = %q", got)
	}
}

func TestEnsureJSON(t *testing.T) {
	if b, err := EnsureJSON([]byte(`{"a":1}`)); err != nil || string(b) != `{"a":1}` {
		t.Errorf("valid []byte: %q, %v", b, err)
	}
	if _, err := EnsureJSON([]byte(`{bad`)); err == nil {
		t.Error("invalid []byte should error")
	}
	if b, err := EnsureJSON(`[1,2]`); err != nil || string(b) != `[1,2]` {
		t.Errorf("valid string: %q, %v", b, err)
	}
	if _, err := EnsureJSON(`not json`); err == nil {
		t.Error("invalid string should error")
	}
	if b, err := EnsureJSON(map[string]any{"x": true}); err != nil || string(b) != `{"x":true}` {
		t.Errorf("marshalable value: %q, %v", b, err)
	}
	if _, err := EnsureJSON(make(chan int)); err == nil {
		t.Error("unmarshalable value should error")
	}
}

func TestParseDateFormats(t *testing.T) {
	want := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)

	cases := []string{
		"2024-03-15T10:30:00Z",
		"2024-03-15T10:30:00",
		"2024-03-15 10:30:00",
	}
	for _, c := range cases {
		got, err := ParseDate(c)
		if err != nil {
			t.Errorf("ParseDate(%q): %v", c, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("ParseDate(%q) = %v, want %v", c, got, want)
		}
	}

	if got, err := ParseDate("2024-03-15"); err != nil || got.Year() != 2024 || got.Month() != 3 {
		t.Errorf("date-only parse failed: %v, %v", got, err)
	}

	now := time.Now()
	if got, err := ParseDate(now); err != nil || !got.Equal(now) {
		t.Errorf("time.Time passthrough failed: %v, %v", got, err)
	}
	if got, err := ParseDate(pgtype.Timestamptz{Time: want, Valid: true}); err != nil || !got.Equal(want) {
		t.Errorf("pgtype.Timestamptz failed: %v, %v", got, err)
	}
	if got, err := ParseDate(pgtype.Timestamp{Time: want, Valid: true}); err != nil || !got.Equal(want) {
		t.Errorf("pgtype.Timestamp failed: %v, %v", got, err)
	}

	if _, err := ParseDate("definitely not a date"); err == nil {
		t.Error("garbage string should error")
	}
	if _, err := ParseDate(12345); err == nil {
		t.Error("unsupported type should error")
	}
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{5, 5, true},
		{int64(7), 7, true},
		{float64(3.9), 3, true},
		{float32(2.5), 2, true},
		{"5", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := toInt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("toInt(%v) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestToFloatConversions(t *testing.T) {
	for _, c := range []struct {
		in   any
		want float64
		ok   bool
	}{
		{1.5, 1.5, true},
		{float32(2), 2, true},
		{3, 3, true},
		{int64(4), 4, true},
		{"5", 0, false},
	} {
		got, ok := toFloat(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("toFloat(%v) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// FileField
// ---------------------------------------------------------------------------

func TestFileFieldValidateMulti(t *testing.T) {
	f := NewFileField("attachments")
	f.MaxSelect = 2

	// nil, optional, multi → empty slice
	v, err := f.Validate(nil)
	if err != nil {
		t.Fatalf("nil optional: %v", err)
	}
	if arr, ok := v.([]string); !ok || len(arr) != 0 {
		t.Errorf("nil optional multi = %#v, want empty []string", v)
	}

	if _, err := f.Validate([]string{"a.png", "b.png", "c.png"}); err == nil {
		t.Error("exceeding MaxSelect should error")
	}
	if v, err := f.Validate([]string{"a.png"}); err != nil || v.([]string)[0] != "a.png" {
		t.Errorf("valid list: %#v, %v", v, err)
	}
	if v, err := f.Validate([]string{}); err != nil {
		t.Errorf("empty list optional: %v", err)
	} else if len(v.([]string)) != 0 {
		t.Errorf("empty list = %#v", v)
	}

	f.SetRequired(true)
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}
	if _, err := f.Validate([]string{}); err == nil {
		t.Error("empty list required should error")
	}
	if _, err := f.Validate("  "); err == nil {
		t.Error("blank string required should error")
	}
	if _, err := f.Validate(42); err == nil {
		t.Error("non string/slice should error")
	}
}

func TestFileFieldMarshalUnmarshal(t *testing.T) {
	single := NewFileField("doc")
	got, err := single.Marshal("a.png")
	if err != nil {
		t.Fatalf("marshal single: %v", err)
	}
	if string(got.([]byte)) != `"a.png"` {
		t.Errorf("single marshal = %s", got)
	}

	multi := NewFileField("docs")
	multi.MaxSelect = 3
	got, err = multi.Marshal([]string{"a.png", "b.png"})
	if err != nil {
		t.Fatalf("marshal multi: %v", err)
	}
	if string(got.([]byte)) != `["a.png","b.png"]` {
		t.Errorf("multi marshal = %s", got)
	}

	// Unmarshal multi paths
	if v, _ := multi.Unmarshal(nil); len(v.([]string)) != 0 {
		t.Errorf("multi nil = %#v", v)
	}
	if v, _ := multi.Unmarshal([]byte(`["x"]`)); v.([]string)[0] != "x" {
		t.Errorf("multi []byte = %#v", v)
	}
	if v, _ := multi.Unmarshal([]byte(`not-json`)); len(v.([]string)) != 0 {
		t.Errorf("multi bad []byte = %#v", v)
	}
	if v, _ := multi.Unmarshal(`["y","z"]`); len(v.([]string)) != 2 {
		t.Errorf("multi string json = %#v", v)
	}
	if v, _ := multi.Unmarshal("  "); len(v.([]string)) != 0 {
		t.Errorf("multi blank string = %#v", v)
	}
	if v, _ := multi.Unmarshal("plain.png"); v.([]string)[0] != "plain.png" {
		t.Errorf("multi plain string = %#v", v)
	}
	if v, _ := multi.Unmarshal([]string{"p"}); v.([]string)[0] != "p" {
		t.Errorf("multi []string = %#v", v)
	}

	// Unmarshal single paths
	if v, _ := single.Unmarshal(nil); v != "" {
		t.Errorf("single nil = %#v", v)
	}
	if v, _ := single.Unmarshal(`"q.png"`); v != "q.png" {
		t.Errorf("single json string = %#v", v)
	}
	if v, _ := single.Unmarshal("raw.png"); v != "raw.png" {
		t.Errorf("single raw string = %#v", v)
	}
	if v, _ := single.Unmarshal([]byte(`"b.png"`)); v != "b.png" {
		t.Errorf("single json []byte = %#v", v)
	}
	if v, _ := single.Unmarshal([]byte("rawbytes")); v != "rawbytes" {
		t.Errorf("single raw []byte = %#v", v)
	}
	if v, _ := single.Unmarshal(7); v != "7" {
		t.Errorf("single other = %#v", v)
	}
}

func TestFileFieldSetOptionsAndClone(t *testing.T) {
	f := NewFileField("img")
	f.SetOptions(map[string]any{
		"max_select": float64(5),
		"max_size":   float64(1024),
		"mime_types": []any{"image/png", "image/jpeg"},
		"thumbs":     []any{"100x100"},
		"protected":  true,
	})
	if f.MaxSelect != 5 || f.MaxSize != 1024 || !f.Protected {
		t.Errorf("options not applied: %+v", f)
	}
	if len(f.MimeTypes) != 2 || f.MimeTypes[0] != "image/png" {
		t.Errorf("mime types = %v", f.MimeTypes)
	}
	if len(f.Thumbs) != 1 || f.Thumbs[0] != "100x100" {
		t.Errorf("thumbs = %v", f.Thumbs)
	}

	clone := f.Clone().(*FileField)
	if clone.MaxSelect != 5 || clone.Name() != "img" {
		t.Errorf("clone lost properties: %+v", clone)
	}
	clone.Options()["new"] = true
	if _, leaked := f.Options()["new"]; leaked {
		t.Error("clone options map is shared with the original")
	}

	if f.PGType() != "TEXT" || f.PGDefault() != "''" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
	req := NewFileField("r")
	req.SetRequired(true)
	if !strings.Contains(req.ColumnDef(), "NOT NULL") {
		t.Errorf("required ColumnDef = %q", req.ColumnDef())
	}
}

// ---------------------------------------------------------------------------
// SelectField
// ---------------------------------------------------------------------------

func TestSelectFieldMultiValidate(t *testing.T) {
	f := NewSelectField("tags")
	f.SetOptions(map[string]any{
		"values":     []any{"a", "b", "c"},
		"max_select": 2,
		"multiple":   true,
	})
	if !f.Multiple || f.MaxSelect != 2 || len(f.Values) != 3 {
		t.Fatalf("options not applied: %+v", f)
	}

	v, err := f.Validate([]any{"a", "b"})
	if err != nil {
		t.Fatalf("valid multi: %v", err)
	}
	if arr := v.([]string); len(arr) != 2 || arr[0] != "a" {
		t.Errorf("multi = %#v", arr)
	}

	// comma string input with whitespace trimming and dedupe
	// (the max_select check counts raw values, so stay within the limit)
	v, err = f.Validate("a,a ")
	if err != nil {
		t.Fatalf("csv multi: %v", err)
	}
	if arr := v.([]string); len(arr) != 1 || arr[0] != "a" {
		t.Errorf("csv dedupe = %#v", arr)
	}

	if _, err := f.Validate([]string{"a", "b", "c"}); err == nil {
		t.Error("exceeding max_select should error")
	}
	if _, err := f.Validate([]any{"nope"}); err == nil {
		t.Error("value outside allowed set should error")
	}
	if _, err := f.Validate(42); err == nil {
		t.Error("non-array multi should error")
	}
	if v, err := f.Validate([]string{}); err != nil || v != nil {
		t.Errorf("empty optional multi = %#v, %v", v, err)
	}

	f.SetRequired(true)
	if _, err := f.Validate([]string{}); err == nil {
		t.Error("empty required multi should error")
	}
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}
}

func TestSelectFieldMarshalUnmarshal(t *testing.T) {
	multi := NewSelectField("tags")
	multi.Multiple = true
	multi.Values = []string{"a", "b"}

	got, err := multi.Marshal([]string{"a", "b"})
	if err != nil || got != "a,b" {
		t.Errorf("multi marshal = %#v, %v", got, err)
	}
	if v, _ := multi.Unmarshal("a,b"); len(v.([]string)) != 2 {
		t.Errorf("multi unmarshal = %#v", v)
	}

	single := NewSelectField("status")
	if got, err := single.Marshal("x"); err != nil || got != "x" {
		t.Errorf("single marshal = %#v, %v", got, err)
	}
	if v, _ := single.Unmarshal("x"); v != "x" {
		t.Errorf("single unmarshal = %#v", v)
	}
	if v, _ := single.Unmarshal(nil); v != nil {
		t.Errorf("nil unmarshal = %#v", v)
	}
	if v, _ := single.Unmarshal("  "); v != nil {
		t.Errorf("blank unmarshal = %#v", v)
	}

	clone := multi.Clone().(*SelectField)
	if !clone.Multiple || len(clone.Values) != 2 {
		t.Errorf("clone lost properties: %+v", clone)
	}
	if multi.PGType() != "TEXT" || multi.PGDefault() != "" {
		t.Errorf("PGType/PGDefault = %q/%q", multi.PGType(), multi.PGDefault())
	}
}

// ---------------------------------------------------------------------------
// GeoPointField
// ---------------------------------------------------------------------------

func TestGeoPointBounds(t *testing.T) {
	f := NewGeoPointField("loc")

	cases := []struct {
		lat, lon float64
		ok       bool
	}{
		{0, 0, true},
		{90, 180, true},
		{-90, -180, true},
		{90.001, 0, false},
		{-90.001, 0, false},
		{0, 180.001, false},
		{0, -180.001, false},
	}
	for _, c := range cases {
		_, err := f.Validate(map[string]any{"lat": c.lat, "lon": c.lon})
		if c.ok && err != nil {
			t.Errorf("(%v,%v): unexpected error %v", c.lat, c.lon, err)
		}
		if !c.ok && err == nil {
			t.Errorf("(%v,%v): expected out-of-bounds error", c.lat, c.lon)
		}
	}
}

func TestGeoPointParseVariants(t *testing.T) {
	f := NewGeoPointField("loc")

	check := func(raw any, lat, lon float64) {
		t.Helper()
		v, err := f.Validate(raw)
		if err != nil {
			t.Errorf("Validate(%#v): %v", raw, err)
			return
		}
		gp := v.(*GeoPoint)
		if gp.Latitude != lat || gp.Longitude != lon {
			t.Errorf("Validate(%#v) = (%v,%v), want (%v,%v)", raw, gp.Latitude, gp.Longitude, lat, lon)
		}
	}

	check(GeoPoint{Latitude: 1, Longitude: 2}, 1, 2)
	check(&GeoPoint{Latitude: 3, Longitude: 4}, 3, 4)
	check(map[string]any{"lat": 5.0, "lon": 6.0}, 5, 6)
	check(map[string]any{"latitude": 7.0, "longitude": 8.0}, 7, 8)
	check(map[string]any{"lat": 9.0, "lng": 10.0}, 9, 10)
	check("12.5,20.25", 12.5, 20.25)
	check("(30.5,40.5)", 30.5, 40.5)
	// lat out of range in first position forces lon,lat interpretation
	check("100,45", 45, 100)
	check([]byte(`{"lat":11,"lon":12}`), 11, 12)
	// arbitrary struct marshals through JSON
	check(struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}{13, 14}, 13, 14)

	if _, err := f.Validate("not-a-point"); err == nil {
		t.Error("garbage string should error")
	}
	if _, err := f.Validate("abc,def"); err == nil {
		t.Error("non-numeric pair should error")
	}
	if v, err := f.Validate(nil); err != nil || v != nil {
		t.Errorf("nil optional = %#v, %v", v, err)
	}
	f.SetRequired(true)
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}
}

func TestGeoPointMarshalUnmarshal(t *testing.T) {
	f := NewGeoPointField("loc")

	got, err := f.Marshal(map[string]any{"lat": 1.5, "lon": 2.5})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// PostgreSQL POINT format is (lon,lat)
	if got != "(2.500000,1.500000)" {
		t.Errorf("point marshal = %q", got)
	}
	if v, err := f.Marshal(nil); err != nil || v != nil {
		t.Errorf("nil marshal = %#v, %v", v, err)
	}

	jf := NewGeoPointField("jloc")
	jf.SetOptions(map[string]any{"use_json": true})
	if jf.PGType() != "JSONB" {
		t.Errorf("use_json PGType = %q", jf.PGType())
	}
	if !strings.Contains(jf.ColumnDef(), "JSONB") {
		t.Errorf("use_json ColumnDef = %q", jf.ColumnDef())
	}
	raw, err := jf.Marshal(map[string]any{"lat": 3.0, "lon": 4.0})
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var decoded GeoPoint
	if err := json.Unmarshal(raw.([]byte), &decoded); err != nil || decoded.Latitude != 3 {
		t.Errorf("json marshal = %s (%v)", raw, err)
	}

	// Unmarshal paths
	v, err := f.Unmarshal("(2.5,1.5)")
	if err != nil {
		t.Fatalf("unmarshal point: %v", err)
	}
	gp := v.(*GeoPoint)
	if gp.Latitude != 1.5 || gp.Longitude != 2.5 {
		t.Errorf("unmarshal point = %+v", gp)
	}
	v, _ = f.Unmarshal([]byte(`{"lat":7,"lon":8}`))
	if gp := v.(*GeoPoint); gp.Latitude != 7 {
		t.Errorf("unmarshal json bytes = %+v", gp)
	}
	if v, _ := f.Unmarshal(nil); v != nil {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if v, _ := f.Unmarshal("garbage"); v != nil {
		t.Errorf("unmarshal garbage = %#v", v)
	}
	v, _ = f.Unmarshal(map[string]any{"lat": 9.0, "lon": 10.0})
	if gp := v.(*GeoPoint); gp.Longitude != 10 {
		t.Errorf("unmarshal map = %+v", gp)
	}

	clone := jf.Clone().(*GeoPointField)
	if !clone.UseJSON {
		t.Error("clone lost UseJSON")
	}
	if f.PGType() != "POINT" || f.PGDefault() != "" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
}

// ---------------------------------------------------------------------------
// DateField
// ---------------------------------------------------------------------------

func TestDateFieldMinMaxAndMarshal(t *testing.T) {
	f := NewDateField("due")
	f.SetOptions(map[string]any{"min": "2020-01-01", "max": "2030-01-01"})
	if f.Min.IsZero() || f.Max.IsZero() {
		t.Fatal("min/max options not applied")
	}

	if _, err := f.Validate("2019-12-31"); err == nil {
		t.Error("date before min should error")
	}
	if _, err := f.Validate("2031-01-01"); err == nil {
		t.Error("date after max should error")
	}
	v, err := f.Validate("2025-06-11")
	if err != nil {
		t.Fatalf("valid date: %v", err)
	}
	if v.(time.Time).Year() != 2025 {
		t.Errorf("parsed year = %d", v.(time.Time).Year())
	}

	if got, err := f.Marshal("2025-06-11"); err != nil || got.(time.Time).Year() != 2025 {
		t.Errorf("marshal = %#v, %v", got, err)
	}

	now := time.Now()
	if v, _ := f.Unmarshal(now); !v.(time.Time).Equal(now) {
		t.Errorf("unmarshal time = %#v", v)
	}
	if v, _ := f.Unmarshal("2025-06-11"); v.(time.Time).Year() != 2025 {
		t.Errorf("unmarshal string = %#v", v)
	}
	if v, _ := f.Unmarshal(42); v != nil {
		t.Errorf("unmarshal other = %#v", v)
	}
	if v, _ := f.Unmarshal(nil); v != nil {
		t.Errorf("unmarshal nil = %#v", v)
	}

	clone := f.Clone().(*DateField)
	if !clone.Min.Equal(f.Min) || !clone.Max.Equal(f.Max) {
		t.Error("clone lost min/max")
	}
	if f.PGType() != "TIMESTAMPTZ" || f.PGDefault() != "" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
}

// ---------------------------------------------------------------------------
// URLField
// ---------------------------------------------------------------------------

func TestURLFieldSchemes(t *testing.T) {
	f := NewURLField("link")

	for _, bad := range []string{
		"ftp://files.example.com",
		"javascript:alert(1)",
		"https://", // no host
		"notaurl",
	} {
		if _, err := f.Validate(bad); err == nil {
			t.Errorf("Validate(%q) should reject", bad)
		}
	}
	if v, err := f.Validate("https://example.com/a?b=c"); err != nil || v != "https://example.com/a?b=c" {
		t.Errorf("valid https = %#v, %v", v, err)
	}
	if v, err := f.Validate([]byte("http://example.com")); err != nil || v != "http://example.com" {
		t.Errorf("[]byte url = %#v, %v", v, err)
	}
	if _, err := f.Validate(42); err == nil {
		t.Error("non-string should error")
	}
	if v, err := f.Validate("   "); err != nil || v != nil {
		t.Errorf("blank optional = %#v, %v", v, err)
	}
	f.SetRequired(true)
	if _, err := f.Validate("  "); err == nil {
		t.Error("blank required should error")
	}
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}

	if v, _ := f.Unmarshal(nil); v != "" {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if v, _ := f.Unmarshal([]byte("http://x")); v != "http://x" {
		t.Errorf("unmarshal bytes = %#v", v)
	}
	if v, _ := f.Unmarshal(9); v != "9" {
		t.Errorf("unmarshal other = %#v", v)
	}

	f.SetOptions(map[string]any{"except_domains": []any{"bad.com"}})
	if len(f.ExceptDomains) != 1 || f.ExceptDomains[0] != "bad.com" {
		t.Errorf("except_domains = %v", f.ExceptDomains)
	}
	clone := f.Clone().(*URLField)
	if len(clone.ExceptDomains) != 1 {
		t.Error("clone lost ExceptDomains")
	}
	if f.PGType() != "TEXT" {
		t.Errorf("PGType = %q", f.PGType())
	}
}

// ---------------------------------------------------------------------------
// EmailField
// ---------------------------------------------------------------------------

func TestEmailFieldDomains(t *testing.T) {
	f := NewEmailField("email")
	f.SetOptions(map[string]any{"only_domains": []any{"example.com"}})

	if v, err := f.Validate("USER@EXAMPLE.COM"); err != nil || v != "user@example.com" {
		t.Errorf("normalized email = %#v, %v", v, err)
	}
	if _, err := f.Validate("user@other.com"); err == nil {
		t.Error("domain outside only_domains should error")
	}

	g := NewEmailField("email")
	g.SetOptions(map[string]any{"except_domains": []any{"spam.com"}})
	if _, err := g.Validate("user@spam.com"); err == nil {
		t.Error("except_domains should reject")
	}
	if v, err := g.Validate([]byte("a@b.com")); err != nil || v != "a@b.com" {
		t.Errorf("[]byte email = %#v, %v", v, err)
	}
	if _, err := g.Validate("not-an-email"); err == nil {
		t.Error("invalid email should error")
	}
	if _, err := g.Validate(42); err == nil {
		t.Error("non-string should error")
	}
	if v, err := g.Validate(""); err != nil || v != nil {
		t.Errorf("blank optional = %#v, %v", v, err)
	}
	g.SetRequired(true)
	if _, err := g.Validate(""); err == nil {
		t.Error("blank required should error")
	}
	if _, err := g.Validate(nil); err == nil {
		t.Error("nil required should error")
	}

	if v, _ := g.Unmarshal(nil); v != "" {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if v, _ := g.Unmarshal([]byte("x@y.com")); v != "x@y.com" {
		t.Errorf("unmarshal bytes = %#v", v)
	}
	if v, _ := g.Unmarshal(3); v != "3" {
		t.Errorf("unmarshal other = %#v", v)
	}

	clone := f.Clone().(*EmailField)
	if len(clone.OnlyDomains) != 1 {
		t.Error("clone lost OnlyDomains")
	}
	if f.PGType() != "TEXT" || f.PGDefault() != "" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
}

// ---------------------------------------------------------------------------
// NumberField
// ---------------------------------------------------------------------------

func TestNumberFieldEdgeCases(t *testing.T) {
	f := NewNumberField("n")
	f.SetOptions(map[string]any{"min": float64(0), "max": float64(100), "only_int": true, "decimal_places": 2})
	if f.Min == nil || f.Max == nil || !f.OnlyInt || f.DecimalPlaces != 2 {
		t.Fatalf("options not applied: %+v", f)
	}

	if v, err := f.Validate("42"); err != nil || v != float64(42) {
		t.Errorf("string parse = %#v, %v", v, err)
	}
	if _, err := f.Validate("abc"); err == nil {
		t.Error("bad string should error")
	}
	if v, err := f.Validate(json.Number("7")); err != nil || v != float64(7) {
		t.Errorf("json.Number = %#v, %v", v, err)
	}
	if _, err := f.Validate(json.Number("xx")); err == nil {
		t.Error("bad json.Number should error")
	}
	if _, err := f.Validate(2.5); err == nil {
		t.Error("only_int should reject fractional values")
	}
	if _, err := f.Validate(float64(-1)); err == nil {
		t.Error("below min should error")
	}
	if _, err := f.Validate(float64(101)); err == nil {
		t.Error("above max should error")
	}
	if v, err := f.Validate(int32(5)); err != nil || v != float64(5) {
		t.Errorf("int32 = %#v, %v", v, err)
	}
	if _, err := f.Validate(true); err == nil {
		t.Error("bool should error")
	}

	g := NewNumberField("g")
	nan := []byte{} // sentinel, build NaN/Inf via math in collection tests instead
	_ = nan
	if _, err := g.Validate(infinity()); err == nil {
		t.Error("Inf should error")
	}
	if _, err := g.Validate(notANumber()); err == nil {
		t.Error("NaN should error")
	}

	if v, _ := g.Unmarshal("3.5"); v != 3.5 {
		t.Errorf("unmarshal string = %#v", v)
	}
	if v, _ := g.Unmarshal(int64(4)); v != float64(4) {
		t.Errorf("unmarshal int64 = %#v", v)
	}
	if v, _ := g.Unmarshal(struct{}{}); v != float64(0) {
		t.Errorf("unmarshal other = %#v", v)
	}
	if v, _ := g.Unmarshal(nil); v != float64(0) {
		t.Errorf("unmarshal nil = %#v", v)
	}

	clone := f.Clone().(*NumberField)
	if clone.Min == nil || clone.Max == nil {
		t.Fatal("clone lost min/max")
	}
	*f.Min = -999
	if *clone.Min == -999 {
		t.Error("clone shares Min pointer with original")
	}
	if f.PGType() != "DOUBLE PRECISION" {
		t.Errorf("PGType = %q", f.PGType())
	}
}

func infinity() float64   { return math.Inf(1) }
func notANumber() float64 { return math.NaN() }

// ---------------------------------------------------------------------------
// BoolField
// ---------------------------------------------------------------------------

func TestBoolFieldCoercions(t *testing.T) {
	f := NewBoolField("flag")

	truthy := []any{"true", "1", "yes", "on", float64(2), int64(1), 3}
	for _, in := range truthy {
		if v, err := f.Validate(in); err != nil || v != true {
			t.Errorf("Validate(%#v) = %#v, %v; want true", in, v, err)
		}
	}
	falsy := []any{"false", "0", "no", "off", "", float64(0), int64(0), 0}
	for _, in := range falsy {
		if v, err := f.Validate(in); err != nil || v != false {
			t.Errorf("Validate(%#v) = %#v, %v; want false", in, v, err)
		}
	}
	if _, err := f.Validate("maybe"); err == nil {
		t.Error("invalid bool string should error")
	}
	if _, err := f.Validate([]string{}); err == nil {
		t.Error("unsupported type should error")
	}
	if v, err := f.Validate(nil); err != nil || v != nil {
		t.Errorf("nil optional = %#v, %v", v, err)
	}
	f.SetRequired(true)
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}

	if v, _ := f.Unmarshal(nil); v != false {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if v, _ := f.Unmarshal("1"); v != true {
		t.Errorf("unmarshal '1' = %#v", v)
	}
	if v, _ := f.Unmarshal("nope"); v != false {
		t.Errorf("unmarshal 'nope' = %#v", v)
	}
	if v, _ := f.Unmarshal(float64(1)); v != true {
		t.Errorf("unmarshal float = %#v", v)
	}
	if v, _ := f.Unmarshal(int64(0)); v != false {
		t.Errorf("unmarshal int64 = %#v", v)
	}
	if v, _ := f.Unmarshal(struct{}{}); v != false {
		t.Errorf("unmarshal other = %#v", v)
	}

	if f.PGType() != "BOOLEAN" || f.PGDefault() != "" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
	if f.Clone().Name() != "flag" {
		t.Error("clone lost name")
	}
}

// ---------------------------------------------------------------------------
// AutoDateField
// ---------------------------------------------------------------------------

func TestAutoDateFieldBehavior(t *testing.T) {
	created := NewCreatedAtField()
	if !created.OnCreate || created.OnUpdate || !created.System() {
		t.Errorf("created_at flags wrong: %+v", created)
	}
	updated := NewUpdatedAtField()
	if !updated.OnCreate || !updated.OnUpdate || !updated.System() {
		t.Errorf("updated_at flags wrong: %+v", updated)
	}

	f := NewAutoDateField("created_at")
	if !f.OnCreate {
		t.Error("name-based OnCreate not set")
	}
	if f.PGType() != "TIMESTAMPTZ" || f.PGDefault() != "NOW()" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
	if !strings.Contains(f.ColumnDef(), "DEFAULT NOW()") {
		t.Errorf("ColumnDef = %q", f.ColumnDef())
	}

	// nil → now
	before := time.Now()
	v, err := f.Validate(nil)
	if err != nil {
		t.Fatalf("validate nil: %v", err)
	}
	if got := v.(time.Time); got.Before(before.Add(-time.Second)) {
		t.Errorf("nil should produce a recent timestamp, got %v", got)
	}
	// unparseable → now, never an error
	v, err = f.Validate([]int{1})
	if err != nil {
		t.Fatalf("unparseable input must not error: %v", err)
	}
	if _, ok := v.(time.Time); !ok {
		t.Errorf("fallback should be time.Time, got %T", v)
	}
	// valid date passes through
	v, _ = f.Validate("2024-01-02T03:04:05Z")
	if v.(time.Time).Year() != 2024 {
		t.Errorf("parsed = %v", v)
	}
	if m, err := f.Marshal("2024-01-02T03:04:05Z"); err != nil || m.(time.Time).Year() != 2024 {
		t.Errorf("marshal = %#v, %v", m, err)
	}

	now := time.Now()
	if v, _ := f.Unmarshal(now); !v.(time.Time).Equal(now) {
		t.Errorf("unmarshal time = %#v", v)
	}
	if v, _ := f.Unmarshal("2024-01-02"); v.(time.Time).Year() != 2024 {
		t.Errorf("unmarshal string = %#v", v)
	}
	if v, _ := f.Unmarshal(1); v != nil {
		t.Errorf("unmarshal other = %#v", v)
	}
	if v, _ := f.Unmarshal(nil); v != nil {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if f.Clone().Type() != TypeAutoDate {
		t.Error("clone lost type")
	}
}

// ---------------------------------------------------------------------------
// EditorField
// ---------------------------------------------------------------------------

func TestEditorFieldValidate(t *testing.T) {
	f := NewEditorField("body")
	f.SetOptions(map[string]any{"max_length": 5, "sanitize": true})
	if f.MaxLength != 5 || !f.Sanitize {
		t.Fatalf("options not applied: %+v", f)
	}

	if _, err := f.Validate("123456"); err == nil {
		t.Error("over max_length should error")
	}
	if v, err := f.Validate("<b>x"); err != nil || v != "<b>x" {
		t.Errorf("html = %#v, %v", v, err)
	}
	if v, err := f.Validate(nil); err != nil || v != "" {
		t.Errorf("nil optional = %#v, %v", v, err)
	}
	if v, err := f.Validate(123); err != nil || v != "123" {
		t.Errorf("stringified = %#v, %v", v, err)
	}
	f.SetRequired(true)
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}
	if _, err := f.Validate(""); err == nil {
		t.Error("empty required should error")
	}

	if v, _ := f.Unmarshal([]byte("hi")); v != "hi" {
		t.Errorf("unmarshal bytes = %#v", v)
	}
	if v, _ := f.Unmarshal(42); v != "42" {
		t.Errorf("unmarshal other = %#v", v)
	}
	if v, _ := f.Unmarshal(nil); v != "" {
		t.Errorf("unmarshal nil = %#v", v)
	}

	if f.PGType() != "TEXT" || f.PGDefault() != "''" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
	if !strings.Contains(f.ColumnDef(), "NOT NULL") {
		t.Errorf("required ColumnDef = %q", f.ColumnDef())
	}
	opt := NewEditorField("o")
	if !strings.Contains(opt.ColumnDef(), "DEFAULT ''") {
		t.Errorf("optional ColumnDef = %q", opt.ColumnDef())
	}
	if f.Clone().(*EditorField).MaxLength != 5 {
		t.Error("clone lost MaxLength")
	}
}

// ---------------------------------------------------------------------------
// PasswordField
// ---------------------------------------------------------------------------

func TestPasswordFieldHashing(t *testing.T) {
	f := NewPasswordField("password")
	f.SetOptions(map[string]any{"cost": 4, "min_length": 8, "max_length": 20, "pattern": "^.+$"})
	if f.Cost != 4 || f.MinLength != 8 || f.MaxLength != 20 || f.Pattern != "^.+$" {
		t.Fatalf("options not applied: %+v", f)
	}

	hashed, err := f.Marshal("hunter2hunter2")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	hash := hashed.(string)
	if hash == "hunter2hunter2" {
		t.Fatal("password stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("hunter2hunter2")); err != nil {
		t.Errorf("hash does not verify: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong-password")); err == nil {
		t.Error("hash verified a wrong password")
	}

	if _, err := f.Marshal("short"); err == nil {
		t.Error("below min_length should error")
	}
	if _, err := f.Marshal(strings.Repeat("x", 21)); err == nil {
		t.Error("above max_length should error")
	}
	if _, err := f.Validate(42); err == nil {
		t.Error("non-string should error")
	}
	if v, err := f.Marshal(""); err != nil || v != "" {
		t.Errorf("empty optional marshal = %#v, %v", v, err)
	}
	f.SetRequired(true)
	if _, err := f.Validate(""); err == nil {
		t.Error("empty required should error")
	}
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}

	// invalid cost outside [4,31] must be ignored
	g := NewPasswordField("p")
	g.SetOptions(map[string]any{"cost": 3})
	if g.Cost != 12 {
		t.Errorf("invalid cost applied: %d", g.Cost)
	}

	// Unmarshal returns the stored hash untouched
	if v, _ := f.Unmarshal(hash); v != hash {
		t.Errorf("unmarshal changed the hash")
	}
	if v, _ := f.Unmarshal([]byte("h")); v != "h" {
		t.Errorf("unmarshal bytes = %#v", v)
	}
	if v, _ := f.Unmarshal(nil); v != "" {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if v, _ := f.Unmarshal(5); v != "5" {
		t.Errorf("unmarshal other = %#v", v)
	}

	if f.PGType() != "TEXT" || f.PGDefault() != "" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
	if f.Clone().(*PasswordField).Cost != 4 {
		t.Error("clone lost cost")
	}
}

// ---------------------------------------------------------------------------
// RelationField
// ---------------------------------------------------------------------------

func TestRelationFieldMulti(t *testing.T) {
	f := NewRelationField("authors")
	f.SetOptions(map[string]any{"collection_id": "users", "cascade_delete": true, "max_select": 2})
	if f.CollectionID != "users" || !f.CascadeDelete || f.MaxSelect != 2 {
		t.Fatalf("options not applied: %+v", f)
	}
	if f.PGType() != "JSONB" {
		t.Errorf("multi PGType = %q", f.PGType())
	}

	// max_select counts raw values before dedupe, so stay within the limit
	v, err := f.Validate([]any{"a", "a"})
	if err != nil {
		t.Fatalf("multi validate: %v", err)
	}
	if arr := v.([]string); len(arr) != 1 || arr[0] != "a" {
		t.Errorf("dedupe failed: %#v", arr)
	}
	if _, err := f.Validate([]string{"a", "b", "c"}); err == nil {
		t.Error("over max_select should error")
	}
	if _, err := f.Validate(42); err == nil {
		t.Error("non-array multi should error")
	}
	if v, err := f.Validate(nil); err != nil || len(v.([]string)) != 0 {
		t.Errorf("nil optional multi = %#v, %v", v, err)
	}
	f.SetRequired(true)
	if _, err := f.Validate([]string{}); err == nil {
		t.Error("empty required should error")
	}
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}
	f.SetRequired(false)

	got, err := f.Marshal([]string{"x", "y"})
	if err != nil || string(got.([]byte)) != `["x","y"]` {
		t.Errorf("multi marshal = %s, %v", got, err)
	}

	if v, _ := f.Unmarshal([]byte(`["a"]`)); v.([]string)[0] != "a" {
		t.Errorf("multi unmarshal = %#v", v)
	}
	if v, _ := f.Unmarshal([]byte(`bad`)); len(v.([]string)) != 0 {
		t.Errorf("multi bad bytes = %#v", v)
	}
	if v, _ := f.Unmarshal("str"); len(v.([]string)) != 0 {
		t.Errorf("multi other = %#v", v)
	}
	if v, _ := f.Unmarshal(nil); v != nil {
		t.Errorf("unmarshal nil = %#v", v)
	}

	single := NewRelationField("owner")
	if single.PGType() != "TEXT" {
		t.Errorf("single PGType = %q", single.PGType())
	}
	if v, err := single.Validate("rec1"); err != nil || v != "rec1" {
		t.Errorf("single validate = %#v, %v", v, err)
	}
	if v, err := single.Validate(""); err != nil || v != nil {
		t.Errorf("single empty optional = %#v, %v", v, err)
	}
	single.SetRequired(true)
	if _, err := single.Validate(""); err == nil {
		t.Error("single empty required should error")
	}
	if v, _ := single.Unmarshal("id9"); v != "id9" {
		t.Errorf("single unmarshal = %#v", v)
	}

	if f.Clone().(*RelationField).MaxSelect != 2 {
		t.Error("clone lost MaxSelect")
	}
	if f.PGDefault() != "" {
		t.Errorf("PGDefault = %q", f.PGDefault())
	}
}

// ---------------------------------------------------------------------------
// JSONField
// ---------------------------------------------------------------------------

func TestJSONFieldSizeAndUnmarshal(t *testing.T) {
	f := NewJSONField("meta")
	f.SetOptions(map[string]any{"max_size": 10})
	if f.MaxSize != 10 {
		t.Fatalf("max_size not applied: %d", f.MaxSize)
	}

	if _, err := f.Validate(map[string]any{"key": "a-long-value"}); err == nil {
		t.Error("oversized JSON should error")
	}
	if v, err := f.Validate(`{"a":1}`); err != nil || string(v.([]byte)) != `{"a":1}` {
		t.Errorf("string json = %#v, %v", v, err)
	}
	if _, err := f.Validate(`{bad`); err == nil {
		t.Error("invalid json should error")
	}
	if v, err := f.Validate(nil); err != nil {
		t.Errorf("nil optional: %v", err)
	} else if _, ok := v.(map[string]any); !ok {
		t.Errorf("nil optional = %T", v)
	}
	f.SetRequired(true)
	if _, err := f.Validate(nil); err == nil {
		t.Error("nil required should error")
	}

	if v, _ := f.Unmarshal([]byte(`{"a":1}`)); string(v.([]byte)) != `{"a":1}` {
		t.Errorf("unmarshal bytes = %#v", v)
	}
	if v, _ := f.Unmarshal(`{"b":2}`); string(v.([]byte)) != `{"b":2}` {
		t.Errorf("unmarshal string = %#v", v)
	}
	if v, _ := f.Unmarshal(map[string]any{"c": 3}); string(v.([]byte)) != `{"c":3}` {
		t.Errorf("unmarshal map = %#v", v)
	}
	if v, _ := f.Unmarshal(make(chan int)); string(v.([]byte)) != "{}" {
		t.Errorf("unmarshal unmarshalable = %#v", v)
	}
	if v, _ := f.Unmarshal(nil); len(v.(map[string]any)) != 0 {
		t.Errorf("unmarshal nil = %#v", v)
	}

	if f.PGType() != "JSONB" || f.PGDefault() != "'{}'::jsonb" {
		t.Errorf("PGType/PGDefault = %q/%q", f.PGType(), f.PGDefault())
	}
	if !strings.Contains(f.ColumnDef(), "NOT NULL") {
		t.Errorf("required ColumnDef = %q", f.ColumnDef())
	}
	opt := NewJSONField("o")
	if !strings.Contains(opt.ColumnDef(), "DEFAULT '{}'::jsonb") {
		t.Errorf("optional ColumnDef = %q", opt.ColumnDef())
	}
	if f.Clone().(*JSONField).MaxSize != 10 {
		t.Error("clone lost MaxSize")
	}
}

// ---------------------------------------------------------------------------
// TextField extras
// ---------------------------------------------------------------------------

func TestTextFieldVarcharAndUnmarshal(t *testing.T) {
	f := NewTextField("name")
	f.SetOptions(map[string]any{"min_length": 2, "max_length": 5, "pattern": "^[a-z]+$"})
	if f.MinLength != 2 || f.MaxLength != 5 || f.Pattern != "^[a-z]+$" {
		t.Fatalf("options not applied: %+v", f)
	}
	if f.PGType() != "VARCHAR(5)" {
		t.Errorf("bounded PGType = %q", f.PGType())
	}
	if NewTextField("x").PGType() != "TEXT" {
		t.Error("unbounded PGType should be TEXT")
	}
	if NewTextField("x").PGDefault() != "" {
		t.Error("PGDefault should be empty")
	}

	// numeric coercion paths
	if v, err := f.Validate(float64(123)); err != nil || v != "123" {
		t.Errorf("float coerce = %#v, %v", v, err)
	}
	if v, err := f.Validate(int64(42)); err != nil || v != "42" {
		t.Errorf("int coerce = %#v, %v", v, err)
	}
	if v, err := f.Validate([]byte("ab")); err != nil || v != "ab" {
		t.Errorf("bytes coerce = %#v, %v", v, err)
	}

	if v, _ := f.Unmarshal(nil); v != "" {
		t.Errorf("unmarshal nil = %#v", v)
	}
	if v, _ := f.Unmarshal([]byte("b")); v != "b" {
		t.Errorf("unmarshal bytes = %#v", v)
	}
	if v, _ := f.Unmarshal(12); v != "12" {
		t.Errorf("unmarshal other = %#v", v)
	}
}

// ---------------------------------------------------------------------------
// Registry / FieldsList
// ---------------------------------------------------------------------------

func TestCreateFromSchemaAppliesEverything(t *testing.T) {
	r := NewRegistry()
	f, err := r.CreateFromSchema(SchemaField{
		ID:       "fid-1",
		Name:     "title",
		Type:     TypeText,
		System:   true,
		Required: true,
		Unique:   true,
		Options:  map[string]any{"min_length": 2, "max_length": 9},
	})
	if err != nil {
		t.Fatalf("CreateFromSchema: %v", err)
	}
	if f.ID() != "fid-1" || !f.System() || !f.Required() || !f.Unique() {
		t.Errorf("base properties not applied: %+v", f)
	}
	tf := f.(*TextField)
	if tf.MinLength != 2 || tf.MaxLength != 9 {
		t.Errorf("options not applied: %+v", tf)
	}

	if _, err := r.CreateFromSchema(SchemaField{Name: "x", Type: FieldType("bogus")}); err == nil {
		t.Error("unknown type should error")
	}
}

func TestFieldsListHelpers(t *testing.T) {
	list, err := NewFieldsList([]SchemaField{
		{ID: "a-id", Name: "title", Type: TypeText, Required: true},
		{ID: "b-id", Name: "count", Type: TypeNumber},
	})
	if err != nil {
		t.Fatalf("NewFieldsList: %v", err)
	}

	if list.Get("title") == nil || list.Get("missing") != nil {
		t.Error("Get by name failed")
	}
	if list.GetByID("b-id") == nil || list.GetByID("zzz") != nil {
		t.Error("GetByID failed")
	}
	names := list.Names()
	if len(names) != 2 || names[0] != "title" || names[1] != "count" {
		t.Errorf("Names = %v", names)
	}
	defs := list.ColumnDefs()
	if len(defs) != 2 || !strings.Contains(defs[0], `"title"`) || !strings.Contains(defs[0], "NOT NULL") {
		t.Errorf("ColumnDefs = %v", defs)
	}

	raw, err := list.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	var round []SchemaField
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("ToJSON output invalid: %v", err)
	}
	if len(round) != 2 || round[0].Name != "title" || round[0].Type != TypeText || !round[0].Required {
		t.Errorf("round trip = %+v", round)
	}

	clone := list.Clone()
	if len(clone) != 2 {
		t.Fatalf("clone length = %d", len(clone))
	}
	if clone[0] == list[0] {
		t.Error("clone should not share field pointers")
	}
	if clone[0].Name() != "title" {
		t.Error("clone lost field data")
	}

	if _, err := NewFieldsList([]SchemaField{{Name: "x", Type: FieldType("nope")}}); err == nil {
		t.Error("NewFieldsList with unknown type should error")
	}
}
