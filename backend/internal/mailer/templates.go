package mailer

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"

	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// Email template registry
//
// Every transactional email has a stable template id. The built-in subject and
// body below are the canonical DEFAULTS. Superusers may override subject and/or
// body per template via settings (Settings.EmailTemplates); an empty or missing
// override falls back to the default. Bodies are html/template, subjects are
// text/template — the same data structs are passed either way, so the
// placeholders listed in each TemplateDef work in both defaults and overrides.
// ---------------------------------------------------------------------------

// TemplateOverride is a user-supplied subject/body override for one template.
// Empty fields mean "use the built-in default".
type TemplateOverride struct {
	Subject string
	Body    string
}

// OverrideProvider returns the current template overrides keyed by template id.
// It is called on every render so settings changes take effect immediately.
type OverrideProvider func() map[string]TemplateOverride

// TemplateDef describes a built-in email template.
type TemplateDef struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Placeholders   []string `json:"placeholders"`
	DefaultSubject string   `json:"defaultSubject"`
	DefaultBody    string   `json:"defaultBody"`
}

// templateDefs is the ordered registry of all built-in email templates.
var templateDefs = []TemplateDef{
	{
		ID:             "verification",
		Name:           "Email verification",
		Description:    "Sent when a user must verify their email address.",
		Placeholders:   []string{"{{.Link}}", "{{.AppName}}"},
		DefaultSubject: "Verify your email",
		DefaultBody:    defaultVerificationBody,
	},
	{
		ID:             "otp",
		Name:           "One-time password (OTP)",
		Description:    "Sent with a short-lived login or verification code.",
		Placeholders:   []string{"{{.Code}}", "{{.AppName}}"},
		DefaultSubject: "Your verification code",
		DefaultBody:    defaultOTPBody,
	},
	{
		ID:             "magic_link",
		Name:           "Magic link",
		Description:    "Sent with a one-click sign-in link.",
		Placeholders:   []string{"{{.Link}}", "{{.AppName}}"},
		DefaultSubject: "Your magic login link",
		DefaultBody:    defaultMagicLinkBody,
	},
	{
		ID:             "password_reset",
		Name:           "Password reset",
		Description:    "Sent when a user requests a password reset.",
		Placeholders:   []string{"{{.Link}}", "{{.AppName}}"},
		DefaultSubject: "Reset your password",
		DefaultBody:    defaultPasswordResetBody,
	},
	{
		ID:             "email_change",
		Name:           "Email change confirmation",
		Description:    "Sent to confirm a new email address.",
		Placeholders:   []string{"{{.Link}}", "{{.AppName}}"},
		DefaultSubject: "Confirm your new email address",
		DefaultBody:    defaultEmailChangeBody,
	},
	{
		ID:             "auth_alert",
		Name:           "New sign-in alert",
		Description:    "Sent when a new login to an account is detected.",
		Placeholders:   []string{"{{.IP}}", "{{.UserAgent}}", "{{.Time}}", "{{.AppName}}"},
		DefaultSubject: "New login to your account",
		DefaultBody:    defaultAuthAlertBody,
	},
	{
		ID:             "backup",
		Name:           "Backup notification",
		Description:    "Sent when a backup completes.",
		Placeholders:   []string{"{{.Name}}", "{{.Size}}", "{{.AppName}}"},
		DefaultSubject: "Backup completed: {{.Name}}",
		DefaultBody:    defaultBackupBody,
	},
	{
		ID:             "backup_failed",
		Name:           "Backup failure alert",
		Description:    "Sent to all superusers when a scheduled backup fails.",
		Placeholders:   []string{"{{.Error}}", "{{.Time}}", "{{.Instance}}", "{{.AppName}}"},
		DefaultSubject: "Scheduled backup failed",
		DefaultBody:    defaultBackupFailedBody,
	},
}

var templateDefsByID = func() map[string]TemplateDef {
	m := make(map[string]TemplateDef, len(templateDefs))
	for _, def := range templateDefs {
		m[def.ID] = def
	}
	return m
}()

