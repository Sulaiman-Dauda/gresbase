package fields

import (
	"testing"
	"time"
)

func TestTextField_Validate(t *testing.T) {
	f := NewTextField("title")
	f.SetRequired(true)
	f.MinLength = 3
	f.MaxLength = 100

	tests := []struct {
		name    string
		input   any
		want    any
		wantErr bool
	}{
		{"valid string", "Hello World", "Hello World", false},
		{"too short", "ab", nil, true},
		{"nil required", nil, nil, true},
		{"empty required", "", nil, true},
		{"trimmed empty", "  ", nil, true},
		{"max length ok", "short", "short", false},
		{"valid from bytes", []byte("test"), "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Validate(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNumberField_Validate(t *testing.T) {
	f := NewNumberField("age")
	f.SetRequired(true)
	min := float64(0)
	max := float64(150)
	f.Min = &min
	f.Max = &max
	f.OnlyInt = true

	valid, err := f.Validate(float64(25))
	if err != nil || valid.(float64) != 25 {
		t.Errorf("valid number failed: err=%v val=%v", err, valid)
	}

	_, err = f.Validate(float64(-1))
	if err == nil {
		t.Error("below min should error")
	}

	_, err = f.Validate(float64(200))
	if err == nil {
		t.Error("above max should error")
	}

	_, err = f.Validate(float64(25.5))
	if err == nil {
		t.Error("non-integer should error with OnlyInt")
	}
}

func TestBoolField_Validate(t *testing.T) {
	f := NewBoolField("active")

	valid, err := f.Validate(true)
	if err != nil || valid != true {
		t.Errorf("true failed: %v", err)
	}

	valid, err = f.Validate("true")
	if err != nil || valid != true {
		t.Errorf("string true failed: %v", err)
	}

	valid, err = f.Validate("false")
	if err != nil || valid != false {
		t.Errorf("string false failed: %v", err)
	}

	valid, err = f.Validate("1")
	if err != nil || valid != true {
		t.Errorf("'1' should be true: %v", err)
	}
}

func TestEmailField_Validate(t *testing.T) {
	f := NewEmailField("email")
	f.SetRequired(true)

	valid, _ := f.Validate("user@example.com")
	if valid != "user@example.com" {
		t.Errorf("valid email failed: %v", valid)
	}

	_, err := f.Validate("not-an-email")
	if err == nil {
		t.Error("invalid email should error")
	}

	_, err = f.Validate(nil)
	if err == nil {
		t.Error("required nil should error")
	}
}

func TestURLField_Validate(t *testing.T) {
	f := NewURLField("website")

	valid, _ := f.Validate("https://example.com")
	if valid != "https://example.com" {
		t.Errorf("valid URL failed: %v", valid)
	}

	_, err := f.Validate("not-a-url")
	if err == nil {
		t.Error("invalid URL should error")
	}
}

func TestDateField_Validate(t *testing.T) {
	f := NewDateField("created_at")

	now := time.Now()
	valid, err := f.Validate(now)
	if err != nil || !valid.(time.Time).Equal(now) {
		t.Errorf("valid time failed: err=%v", err)
	}

	valid, err = f.Validate("2024-01-15T10:30:00Z")
	if err != nil || valid.(time.Time).Year() != 2024 {
		t.Errorf("valid string time failed: err=%v val=%v", err, valid)
	}
}

func TestSelectField_Validate(t *testing.T) {
	f := NewSelectField("status")
	f.Values = []string{"active", "draft", "archived"}

	valid, err := f.Validate("active")
	if err != nil || valid != "active" {
		t.Errorf("valid select failed: %v", err)
	}

	_, err = f.Validate("deleted")
	if err == nil {
		t.Error("invalid select value should error")
	}

	// Multiple
	f2 := NewSelectField("tags")
	f2.Values = []string{"go", "rust", "js"}
	f2.Multiple = true
	f2.MaxSelect = 2

	valid, err = f2.Validate([]string{"go", "rust"})
	if err != nil {
		t.Errorf("valid multiple select failed: %v", err)
	}

	_, err = f2.Validate([]string{"go", "rust", "js"})
	if err == nil {
		t.Error("too many selections should error")
	}
}

func TestJSONField_Validate(t *testing.T) {
	f := NewJSONField("config")

	valid, err := f.Validate(map[string]any{"theme": "dark"})
	if err != nil {
		t.Errorf("valid JSON failed: %v", err)
	}
	if valid == nil {
		t.Error("nil result from valid JSON")
	}

	_, err = f.Validate(nil)
	if err != nil {
		t.Errorf("nil optional should return empty: %v", err)
	}
}

func TestFileField_Validate(t *testing.T) {
	f := NewFileField("avatar")
	f.MaxSelect = 1

	valid, err := f.Validate("file_abc123.jpg")
	if err != nil || valid != "file_abc123.jpg" {
		t.Errorf("valid file failed: %v", err)
	}

	// Multiple files
	f2 := NewFileField("attachments")
	f2.MaxSelect = 3

	valid, err = f2.Validate([]string{"f1.pdf", "f2.pdf"})
	if err != nil {
		t.Errorf("valid multiple files failed: %v", err)
	}

	_, err = f2.Validate([]string{"f1.pdf", "f2.pdf", "f3.pdf", "f4.pdf"})
	if err == nil {
		t.Error("too many files should error")
	}
}

func TestPasswordField_Marshal(t *testing.T) {
	f := NewPasswordField("password")
	f.MinLength = 8
	f.Cost = 4 // lower cost for faster tests

	valid, err := f.Validate("mysecret123")
	if err != nil {
		t.Errorf("valid password failed: %v", err)
	}

	hash, err := f.Marshal(valid)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if hash == nil || hash.(string) == "" {
		t.Fatal("hash should not be empty")
	}

	// Verify it's a valid bcrypt hash
	if len(hash.(string)) < 50 {
		t.Errorf("unexpected hash length: %d", len(hash.(string)))
	}

	t.Logf("Hash: %s", hash.(string))
}

func TestGeoPointField_Validate(t *testing.T) {
	f := NewGeoPointField("location")

	gp, err := f.Validate(GeoPoint{Latitude: 48.8566, Longitude: 2.3522})
	if err != nil {
		t.Errorf("valid geopoint failed: %v", err)
	}
	if gp.(*GeoPoint).Latitude != 48.8566 {
		t.Errorf("latitude mismatch")
	}

	_, err = f.Validate(GeoPoint{Latitude: 200, Longitude: 0})
	if err == nil {
		t.Error("invalid latitude should error")
	}
}

func TestAutoDateField(t *testing.T) {
	f := NewCreatedAtField()
	if !f.System() {
		t.Error("auto date should be system field")
	}
	if f.PGDefault() != "NOW()" {
		t.Errorf("default should be NOW(), got %s", f.PGDefault())
	}

	valid, err := f.Validate(nil)
	if err != nil {
		t.Errorf("nil should auto-generate time: %v", err)
	}
	if valid == nil {
		t.Error("nil should not return nil value")
	}
}

func TestRegistry_CreateAllTypes(t *testing.T) {
	r := NewRegistry()

	types := []FieldType{
		TypeText, TypeNumber, TypeBool, TypeEmail, TypeURL,
		TypeDate, TypeSelect, TypeJSON, TypeFile, TypeRelation,
		TypePassword, TypeEditor, TypeGeoPoint, TypeAutoDate,
	}

	for _, ft := range types {
		f, err := r.Create(ft, "test_"+string(ft))
		if err != nil {
			t.Errorf("create %s: %v", ft, err)
			continue
		}
		if f.Name() != "test_"+string(ft) {
			t.Errorf("unexpected name for %s: %s", ft, f.Name())
		}
		// Check column def is non-empty
		if f.ColumnDef() == "" {
			t.Errorf("column def empty for %s", ft)
		}
	}
}

func TestFieldsList_CreateTableSQL(t *testing.T) {
	list := FieldsList{
		NewTextField("title"),
		NewNumberField("views"),
		NewBoolField("published"),
	}

	sql := list.CreateTableSQL("posts")
	if sql == "" {
		t.Error("empty SQL")
	}
	t.Logf("CreateTableSQL:\n%s", sql)
}

func TestFieldsList_ValidateRecord(t *testing.T) {
	f1 := NewTextField("title")
	f1.SetRequired(true)
	f2 := NewNumberField("views")

	list := FieldsList{f1, f2}

	record := map[string]any{
		"title": "Hello",
		"views": float64(100),
	}

	cleaned, err := list.ValidateRecord(record)
	if err != nil {
		t.Fatalf("valid record failed: %v", err)
	}
	if cleaned["title"] != "Hello" {
		t.Errorf("title mismatch")
	}
	if cleaned["views"].(float64) != 100 {
		t.Errorf("views mismatch")
	}

	// Missing required
	_, err = list.ValidateRecord(map[string]any{"views": 1})
	if err == nil {
		t.Error("missing required field should error")
	}
}

func TestFieldsList_MarshalRecord(t *testing.T) {
	f := NewPasswordField("password")
	f.Cost = 4

	list := FieldsList{f}

	marshaled, err := list.MarshalRecord(map[string]any{"password": "secret123"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if marshaled["password"].(string) == "secret123" {
		t.Error("password should be hashed, not plaintext")
	}
}

func TestClone(t *testing.T) {
	f := NewTextField("title")
	f.SetRequired(true)
	f.MinLength = 5

	clone := f.Clone()
	cf := clone.(*TextField)

	if cf.Name() != "title" || cf.MinLength != 5 || !cf.Required() {
		t.Error("clone doesn't match original")
	}

	// Mutating clone shouldn't affect original
	cf.MinLength = 10
	if f.MinLength != 5 {
		t.Error("clone mutation affected original")
	}
}
