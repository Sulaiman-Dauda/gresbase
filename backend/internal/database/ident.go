package database

import (
	"fmt"
	"regexp"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateIdentifier ensures that an SQL identifier is safe to use as a table,
// column, index or other schema object name.
func ValidateIdentifier(name, kind string) error {
	if strings.TrimSpace(name) == "" {
		if kind == "" {
			kind = "identifier"
		}
		return fmt.Errorf("%s is required", kind)
	}

	if !identifierPattern.MatchString(name) {
		if kind == "" {
			kind = "identifier"
		}
		return fmt.Errorf("invalid %s %q: only letters, digits, and underscores are allowed, and it must not start with a digit", kind, name)
	}

	return nil
}

// IsSafeIdentifier reports whether name passes ValidateIdentifier.
func IsSafeIdentifier(name string) bool {
	return identifierPattern.MatchString(name)
}

// QuoteIdent quotes a PostgreSQL identifier.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