// Precompiled defaults — these are constants, so a parse failure is a
// programming error and panics at startup.
var (
	defaultSubjectTmpls = func() map[string]*texttemplate.Template {
		m := make(map[string]*texttemplate.Template, len(templateDefs))
		for _, def := range templateDefs {
			m[def.ID] = texttemplate.Must(texttemplate.New(def.ID + "_subject").Parse(def.DefaultSubject))
		}
		return m
	}()
	defaultBodyTmpls = func() map[string]*htmltemplate.Template {
		m := make(map[string]*htmltemplate.Template, len(templateDefs))
		for _, def := range templateDefs {
			m[def.ID] = htmltemplate.Must(htmltemplate.New(def.ID).Parse(def.DefaultBody))
		}
		return m
	}()
)

// Templates returns the ordered list of built-in email template definitions.
func Templates() []TemplateDef {
	out := make([]TemplateDef, len(templateDefs))
	copy(out, templateDefs)
	return out
}

// TemplateByID returns the definition for a template id.
func TemplateByID(id string) (TemplateDef, bool) {
	def, ok := templateDefsByID[id]
	return def, ok
}

// ValidateOverride checks a user-supplied override for the given template id.
// It verifies the id is known and that subject (text/template) and body
// (html/template) parse and execute against sample data, so a broken override
// is rejected at settings-save time instead of breaking email delivery.
func ValidateOverride(id, subject, body string) error {
	def, ok := templateDefsByID[id]
	if !ok {
		return fmt.Errorf("unknown email template id %q", id)
	}
	sample := sampleData(def)
	if subject != "" {
		tmpl, err := texttemplate.New(id + "_subject").Parse(subject)
		if err != nil {
			return fmt.Errorf("subject: %w", err)
		}
		if err := tmpl.Execute(&bytes.Buffer{}, sample); err != nil {
			return fmt.Errorf("subject: %w", err)
		}
	}
	if body != "" {
		tmpl, err := htmltemplate.New(id).Parse(body)
		if err != nil {
			return fmt.Errorf("body: %w", err)
		}
		if err := tmpl.Execute(&bytes.Buffer{}, sample); err != nil {
			return fmt.Errorf("body: %w", err)
		}
	}
	return nil
}

// sampleData builds placeholder sample values for trial execution.
func sampleData(def TemplateDef) map[string]string {
	data := map[string]string{}
	for _, p := range def.Placeholders {
		// "{{.Link}}" -> "Link"
		name := p
		name = trimPrefixSuffix(name, "{{.", "}}")
		data[name] = "sample"
	}
	return data
}

func trimPrefixSuffix(s, prefix, suffix string) string {
	if len(s) > len(prefix)+len(suffix) && s[:len(prefix)] == prefix && s[len(s)-len(suffix):] == suffix {
		return s[len(prefix) : len(s)-len(suffix)]
	}
	return s
}

// SetOverrideProvider wires a source of template overrides (e.g. settings).
func (s *Service) SetOverrideProvider(p OverrideProvider) {
	s.overrides = p
}

// renderEmail renders the subject and body for a template id: the override is
// used when present, the built-in default otherwise. If an override fails to
// parse or execute at send time (it should have been rejected at save time),
// a warning is logged and the default is rendered instead — a bad override
// never breaks email delivery.
func (s *Service) renderEmail(id string, data any) (subject, body string, err error) {
	if _, ok := templateDefsByID[id]; !ok {
		return "", "", fmt.Errorf("unknown email template %q", id)
	}

	var ov TemplateOverride
	if s.overrides != nil {
		ov = s.overrides()[id]
	}

	subject, sErr := renderSubject(id, ov.Subject, data)
	if sErr != nil {
		return "", "", fmt.Errorf("render subject %q: %w", id, sErr)
	}
	body, bErr := renderBody(id, ov.Body, data)
	if bErr != nil {
		return "", "", fmt.Errorf("render body %q: %w", id, bErr)
	}
	return subject, body, nil
}

