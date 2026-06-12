package mailer

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/config"
)

func TestNewService(t *testing.T) {
	cfg := &config.Config{
		SMTPHost: "",
	}
	s := NewService(cfg)
	if s == nil {
		t.Fatal("expected non-nil service")
	}
	if s.Enabled() {
		t.Error("service without SMTP host should not be enabled")
	}
}

func TestServiceEnabled(t *testing.T) {
	cfg := &config.Config{
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
	}
	s := NewService(cfg)
	if !s.Enabled() {
		t.Error("service with SMTP host should be enabled")
	}
}

func TestMessageStruct(t *testing.T) {
	msg := &Message{
		From:    mail.Address{Name: "Gresbase", Address: "noreply@gresbase.io"},
		To:      []mail.Address{{Address: "user@example.com"}},
		Subject: "Test Email",
		HTML:    "<h1>Hello</h1>",
		Text:    "Hello",
	}

	if msg.Subject != "Test Email" {
		t.Error("subject mismatch")
	}
	if len(msg.To) != 1 {
		t.Errorf("expected 1 recipient, got %d", len(msg.To))
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, result, tt.expected)
		}
	}
}

func TestAddressesToString(t *testing.T) {
	addrs := []mail.Address{
		{Name: "Alice", Address: "alice@example.com"},
		{Address: "bob@example.com"},
	}

	result := addressesToString(addrs)
	if result == "" {
		t.Error("address string should not be empty")
	}
}

func TestServiceBaseURL(t *testing.T) {
	cfg := &config.Config{
		Domain:    "example.com",
		EnableTLS: true,
	}
	s := NewService(cfg)
	url := s.BaseURL()
	if url != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %q", url)
	}

	cfg2 := &config.Config{
		Domain:    "",
		EnableTLS: false,
	}
	s2 := NewService(cfg2)
	url2 := s2.BaseURL()
	if url2 != "http://localhost:8080" {
		t.Errorf("expected default URL, got %q", url2)
	}
}

func TestServiceSendOTP(t *testing.T) {
	cfg := &config.Config{SMTPHost: ""}
	s := NewService(cfg)

	// Should not error even without SMTP (just logs a warning)
	err := s.SendOTP("user@example.com", "123456")
	if err != nil {
		t.Errorf("expected no error without SMTP, got %v", err)
	}
}

func TestServiceSendPasswordReset(t *testing.T) {
	cfg := &config.Config{SMTPHost: ""}
	s := NewService(cfg)

	err := s.SendPasswordReset("user@example.com", "test-token-123")
	if err != nil {
		t.Errorf("expected no error without SMTP, got %v", err)
	}
}

