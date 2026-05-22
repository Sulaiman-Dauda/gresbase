package mailer

import (
	"net/mail"
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
	html, err := s.renderTemplate("otp", map[string]string{"Code": "123456", "AppName": "Gresbase"})
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
	_, err = s.renderTemplate("nonexistent", nil)
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
