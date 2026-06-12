package settings

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNewService(t *testing.T) {
	s := NewService(nil)
	if s == nil {
		t.Fatal("expected non-nil settings service")
	}
}

func TestEmailTemplatesJSONRoundTrip(t *testing.T) {
	original := DefaultSettings()
	original.EmailTemplates = map[string]EmailTemplate{
		"otp":          {Subject: "Your code: {{.Code}}", Body: "<p>{{.Code}}</p>"},
		"verification": {Body: "<p>Verify: {{.Link}}</p>"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Settings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(original.EmailTemplates, decoded.EmailTemplates) {
		t.Errorf("email_templates did not round-trip:\nwant %#v\ngot  %#v", original.EmailTemplates, decoded.EmailTemplates)
	}

	// The section must be present under the expected JSON key.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["email_templates"]; !ok {
		t.Error("expected email_templates key in settings JSON")
	}
}

func TestSettingsCloneIsolatesEmailTemplates(t *testing.T) {
	original := DefaultSettings()
	original.EmailTemplates = map[string]EmailTemplate{
		"otp": {Subject: "original"},
	}

	clone := original.clone()
	clone.EmailTemplates["otp"] = EmailTemplate{Subject: "mutated"}
	clone.EmailTemplates["magic_link"] = EmailTemplate{Body: "new"}

	if original.EmailTemplates["otp"].Subject != "original" {
		t.Error("mutating the clone changed the original settings")
	}
	if _, ok := original.EmailTemplates["magic_link"]; ok {
		t.Error("adding to the clone changed the original settings")
	}
}
