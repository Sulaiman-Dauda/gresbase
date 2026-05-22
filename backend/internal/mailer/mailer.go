package mailer

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/config"
	"github.com/rs/zerolog/log"
)

// Message represents an email message.
type Message struct {
	From        mail.Address
	To          []mail.Address
	Cc          []mail.Address
	Bcc         []mail.Address
	Subject     string
	HTML        string
	Text        string
	Headers     map[string]string
}

// Sender is the interface for sending emails.
type Sender interface {
	Send(msg *Message) error
}

// Service handles email delivery via SMTP or sendmail.
type Service struct {
	cfg      *config.Config
	sender   Sender
	tmpl     *template.Template
}

// NewService creates a new mailer service.
func NewService(cfg *config.Config) *Service {
	s := &Service{cfg: cfg}

	if cfg.SMTPHost != "" {
		s.sender = &SMTPSender{
			host:     cfg.SMTPHost,
			port:     cfg.SMTPPort,
			username: cfg.SMTPUsername,
			password: cfg.SMTPPassword,
		}
	}

	// Parse built-in email templates
	s.tmpl = template.Must(template.New("mail").Parse(mailTemplates))

	return s
}

// Enabled returns whether the mailer is configured.
func (s *Service) Enabled() bool {
	return s.cfg.SMTPHost != ""
}

// BaseURL returns the application's public URL.
func (s *Service) BaseURL() string {
	domain := s.cfg.Domain
	if domain == "" {
		domain = "http://localhost:8080"
	}
	if !strings.HasPrefix(domain, "http") {
		if s.cfg.EnableTLS {
			domain = "https://" + domain
		} else {
			domain = "http://" + domain
		}
	}
	return domain
}

// Send dispatches an email.
func (s *Service) Send(msg *Message) error {
	if !s.Enabled() {
		log.Warn().Str("to", addressesToString(msg.To)).Msg("Mailer not configured, skipping email")
		return nil
	}

	if msg.From.Address == "" {
		msg.From = mail.Address{
			Name:    "Gresbase",
			Address: s.cfg.SMTPFrom,
		}
	}

	return s.sender.Send(msg)
}

func (s *Service) SendOTPEmail(to string, code string) error {
	return s.SendOTP(to, code)
}

func (s *Service) SendVerificationEmail(to string, link string) error {
	return s.SendVerification(to, strings.TrimPrefix(link, s.BaseURL()))
}

func (s *Service) SendPasswordResetEmail(to string, link string) error {
	token := strings.TrimPrefix(link, s.BaseURL()+"/_/reset-password?token=")
	return s.SendPasswordReset(to, token)
}

func (s *Service) SendMagicLinkEmail(to string, link string) error {
	token := strings.TrimPrefix(link, s.BaseURL()+"/_/magic-link?token=")
	return s.SendMagicLink(to, token)
}

func (s *Service) SendEmailChangeConfirmation(to string, link string) error {
	token := strings.TrimPrefix(link, s.BaseURL()+"/_/confirm-email-change?token=")
	return s.SendEmailChange(to, token)
}

// --- Convenience senders for auth flows ---

func (s *Service) SendOTP(to string, code string) error {
	body, _ := s.renderTemplate("otp", map[string]string{"Code": code, "AppName": "Gresbase"})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: "Your verification code",
		HTML:    body,
	})
}

func (s *Service) SendMagicLink(to string, token string) error {
	link := fmt.Sprintf("%s/auth/magic-link?token=%s", s.cfg.Domain, token)
	body, _ := s.renderTemplate("magic_link", map[string]string{"Link": link, "AppName": "Gresbase"})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: "Your magic login link",
		HTML:    body,
	})
}

func (s *Service) SendPasswordReset(to string, token string) error {
	link := fmt.Sprintf("%s/auth/reset-password?token=%s", s.cfg.Domain, token)
	body, _ := s.renderTemplate("password_reset", map[string]string{"Link": link, "AppName": "Gresbase"})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: "Reset your password",
		HTML:    body,
	})
}

func (s *Service) SendVerification(to string, token string) error {
	link := fmt.Sprintf("%s/auth/verify?token=%s", s.cfg.Domain, token)
	body, _ := s.renderTemplate("verification", map[string]string{"Link": link, "AppName": "Gresbase"})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: "Verify your email",
		HTML:    body,
	})
}

func (s *Service) SendEmailChange(to string, token string) error {
	link := fmt.Sprintf("%s/auth/confirm-email-change?token=%s", s.cfg.Domain, token)
	body, _ := s.renderTemplate("email_change", map[string]string{"Link": link, "AppName": "Gresbase"})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: "Confirm your new email address",
		HTML:    body,
	})
}

