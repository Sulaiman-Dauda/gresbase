package filter

import (
	"strings"
	"testing"
)

func TestFilterToSQLOffset(t *testing.T) {
	expr, err := ParseFilter(`age > 18 && status = "active"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	sql, args, err := FilterToSQLOffset(expr, nil, 3)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if !strings.Contains(sql, "$3") || !strings.Contains(sql, "$4") {
		t.Fatalf("expected placeholders $3 and $4, got: %s", sql)
	}
	if strings.Contains(sql, "$1") || strings.Contains(sql, "$2") {
		t.Fatalf("offset compile must not emit $1/$2: %s", sql)
	}
}

// TestCombinedClausesDoNotCollide reproduces the access-rule + client-filter
// combination: two separately compiled fragments share one args slice, so the
// second fragment must start numbering after the first.
func TestCombinedClausesDoNotCollide(t *testing.T) {
	ruleExpr, err := ParseFilter(`owner = "user_1"`)
	if err != nil {
		t.Fatalf("parse rule: %v", err)
	}
	ruleSQL, ruleArgs, err := FilterToSQLOffset(ruleExpr, nil, 1)
	if err != nil {
		t.Fatalf("compile rule: %v", err)
	}

	filterExpr, err := ParseFilter(`age > 18`)
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	filterSQL, filterArgs, err := FilterToSQLOffset(filterExpr, nil, len(ruleArgs)+1)
	if err != nil {
		t.Fatalf("compile filter: %v", err)
	}

	combined := "(" + ruleSQL + ") AND (" + filterSQL + ")"
	args := append(append([]any{}, ruleArgs...), filterArgs...)

	if len(args) != 2 {
		t.Fatalf("expected 2 combined args, got %d", len(args))
	}
	if !strings.Contains(ruleSQL, "$1") {
		t.Fatalf("rule fragment should use $1: %s", ruleSQL)
	}
	if !strings.Contains(filterSQL, "$2") {
		t.Fatalf("filter fragment should use $2 after offset, got: %s", filterSQL)
	}
	if strings.Count(combined, "$1") != 1 {
		t.Fatalf("placeholder $1 must appear exactly once in combined SQL: %s", combined)
	}
}
