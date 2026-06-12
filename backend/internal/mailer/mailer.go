package mailer

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/config"
	"github.com/rs/zerolog/log"
)

// Message represents an email message.
type Message struct {
	From    mail.Address
	To      []mail.Address
	Cc      []mail.Address
	Bcc     []mail.Address
	Subject string
	HTML    string
	Text    string
	Headers map[string]string
}

// Sender is the interface for sending emails.
type Sender interface {
	Send(msg *Message) error
}

// Service handles email delivery via SMTP or sendmail.
type Service struct {
	cfg       *config.Config
	sender    Sender
	overrides OverrideProvider
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
	return s.sendTemplated("otp", to, map[string]string{"Code": code, "AppName": "Gresbase"})
}

func (s *Service) SendMagicLink(to string, token string) error {
	link := fmt.Sprintf("%s/auth/magic-link?token=%s", s.cfg.Domain, token)
	return s.sendTemplated("magic_link", to, map[string]string{"Link": link, "AppName": "Gresbase"})
}

func (s *Service) SendPasswordReset(to string, token string) error {
	link := fmt.Sprintf("%s/auth/reset-password?token=%s", s.cfg.Domain, token)
	return s.sendTemplated("password_reset", to, map[string]string{"Link": link, "AppName": "Gresbase"})
}

func (s *Service) SendVerification(to string, token string) error {
	link := fmt.Sprintf("%s/auth/verify?token=%s", s.cfg.Domain, token)
	return s.sendTemplated("verification", to, map[string]string{"Link": link, "AppName": "Gresbase"})
}

func (s *Service) SendEmailChange(to string, token string) error {
	link := fmt.Sprintf("%s/auth/confirm-email-change?token=%s", s.cfg.Domain, token)
	return s.sendTemplated("email_change", to, map[string]string{"Link": link, "AppName": "Gresbase"})
}

func (s *Service) SendAuthAlert(to string, info map[string]string) error {
	return s.sendTemplated("auth_alert", to, map[string]string{
		"IP":        info["ip"],
		"UserAgent": info["user_agent"],
		"Time":      info["time"],
		"AppName":   "Gresbase",
	})
}

func (s *Service) SendBackupNotification(to string, name string, size int64) error {
	return s.sendTemplated("backup", to, map[string]string{
		"Name":    name,
		"Size":    formatBytes(size),
		"AppName": "Gresbase",
	})
}

func (s *Service) SendBackupFailedAlert(to string, errMsg, when, instance string) error {
	return s.sendTemplated("backup_failed", to, map[string]string{
		"Error":    errMsg,
		"Time":     when,
		"Instance": instance,
		"AppName":  "Gresbase",
	})
}

// sendTemplated renders the subject and body for a template id (override or
// built-in default, see renderEmail) and dispatches the email.
func (s *Service) sendTemplated(id, to string, data any) error {
	subject, body, err := s.renderEmail(id, data)
	if err != nil {
		return err
	}
	return s.Send(&Message{
		To:      []mail.Address{{Address: to}},
		Subject: subject,
		HTML:    body,
	})
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
	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
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
	defer func() { _ = client.Quit() }() // best-effort SMTP session teardown

	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
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
