package forms

import (
	"errors"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
	appsettings "github.com/gresbase/gresbase/internal/settings"
)

func TestLoginFormValidate(t *testing.T) {
	form := &LoginForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form = &LoginForm{Email: "admin@example.com", Password: "secret"}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid form, got %v", err)
	}
}

func TestAdminUpsertFormValidateUpdate(t *testing.T) {
	form := &AdminUpsertForm{}
	if err := form.ValidateUpdate(); err == nil {
		t.Fatal("expected update validation error")
	}

	form = &AdminUpsertForm{Avatar: "avatar.png"}
	if err := form.ValidateUpdate(); err != nil {
		t.Fatalf("expected valid update form, got %v", err)
	}
}

func TestCollectionUpsertFormValidate(t *testing.T) {
	svc := collection.NewService(nil)
	form := &CollectionUpsertForm{Collection: collection.Collection{
		Name:   "posts",
		Type:   collection.TypeBase,
		Schema: []collection.SchemaField{{Name: "title", Type: collection.FieldText, Required: true}},
	}}

	if err := form.Validate(svc); err != nil {
		t.Fatalf("expected valid collection form, got %v", err)
	}
}

func TestCollectionsImportFormValidate(t *testing.T) {
	form := &CollectionsImportForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form.Collections = []map[string]any{{"name": "posts", "type": "base"}}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid import form, got %v", err)
	}
}

func TestRecordAuthPasswordFormValidate(t *testing.T) {
	form := &RecordAuthPasswordForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form = &RecordAuthPasswordForm{Identity: "user@example.com", Password: "secret123"}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid form, got %v", err)
	}
}

func TestOAuthCallbackFormValidate(t *testing.T) {
	form := &OAuthCallbackForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form = &OAuthCallbackForm{Code: "code", State: "state"}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid form, got %v", err)
	}
}

func TestBatchPayloadFormValidate(t *testing.T) {
	form := &BatchPayloadForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form = &BatchPayloadForm{Requests: []BatchRequestForm{{Method: "POST", URL: "/api/v1/health"}}}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid form, got %v", err)
	}
}

func TestBatchRecordsFormValidate(t *testing.T) {
	form := &BatchRecordsForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form = &BatchRecordsForm{Deletes: []string{"rec1"}}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid form, got %v", err)
	}
}

func TestLogsQueryFormValidate(t *testing.T) {
	form := &LogsQueryForm{DateFrom: "not-a-date"}
	if err := form.Validate(); err == nil {
		t.Fatal("expected date validation error")
	}

	form = &LogsQueryForm{Page: 0, PerPage: 500, DateFrom: "2026-05-25"}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid form, got %v", err)
	}
	if form.Page != 1 || form.PerPage != 200 {
		t.Fatalf("expected normalized pagination, got page=%d perPage=%d", form.Page, form.PerPage)
	}
}

func TestSettingsUpdateFormValidate(t *testing.T) {
	form := &SettingsUpdateForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	settings := appsettings.DefaultSettings()
	form = &SettingsUpdateForm{Settings: settings}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid settings form, got %v", err)
	}
}

func TestSettingsUpdateFormEmailTemplates(t *testing.T) {
	// Valid override passes.
	settings := appsettings.DefaultSettings()
	settings.EmailTemplates = map[string]appsettings.EmailTemplate{
		"otp": {Subject: "Code: {{.Code}}", Body: "<p>{{.Code}}</p>"},
	}
	form := &SettingsUpdateForm{Settings: settings}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid email template override, got %v", err)
	}

	// Broken template is rejected at save time with a field-scoped error.
	settings = appsettings.DefaultSettings()
	settings.EmailTemplates = map[string]appsettings.EmailTemplate{
		"otp": {Body: "<p>{{.Code</p>"},
	}
	form = &SettingsUpdateForm{Settings: settings}
	err := form.Validate()
	if err == nil {
		t.Fatal("expected validation error for broken template")
	}
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("expected forms.Errors, got %T", err)
	}
	if _, ok := errs["email_templates.otp"]; !ok {
		t.Errorf("expected error keyed by email_templates.otp, got %v", errs)
	}

	// Unknown template id is rejected.
	settings = appsettings.DefaultSettings()
	settings.EmailTemplates = map[string]appsettings.EmailTemplate{
		"bogus": {Subject: "x"},
	}
	form = &SettingsUpdateForm{Settings: settings}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error for unknown template id")
	}

	// Empty entry means "use default" and is always valid.
	settings = appsettings.DefaultSettings()
	settings.EmailTemplates = map[string]appsettings.EmailTemplate{
		"otp": {},
	}
	form = &SettingsUpdateForm{Settings: settings}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected empty entry to validate, got %v", err)
	}
}

func TestJobCreateFormValidate(t *testing.T) {
	form := &JobCreateForm{}
	if err := form.Validate(); err == nil {
		t.Fatal("expected validation error")
	}

	form = &JobCreateForm{Name: "cleanup", CronExpr: "*/5 * * * *"}
	if err := form.Validate(); err != nil {
		t.Fatalf("expected valid job form, got %v", err)
	}
}