func renderSubject(id, override string, data any) (string, error) {
	if override != "" {
		tmpl, err := texttemplate.New(id + "_subject").Parse(override)
		if err == nil {
			var buf bytes.Buffer
			if execErr := tmpl.Execute(&buf, data); execErr == nil {
				return buf.String(), nil
			} else {
				err = execErr
			}
		}
		log.Warn().Err(err).Str("template", id).Msg("Email subject override failed to render, falling back to default")
	}
	var buf bytes.Buffer
	if err := defaultSubjectTmpls[id].Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderBody(id, override string, data any) (string, error) {
	if override != "" {
		tmpl, err := htmltemplate.New(id).Parse(override)
		if err == nil {
			var buf bytes.Buffer
			if execErr := tmpl.Execute(&buf, data); execErr == nil {
				return buf.String(), nil
			} else {
				err = execErr
			}
		}
		log.Warn().Err(err).Str("template", id).Msg("Email body override failed to render, falling back to default")
	}
	var buf bytes.Buffer
	if err := defaultBodyTmpls[id].Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ---------------------------------------------------------------------------
// Default bodies (canonical defaults — keep in sync with nothing else; these
// ARE the source of truth)
// ---------------------------------------------------------------------------

const defaultOTPBody = `<!DOCTYPE html>
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
</html>`

//nolint:gosec // G101 false positive: HTML email template for password-reset notifications, not a credential.
const defaultPasswordResetBody = `<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Reset your password</h1>
    <p style="color: #888; margin: 0 0 24px;">Click the button below to reset your password for {{.AppName}}.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Reset Password</a>
    <p style="color: #666; font-size: 12px; margin-top: 24px;">This link expires in 1 hour. If you did not request this, you can safely ignore this email.</p>
  </div>
</body>
</html>`

const defaultVerificationBody = `<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Verify your email</h1>
    <p style="color: #888; margin: 0 0 24px;">Click below to verify your email address for {{.AppName}}.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Verify Email</a>
  </div>
</body>
</html>`

const defaultMagicLinkBody = `<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Sign in to {{.AppName}}</h1>
    <p style="color: #888; margin: 0 0 24px;">Click the button below to securely sign in.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Sign in</a>
    <p style="color: #666; font-size: 12px; margin-top: 24px;">This link expires in 15 minutes.</p>
  </div>
</body>
</html>`

const defaultEmailChangeBody = `<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Confirm email change</h1>
    <p style="color: #888; margin: 0 0 24px;">Click below to confirm your new email address.</p>
    <a href="{{.Link}}" style="display: block; background: #fafafa; color: #0a0a0a; text-decoration: none; text-align: center; padding: 12px; border-radius: 8px; font-weight: 600; font-size: 14px;">Confirm Change</a>
  </div>
</body>
</html>`

const defaultAuthAlertBody = `<!DOCTYPE html>
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
</html>`

const defaultBackupBody = `<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Backup completed</h1>
    <p style="color: #888; margin: 0 0 24px;">Backup "{{.Name}}" ({{.Size}}) has been created successfully.</p>
  </div>
</body>
</html>`

const defaultBackupFailedBody = `<!DOCTYPE html>
<html>
<body style="font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; padding: 40px;">
  <div style="max-width: 400px; margin: 0 auto; background: #141414; border-radius: 12px; padding: 32px; border: 1px solid #222;">
    <h1 style="font-size: 20px; margin: 0 0 8px;">Scheduled backup failed</h1>
    <p style="color: #888; margin: 0 0 24px;">A scheduled backup on {{.Instance}} failed and may need attention.</p>
    <div style="background: #1a1a1a; border-radius: 8px; padding: 16px; font-size: 13px; color: #aaa; line-height: 1.8;">
      <p style="margin: 0;">Error: {{.Error}}</p>
      <p style="margin: 0;">Time: {{.Time}}</p>
      <p style="margin: 0;">Instance: {{.Instance}}</p>
    </div>
    <p style="color: #666; font-size: 12px; margin-top: 24px;">Check the {{.AppName}} server logs for the full failure details. Repeated alerts are throttled to at most one per hour.</p>
  </div>
</body>
</html>`
