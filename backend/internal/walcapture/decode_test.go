package walcapture

import (
	"reflect"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/jackc/pglogrepl"
)

func textCol(v string) *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeText, Data: []byte(v)}
}

func nullCol() *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeNull}
}

func toastCol() *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeToast}
}

func relation(cols ...*pglogrepl.RelationMessageColumn) *pglogrepl.RelationMessage {
	return &pglogrepl.RelationMessage{RelationID: 1, RelationName: "posts", Columns: cols}
}

func relCol(name string, oid uint32) *pglogrepl.RelationMessageColumn {
	return &pglogrepl.RelationMessageColumn{Name: name, DataType: oid}
}

func TestDecodeTuple_FieldTypeShapes(t *testing.T) {
	rel := relation(
		relCol("id", 25), // text
		relCol("title", 25),
		relCol("count", oidFloat8),
		relCol("active", oidBool),
		relCol("meta", oidJSONB),
		relCol("tags", oidJSONB),
		relCol("attachment", oidJSONB),
		relCol("friend", 25),
		relCol("friends", oidJSONB),
		relCol("published", oidTimestampTZ),
		relCol("location", oidJSONB),
	)
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("rec1"),
		textCol("hello"),
		textCol("4.5"),
		textCol("t"),
		textCol(`{"a": 1}`),
		textCol(`["x", "y"]`),
		textCol(`["f1.png"]`),
		textCol("rel-id-1"),
		textCol(`["r1", "r2"]`),
		textCol("2026-06-12 10:30:00.123456+00"),
		textCol(`{"lat": 1.5, "lon": 2.5}`),
	}}
	fields := map[string]collection.FieldType{
		"title":      collection.FieldText,
		"count":      collection.FieldNumber,
		"active":     collection.FieldBool,
		"meta":       collection.FieldJSON,
		"tags":       collection.FieldJSON,
		"attachment": collection.FieldFile,
		"friend":     collection.FieldRelation,
		"friends":    collection.FieldRelation,
		"published":  collection.FieldDate,
		"location":   collection.FieldGeoPoint,
	}

	record := decodeTuple(rel, tuple, fields)
	if record == nil {
		t.Fatal("expected record")
	}

	if record["id"] != "rec1" || record["title"] != "hello" {
		t.Fatalf("string fields wrong: %v", record)
	}
	if v, ok := record["count"].(float64); !ok || v != 4.5 {
		t.Fatalf("number field must be float64 4.5, got %T %v", record["count"], record["count"])
	}
	if v, ok := record["active"].(bool); !ok || !v {
		t.Fatalf("bool field must be true, got %T %v", record["active"], record["active"])
	}
	meta, ok := record["meta"].(map[string]any)
	if !ok || meta["a"] != float64(1) {
		t.Fatalf("json field must unmarshal to map, got %T %v", record["meta"], record["meta"])
	}
	if tags, ok := record["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("json array field must unmarshal, got %T", record["tags"])
	}
	if files, ok := record["attachment"].([]any); !ok || files[0] != "f1.png" {
		t.Fatalf("file field must unmarshal to array, got %T %v", record["attachment"], record["attachment"])
	}
	if record["friend"] != "rel-id-1" {
		t.Fatalf("single relation must stay a string, got %T %v", record["friend"], record["friend"])
	}
	if rels, ok := record["friends"].([]any); !ok || len(rels) != 2 {
		t.Fatalf("multi relation must unmarshal to array, got %T", record["friends"])
	}
	if record["published"] != "2026-06-12T10:30:00Z" {
		t.Fatalf("date field must be RFC3339 (matching the API path), got %v", record["published"])
	}
	if loc, ok := record["location"].(map[string]any); !ok || loc["lat"] != 1.5 {
		t.Fatalf("geo point must unmarshal, got %T %v", record["location"], record["location"])
	}
}

func TestDecodeTuple_SystemColumnsByOID(t *testing.T) {
	rel := relation(
		relCol("id", 25),
		relCol("verified", oidBool),
		relCol("created_at", oidTimestampTZ),
		relCol("legacy_count", oidInt8),
	)
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("rec2"),
		textCol("f"),
		textCol("2026-06-12 08:00:00+00"),
		textCol("42"),
	}}

	record := decodeTuple(rel, tuple, map[string]collection.FieldType{})
	if v, ok := record["verified"].(bool); !ok || v {
		t.Fatalf("verified must decode to bool false, got %T %v", record["verified"], record["verified"])
	}
	ts, ok := record["created_at"].(time.Time)
	if !ok {
		t.Fatalf("created_at must decode to time.Time (matches API rows.Values), got %T", record["created_at"])
	}
	if got := ts.UTC().Format(time.RFC3339); got != "2026-06-12T08:00:00Z" {
		t.Fatalf("created_at wrong: %s", got)
	}
	if v, ok := record["legacy_count"].(int64); !ok || v != 42 {
		t.Fatalf("int column must decode to int64, got %T %v", record["legacy_count"], record["legacy_count"])
	}
}

