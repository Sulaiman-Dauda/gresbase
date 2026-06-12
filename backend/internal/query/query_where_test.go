package query

import (
	"context"
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
)

func testCollection() *collection.Collection {
	return &collection.Collection{
		Name: "posts",
		Schema: []collection.SchemaField{
			{Name: "owner", Type: collection.FieldText},
			{Name: "age", Type: collection.FieldNumber},
		},
	}
}

// TestAccessRuleAndFilterPlaceholders is the regression test for the
// placeholder collision: an access rule and a client filter are compiled
// separately and ANDed together, so the filter's placeholders must continue
// numbering after the rule's.
func TestAccessRuleAndFilterPlaceholders(t *testing.T) {
	b := NewBuilder(nil, testCollection()).
		WithParams(Params{Filter: `age > 18`, Page: 1, PerPage: 30}).
		WithAccessRule(`owner = "user_1"`)

	if err := b.BuildWhere(context.Background()); err != nil {
		t.Fatalf("BuildWhere: %v", err)
	}

	if len(b.whereArgs) != 2 {
		t.Fatalf("expected 2 args (rule + filter), got %v", b.whereArgs)
	}
	if !strings.Contains(b.whereClause, "$1") || !strings.Contains(b.whereClause, "$2") {
		t.Fatalf("expected $1 and $2 in combined clause: %s", b.whereClause)
	}
	if strings.Count(b.whereClause, "$1") != 1 {
		t.Fatalf("placeholder $1 reused — collision is back: %s", b.whereClause)
	}
	// Args order must match numbering: rule arg first, filter arg second.
	if b.whereArgs[0] != "user_1" {
		t.Fatalf("rule arg should be first: %v", b.whereArgs)
	}
}

func TestBuildWhereInvalidFilterFailsLoudly(t *testing.T) {
	b := NewBuilder(nil, testCollection()).
		WithParams(Params{Filter: `age >`, Page: 1, PerPage: 30})
	if err := b.BuildWhere(context.Background()); err == nil {
		t.Fatal("malformed filter must return an error, not silently match everything")
	}
}

func TestBuildSortRejectsInjection(t *testing.T) {
	cases := map[string]string{
		"":                        `"created_at" DESC`, // default
		"age":                     `"age" ASC`,
		"-age":                    `"age" DESC`,
		"+age,-owner":             `"age" ASC, "owner" DESC`,
		"age;DROP TABLE posts":    `"created_at" DESC`, // injection chars → default order
		`age" DESC, (SELECT 1)--`: `"created_at" DESC`,
	}
	for sortParam, want := range cases {
		b := NewBuilder(nil, testCollection()).WithParams(Params{Sort: sortParam, Page: 1, PerPage: 30})
		b.buildSort()
		got := b.orderClause
		if got == "" {
			got = `"created_at" DESC`
		}
		if got != want {
			t.Errorf("sort %q: got %q, want %q", sortParam, got, want)
		}
	}
}

func TestWithParamsClamps(t *testing.T) {
	b := NewBuilder(nil, testCollection()).WithParams(Params{Page: -3, PerPage: 9999})
	if b.params.Page != 1 {
		t.Errorf("negative page should clamp to 1, got %d", b.params.Page)
	}
	if b.params.PerPage != 500 {
		t.Errorf("perPage should cap at 500, got %d", b.params.PerPage)
	}
}