func TestServiceRenderTemplate(t *testing.T) {
	cfg := &config.Config{}
	s := NewService(cfg)

	// Render OTP template
	_, html, err := s.renderEmail("otp", map[string]string{"Code": "123456", "AppName": "Gresbase"})
	if err != nil {
		t.Fatal(err)
	}
	if html == "" {
		t.Error("rendered HTML should not be empty")
	}
	if !stringContains(html, "123456") {
		t.Error("rendered template should contain the OTP code")
	}

	// Render non-existent template
	_, _, err = s.renderEmail("nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Email template override tests
// ---------------------------------------------------------------------------

func TestRenderEmailDefaults(t *testing.T) {
	s := NewService(&config.Config{})

	subject, body, err := s.renderEmail("otp", map[string]string{"Code": "123456", "AppName": "Gresbase"})
	if err != nil {
		t.Fatalf("renderEmail: %v", err)
	}
	if subject != "Your verification code" {
		t.Errorf("unexpected default subject: %q", subject)
	}
	if !strings.Contains(body, "123456") {
		t.Errorf("default body should contain the code, got: %q", body)
	}

	// Templated default subject (backup uses {{.Name}}).
	subject, _, err = s.renderEmail("backup", map[string]string{"Name": "daily.tar.gz", "Size": "1.0 MB", "AppName": "Gresbase"})
	if err != nil {
		t.Fatalf("renderEmail backup: %v", err)
	}
	if subject != "Backup completed: daily.tar.gz" {
		t.Errorf("unexpected backup subject: %q", subject)
	}
}

func TestRenderEmailWithOverride(t *testing.T) {
	s := NewService(&config.Config{})
	s.SetOverrideProvider(func() map[string]TemplateOverride {
		return map[string]TemplateOverride{
			"otp": {
				Subject: "{{.AppName}} code: {{.Code}}",
				Body:    "<p>Use code {{.Code}} now</p>",
			},
		}
	})

	subject, body, err := s.renderEmail("otp", map[string]string{"Code": "654321", "AppName": "Acme"})
	if err != nil {
		t.Fatalf("renderEmail: %v", err)
	}
	if subject != "Acme code: 654321" {
		t.Errorf("override subject not rendered, got: %q", subject)
	}
	if body != "<p>Use code 654321 now</p>" {
		t.Errorf("override body not rendered, got: %q", body)
	}

	// Other templates are untouched by the override.
	subject, _, err = s.renderEmail("verification", map[string]string{"Link": "https://x", "AppName": "Acme"})
	if err != nil {
		t.Fatalf("renderEmail verification: %v", err)
	}
	if subject != "Verify your email" {
		t.Errorf("verification should use default subject, got: %q", subject)
	}
}

func TestRenderEmailPartialOverride(t *testing.T) {
	s := NewService(&config.Config{})
	s.SetOverrideProvider(func() map[string]TemplateOverride {
		return map[string]TemplateOverride{
			"otp": {Subject: "Custom subject"}, // body empty -> default body
		}
	})

	subject, body, err := s.renderEmail("otp", map[string]string{"Code": "111222", "AppName": "Gresbase"})
	if err != nil {
		t.Fatalf("renderEmail: %v", err)
	}
	if subject != "Custom subject" {
		t.Errorf("expected overridden subject, got: %q", subject)
	}
	if !strings.Contains(body, "111222") || !strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("expected default body, got: %q", body)
	}
}

func TestRenderEmailInvalidOverrideFallsBackToDefault(t *testing.T) {
	s := NewService(&config.Config{})
	s.SetOverrideProvider(func() map[string]TemplateOverride {
		return map[string]TemplateOverride{
			"otp": {
				Subject: "{{.Code",        // parse error
				Body:    "<p>{{.Code</p>", // parse error
			},
		}
	})

	subject, body, err := s.renderEmail("otp", map[string]string{"Code": "999000", "AppName": "Gresbase"})
	if err != nil {
		t.Fatalf("invalid override must not break sending, got error: %v", err)
	}
	if subject != "Your verification code" {
		t.Errorf("expected fallback to default subject, got: %q", subject)
	}
	if !strings.Contains(body, "999000") {
		t.Errorf("expected fallback default body containing the code, got: %q", body)
	}
}

func TestRenderEmailUnknownTemplate(t *testing.T) {
	s := NewService(&config.Config{})
	if _, _, err := s.renderEmail("nope", nil); err == nil {
		t.Fatal("expected error for unknown template id")
	}
}

func TestValidateOverride(t *testing.T) {
	if err := ValidateOverride("otp", "Code {{.Code}}", "<p>{{.Code}}</p>"); err != nil {
		t.Errorf("valid override rejected: %v", err)
	}
	if err := ValidateOverride("otp", "", ""); err != nil {
		t.Errorf("empty override should be valid (means default): %v", err)
	}
	if err := ValidateOverride("otp", "{{.Code", ""); err == nil {
		t.Error("expected error for broken subject template")
	}
	if err := ValidateOverride("otp", "", "<p>{{.Code</p>"); err == nil {
		t.Error("expected error for broken body template")
	}
	if err := ValidateOverride("does_not_exist", "x", "y"); err == nil {
		t.Error("expected error for unknown template id")
	}
}

func TestTemplatesRegistry(t *testing.T) {
	defs := Templates()
	if len(defs) == 0 {
		t.Fatal("expected built-in templates")
	}
	want := []string{"verification", "otp", "magic_link", "password_reset", "email_change", "auth_alert", "backup", "backup_failed"}
	if len(defs) != len(want) {
		t.Fatalf("expected %d templates, got %d", len(want), len(defs))
	}
	for i, id := range want {
		if defs[i].ID != id {
			t.Errorf("expected template %q at position %d, got %q", id, i, defs[i].ID)
		}
		def, ok := TemplateByID(id)
		if !ok {
			t.Errorf("TemplateByID(%q) not found", id)
			continue
		}
		if def.DefaultSubject == "" || def.DefaultBody == "" || len(def.Placeholders) == 0 {
			t.Errorf("template %q is missing defaults or placeholders", id)
		}
		// Every default must itself pass override validation.
		if err := ValidateOverride(id, def.DefaultSubject, def.DefaultBody); err != nil {
			t.Errorf("default for %q does not validate: %v", id, err)
		}
	}
}