func TestDecodeTuple_StripsSecrets(t *testing.T) {
	rel := relation(
		relCol("id", 25),
		relCol("email", 25),
		relCol("password", 25),
		relCol("tokenKey", 25),
	)
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("u1"),
		textCol("a@b.com"),
		textCol("$2a$10$hash"),
		textCol("secret-salt"),
	}}
	fields := map[string]collection.FieldType{
		"email":    collection.FieldEmail,
		"password": collection.FieldPassword,
	}

	record := decodeTuple(rel, tuple, fields)
	if _, exists := record["password"]; exists {
		t.Fatal("password field must be stripped from WAL events")
	}
	if _, exists := record["tokenKey"]; exists {
		t.Fatal("tokenKey must be stripped from WAL events")
	}
	if record["email"] != "a@b.com" {
		t.Fatalf("non-secret fields must survive, got %v", record)
	}
}

func TestDecodeTuple_NullAndToast(t *testing.T) {
	rel := relation(relCol("id", 25), relCol("title", 25), relCol("body", 25))
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{
		textCol("r1"),
		nullCol(),
		toastCol(),
	}}

	record := decodeTuple(rel, tuple, map[string]collection.FieldType{
		"title": collection.FieldText,
		"body":  collection.FieldText,
	})
	if v, exists := record["title"]; !exists || v != nil {
		t.Fatalf("NULL column must be an explicit nil, got %v exists=%v", v, exists)
	}
	if _, exists := record["body"]; exists {
		t.Fatal("unchanged TOAST column must be omitted, not invented")
	}
}

func TestDecodeTuple_ColumnCountMismatch(t *testing.T) {
	rel := relation(relCol("id", 25), relCol("title", 25))
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{textCol("r1")}}
	if record := decodeTuple(rel, tuple, nil); record != nil {
		t.Fatalf("mismatched tuple must be rejected, got %v", record)
	}
}

func TestTupleColumnText(t *testing.T) {
	rel := relation(relCol("id", 25), relCol("title", 25))
	tuple := &pglogrepl.TupleData{Columns: []*pglogrepl.TupleDataColumn{textCol("r9"), nullCol()}}
	if got := tupleColumnText(rel, tuple, "id"); got != "r9" {
		t.Fatalf("id = %q", got)
	}
	if got := tupleColumnText(rel, tuple, "title"); got != "" {
		t.Fatalf("null column must return empty, got %q", got)
	}
	if got := tupleColumnText(rel, tuple, "missing"); got != "" {
		t.Fatalf("missing column must return empty, got %q", got)
	}
}

func TestParsePGTimestamp(t *testing.T) {
	cases := map[string]string{
		"2026-06-12 10:30:00.123456+00": "2026-06-12T10:30:00Z",
		"2026-06-12 10:30:00+02":        "2026-06-12T08:30:00Z",
		"2026-06-12 10:30:00":           "2026-06-12T10:30:00Z",
		"2026-06-12":                    "2026-06-12T00:00:00Z",
	}
	for in, want := range cases {
		ts, ok := parsePGTimestamp(in)
		if !ok {
			t.Fatalf("failed to parse %q", in)
		}
		if got := ts.UTC().Format(time.RFC3339); got != want {
			t.Fatalf("%q: got %s want %s", in, got, want)
		}
	}
	if _, ok := parsePGTimestamp("not a date"); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestPublicationSyncStatements(t *testing.T) {
	stmts := publicationSyncStatements("gresbase_realtime",
		[]string{"old_table", "posts"},
		[]string{"posts", "users"},
	)
	want := []string{
		`ALTER PUBLICATION "gresbase_realtime" ADD TABLE "users"`,
		`ALTER PUBLICATION "gresbase_realtime" DROP TABLE "old_table"`,
	}
	if !reflect.DeepEqual(stmts, want) {
		t.Fatalf("got %v want %v", stmts, want)
	}

	if stmts := publicationSyncStatements("p", []string{"a"}, []string{"a"}); len(stmts) != 0 {
		t.Fatalf("no-op sync must produce no statements, got %v", stmts)
	}

	// Empty desired set must drop everything (SET TABLE cannot express this).
	stmts = publicationSyncStatements("p", []string{"a"}, nil)
	if len(stmts) != 1 || stmts[0] != `ALTER PUBLICATION "p" DROP TABLE "a"` {
		t.Fatalf("got %v", stmts)
	}
}
