package database

import (
	"strings"
	"testing"
)

func TestValidateIdentifier(t *testing.T) {
	valid := []string{"posts", "_internal", "user_name2", "A", "snake_case_99"}
	for _, name := range valid {
		if err := ValidateIdentifier(name, "column"); err != nil {
			t.Errorf("%q should be valid: %v", name, err)
		}
		if !IsSafeIdentifier(name) {
			t.Errorf("IsSafeIdentifier(%q) should be true", name)
		}
	}

	invalid := []string{
		"",
		"  ",
		"1starts_with_digit",
		"has-hyphen",
		"has space",
		`has"quote`,
		"semi;colon",
		"drop table users;--",
		"ünicode",
		"dots.path",
	}
	for _, name := range invalid {
		if err := ValidateIdentifier(name, "column"); err == nil {
			t.Errorf("%q should be rejected", name)
		}
	}
}

func TestValidateIdentifierKindInMessage(t *testing.T) {
	err := ValidateIdentifier("", "table name")
	if err == nil || !strings.Contains(err.Error(), "table name") {
		t.Fatalf("error should name the kind: %v", err)
	}
	err = ValidateIdentifier("bad-name", "")
	if err == nil || !strings.Contains(err.Error(), "identifier") {
		t.Fatalf("empty kind should fall back to 'identifier': %v", err)
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := QuoteIdent("user_name"); got != `"user_name"` {
		t.Fatalf("plain quote: %s", got)
	}
	// Embedded quotes are doubled so the identifier cannot break out.
	if got := QuoteIdent(`evil"ident`); got != `"evil""ident"` {
		t.Fatalf("quote escaping: %s", got)
	}
}
