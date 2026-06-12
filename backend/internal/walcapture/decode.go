package walcapture

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/jackc/pglogrepl"
)

// PostgreSQL type OIDs used for decoding columns that are not in the
// collection schema (system columns like created_at/updated_at/verified).
const (
	oidBool        = 16
	oidInt8        = 20
	oidInt2        = 21
	oidInt4        = 23
	oidJSON        = 114
	oidFloat4      = 700
	oidFloat8      = 701
	oidNumeric     = 1700
	oidTimestamp   = 1114
	oidTimestampTZ = 1184
	oidDate        = 1082
	oidJSONB       = 3802
)

// pgTimestampLayouts are the text output formats PostgreSQL uses for
// timestamp/timestamptz under the default DateStyle (ISO).
var pgTimestampLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999-07",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

// decodeTuple converts a pgoutput tuple (text-format column values) into the
// same map shape the API path broadcasts:
//
//   - numbers as float64 / int64 (JSON numbers)
//   - booleans as bool
//   - json/jsonb columns unmarshaled
//   - schema date fields as RFC3339 strings (matching normalizeFieldValue);
//     non-schema timestamp columns (created_at/updated_at) as time.Time,
//     which marshals to the same RFC3339 wire format the API path produces
//   - bytea/file/vector values as their stored text representation
//   - password fields and tokenKey are stripped — realtime events must never
//     carry more than the API auth responses expose
//
// Columns marked TOASTed-and-unchanged by pgoutput are omitted (their value
// is not in the WAL). NULLs become explicit nil entries.
func decodeTuple(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData, fields map[string]collection.FieldType) map[string]any {
	if rel == nil || tuple == nil || len(tuple.Columns) != len(rel.Columns) {
		return nil
	}

	record := make(map[string]any, len(tuple.Columns))
	for i, col := range tuple.Columns {
		relCol := rel.Columns[i]
		name := relCol.Name

		// Never leak secrets: password-type schema fields and the auth token
		// salt column are stripped, same as the record auth API responses.
		if fields[name] == collection.FieldPassword || name == "tokenKey" {
			continue
		}

		switch col.DataType {
		case pglogrepl.TupleDataTypeNull:
			record[name] = nil
		case pglogrepl.TupleDataTypeToast:
			// Unchanged TOASTed value: not present in the WAL. Omit the key
			// rather than inventing a value.
		case pglogrepl.TupleDataTypeText, pglogrepl.TupleDataTypeBinary:
			record[name] = decodeColumnValue(string(col.Data), fields[name], relCol.DataType)
		}
	}
	return record
}

// decodeColumnValue converts one text-format column value using the
// collection schema field type first, falling back to the PostgreSQL type
// OID for columns outside the schema (system columns).
func decodeColumnValue(raw string, fieldType collection.FieldType, typeOID uint32) any {
	switch fieldType {
	case collection.FieldNumber:
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
		return raw
	case collection.FieldBool:
		return raw == "t" || raw == "true"
	case collection.FieldJSON, collection.FieldGeoPoint, collection.FieldFile, collection.FieldRelation:
		// Mirrors collection.decodeMaybeJSONValue: single relations stay
		// plain strings; jsonb-backed values unmarshal.
		return decodeMaybeJSON(raw)
	case collection.FieldDate, collection.FieldAutoDate:
		if t, ok := parsePGTimestamp(raw); ok {
			return t.UTC().Format(time.RFC3339) // matches normalizeFieldValue
		}
		return raw
	case "":
		return decodeByOID(raw, typeOID)
	default:
		// text, email, url, select, editor, vector, …: stored text as-is.
		return raw
	}
}

// decodeByOID handles columns absent from the collection schema metadata
// (id, created_at, updated_at, verified, …) using the wire type OID.
func decodeByOID(raw string, typeOID uint32) any {
	switch typeOID {
	case oidBool:
		return raw == "t" || raw == "true"
	case oidInt2, oidInt4, oidInt8:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n
		}
		return raw
	case oidFloat4, oidFloat8, oidNumeric:
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
		return raw
	case oidJSON, oidJSONB:
		return decodeMaybeJSON(raw)
	case oidTimestamp, oidTimestampTZ, oidDate:
		if t, ok := parsePGTimestamp(raw); ok {
			// time.Time, exactly like pgx rows.Values() in the API path;
			// json.Marshal renders it as RFC3339.
			return t.UTC()
		}
		return raw
	default:
		return raw
	}
}

// decodeMaybeJSON unmarshals values that look like JSON documents, mirroring
// collection.decodeMaybeJSONValue.
func decodeMaybeJSON(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
	}
	return raw
}

// parsePGTimestamp parses PostgreSQL's ISO text output for timestamp,
// timestamptz, and date columns. Values without a zone are taken as UTC.
func parsePGTimestamp(raw string) (time.Time, bool) {
	for _, layout := range pgTimestampLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// tupleColumnText returns a column's raw text value from a tuple, or "" when
// absent/NULL. Used to extract the primary key from delete old-tuples.
func tupleColumnText(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData, column string) string {
	if rel == nil || tuple == nil || len(tuple.Columns) != len(rel.Columns) {
		return ""
	}
	for i, relCol := range rel.Columns {
		if relCol.Name != column {
			continue
		}
		col := tuple.Columns[i]
		if col.DataType == pglogrepl.TupleDataTypeText || col.DataType == pglogrepl.TupleDataTypeBinary {
			return string(col.Data)
		}
		return ""
	}
	return ""
}
