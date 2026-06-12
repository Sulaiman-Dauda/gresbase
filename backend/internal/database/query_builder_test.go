package database

import (
	"strings"
	"testing"
)

func TestBuildSelectClauses(t *testing.T) {
	sql, args, err := NewQuery(nil, "orders").
		Select("status", "SUM(amount) AS total").
		Join("JOIN customers ON customers.id = orders.customer_id").
		Where("amount > $", 10).
		And().
		Where("status = $", "paid").
		GroupBy("status").
		Having("COUNT(*) > $", 1).
		OrderBy("status ASC").
		Limit(50).
		Offset(10).
		BuildSelect()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	for _, want := range []string{
		"SELECT status, SUM(amount) AS total FROM orders",
		"JOIN customers",
		"WHERE amount > $1 AND status = $2",
		"GROUP BY status",
		"HAVING COUNT(*) > $3",
		"ORDER BY status ASC",
		"LIMIT 50",
		"OFFSET 10",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in: %s", want, sql)
		}
	}
	if len(args) != 3 || args[0] != 10 || args[1] != "paid" || args[2] != 1 {
		t.Fatalf("args mismatch: %v", args)
	}

	// Clause ordering: WHERE before GROUP BY before HAVING before ORDER BY.
	if !(strings.Index(sql, "WHERE") < strings.Index(sql, "GROUP BY") &&
		strings.Index(sql, "GROUP BY") < strings.Index(sql, "HAVING") &&
		strings.Index(sql, "HAVING") < strings.Index(sql, "ORDER BY")) {
		t.Fatalf("clause order wrong: %s", sql)
	}
}

func TestBuildSelectDefaultsAndDistinct(t *testing.T) {
	sql, args, err := NewQuery(nil, "users").Distinct().BuildSelect()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if sql != "SELECT DISTINCT * FROM users" {
		t.Fatalf("unexpected SQL: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
}

func TestWhereHelpers(t *testing.T) {
	sql, args, err := NewQuery(nil, "t").
		WhereIn("status", []any{"a", "b"}).
		And().
		WhereNull("deleted_at").
		And().
		WhereNotNull("created_at").
		And().
		WhereJSONContains("tags", "go").
		BuildSelect()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(sql, "status IN ($1, $2)") {
		t.Errorf("WhereIn numbering wrong: %s", sql)
	}
	if !strings.Contains(sql, "deleted_at IS NULL") || !strings.Contains(sql, "created_at IS NOT NULL") {
		t.Errorf("null checks missing: %s", sql)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args (2 IN + 1 JSON), got %v", args)
	}
}

func TestWhereParenGrouping(t *testing.T) {
	sql, _, err := NewQuery(nil, "t").
		OpenParen().
		Where("a = $", 1).
		Or().
		Where("b = $", 2).
		CloseParen().
		And().
		Where("c = $", 3).
		BuildSelect()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(sql, "( a = $1 OR b = $2 ) AND c = $3") {
		t.Fatalf("paren grouping wrong: %s", sql)
	}
}

func TestBuildUpdateSetThenWhere(t *testing.T) {
	sql, args, err := NewQuery(nil, "posts").
		Set("title", "new").
		SetRaw(`"updated_at" = NOW()`).
		Where("id = $", "r1").
		Returning("*").
		BuildUpdate()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(sql, `UPDATE posts SET title = $1, "updated_at" = NOW() WHERE id = $2 RETURNING *`) {
		t.Fatalf("unexpected SQL: %s", sql)
	}
	// SET args precede WHERE args, matching the $ numbering above.
	if len(args) != 2 || args[0] != "new" || args[1] != "r1" {
		t.Fatalf("args mismatch: %v", args)
	}
}

func TestBuildUpdateRequiresSet(t *testing.T) {
	if _, _, err := NewQuery(nil, "posts").Where("id = $", 1).BuildUpdate(); err == nil {
		t.Fatal("UPDATE without SET must error")
	}
}

func TestBuildInsert(t *testing.T) {
	sql, args, err := NewQuery(nil, "posts").
		Returning("id").
		BuildInsert([]string{"title", "amount"}, []any{"hello", 5})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if sql != "INSERT INTO posts (title, amount) VALUES ($1, $2) RETURNING id" {
		t.Fatalf("unexpected SQL: %s", sql)
	}
	if len(args) != 2 || args[0] != "hello" || args[1] != 5 {
		t.Fatalf("args mismatch: %v", args)
	}

	if _, _, err := NewQuery(nil, "posts").BuildInsert([]string{"a"}, []any{1, 2}); err == nil {
		t.Fatal("column/value count mismatch must error")
	}
}

func TestBuildDeleteAndCount(t *testing.T) {
	sql, args, err := NewQuery(nil, "posts").Where("id = $", "r1").BuildDelete()
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if sql != "DELETE FROM posts WHERE id = $1" || len(args) != 1 {
		t.Fatalf("unexpected delete: %s %v", sql, args)
	}

	sql, args, err = NewQuery(nil, "posts").Where("status = $", "paid").BuildCount()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if sql != "SELECT COUNT(*) FROM posts WHERE status = $1" || len(args) != 1 {
		t.Fatalf("unexpected count: %s %v", sql, args)
	}
}