func (s *Service) SendAuthAlert(to string, info map[string]string) error {
	body, _ := s.renderTemplate("auth_alert", map[string]string{
		"IP":        info["ip"],
		"UserAgent": info["user_agent"],
		"Time":      info["time"],
		"AppName":   "Gresbase",
	})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: "New login to your account",
		HTML:    body,
	})
}

func (s *Service) SendBackupNotification(to string, name string, size int64) error {
	body, _ := s.renderTemplate("backup", map[string]any{
		"Name":    name,
		"Size":    formatBytes(size),
		"AppName": "Gresbase",
	})
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: fmt.Sprintf("Backup completed: %s", name),
		HTML:    body,
	})
}

func (s *Service) renderTemplate(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ---------------------------------------------------------------------------
// SMTP Sender
// ---------------------------------------------------------------------------

type SMTPSender struct {
	host     string
	port     int
	username string
	password string
}

func (s *SMTPSender) Send(msg *Message) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	from := msg.From.Address
	to := make([]string, len(msg.To))
	for i, a := range msg.To {
		to[i] = a.Address
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", msg.From.String()))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", addressesToString(msg.To)))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if msg.HTML != "" {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	} else {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	}
	buf.WriteString("\r\n")

	if msg.HTML != "" {
		buf.WriteString(msg.HTML)
	} else {
		buf.WriteString(msg.Text)
	}

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Quit()

	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: s.host}
			if err := client.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, t := range to {
		if err := client.Rcpt(t); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(buf.Bytes())
	if err != nil {
		return err
	}
	return w.Close()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func addressesToString(addrs []mail.Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ---------------------------------------------------------------------------
// Email Templates
// ---------------------------------------------------------------------------

const mailTemplates = `
{{define "otp"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">{{.AppName}}</h1>
    <p style="color: #888; margin: 0 0 24px;">Use this code to verify your identity</p>
    <div style="background: #1a1a1a; border-radius: 8px; padding: 20px; text-align: center; margin-bottom: 24px;">
      <span style="font-size: 32px; font-family: 'JetBrains Mono', monospace; letter-spacing: 8px; font-weight: 700;">{{.Code}}</span>
    </div>
    <p style="color: #666; font-size: 12px;">This code expires in 5 minutes.</p>
  </div>
</body>
</html>
{{end}}

{{define "password_reset"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Reset your password</h1>
    <p style="color: #888; margin: 0 0 24px;">Click the button below to reset your password for {{.AppName}}.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Reset Password</a>
    <p style="color: #666; font-size: 12px; margin-top: 24px;">This link expires in 1 hour. If you did not request this, you can safely ignore this email.</p>
  </div>
</body>
</html>
{{end}}

{{define "verification"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Verify your email</h1>
    <p style="color: #888; margin: 0 0 24px;">Click below to verify your email address for {{.AppName}}.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Verify Email</a>
  </div>
</body>
</html>
{{end}}

{{define "magic_link"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Sign in to {{.AppName}}</h1>
    <p style="color: #888; margin: 0 0 24px;">Click the button below to securely sign in.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Sign in</a>
    <p style="color: #666; font-size: 12px; margin-top: 24px;">This link expires in 15 minutes.</p>
  </div>
</body>
</html>
{{end}}

{{define "email_change"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Confirm email change</h1>
    <p style="color: #888; margin: 0 0 24px;">Click below to confirm your new email address.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Confirm Change</a>
  </div>
</body>
</html>
{{end}}

{{define "auth_alert"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">New sign-in</h1>
    <p style="color: #888; margin: 0 0 24px;">A new sign-in to your {{.AppName}} account was detected.</p>
    <div style="background: #1a1a1a; border-radius: 8px; padding: 16px; font-size: 13px; color: #aaa; line-height: 1.8;">
      <p style="margin: 0;">IP: {{.IP}}</p>
      <p style="margin: 0;">Browser: {{.UserAgent}}</p>
      <p style="margin: 0;">Time: {{.Time}}</p>
    </div>
    <p style="color: #666; font-size: 12px; margin-top: 24px;">If this was not you, please change your password immediately.</p>
  </div>
</body>
</html>
{{end}}

{{define "backup"}}
<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Backup completed</h1>
    <p style="color: #888; margin: 0 0 24px;">Backup "{{.Name}}" ({{.Size}}) has been created successfully.</p>
  </div>
</body>
</html>
{{end}}
`
