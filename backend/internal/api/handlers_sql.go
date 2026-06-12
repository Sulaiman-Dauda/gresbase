package api

import (
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SQL console + schema introspection (superuser only).
// In-process replacement for what Supabase runs a separate postgres-meta
// container to provide.
// ---------------------------------------------------------------------------

const sqlMaxRows = 1000

type sqlExecuteForm struct {
	Query string `json:"query"`
	// Write must be set explicitly; without it the statement runs inside a
	// READ ONLY transaction so an exploratory query cannot mutate data.
	Write bool `json:"write"`
}

// SQLExecute runs an arbitrary SQL statement as the application's database
// user. Read-only by default; writes require {"write": true}.
func (h *Handlers) SQLExecute(w http.ResponseWriter, r *http.Request) {
	var form sqlExecuteForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	query := strings.TrimSpace(form.Query)
	if query == "" {
		writeError(w, 400, "query is required")
		return
	}

	ctx := r.Context()
	conn, err := h.app.DB().Pool.Acquire(ctx)
	if err != nil {
		writeError(w, 500, "Failed to acquire connection: "+err.Error())
		return
	}
	defer conn.Release()

	mode := "READ WRITE"
	if !form.Write {
		mode = "READ ONLY"
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET TRANSACTION "+mode); err != nil {
		writeError(w, 500, err.Error())
		return
	}

	start := time.Now()
	rows, err := tx.Query(ctx, query)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	columns := []string{}
	for _, fd := range rows.FieldDescriptions() {
		columns = append(columns, fd.Name)
	}

	results := []map[string]any{}
	truncated := false
	for rows.Next() {
		if len(results) >= sqlMaxRows {
			truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			break
		}
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeSQLValue(values[i])
		}
		results = append(results, row)
	}
	tag := rows.CommandTag()
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "sql.execute", "", "", map[string]any{
		"write": form.Write,
		"rows":  len(results),
	}, r)

	writeOK(w, map[string]any{
		"columns":       columns,
		"rows":          results,
		"row_count":     len(results),
		"truncated":     truncated,
		"rows_affected": tag.RowsAffected(),
		"duration_ms":   time.Since(start).Milliseconds(),
	})
}

func normalizeSQLValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339Nano)
	default:
		return v
	}
}

// SQLSchema returns a structured snapshot of the public schema: tables,
// columns, indexes and views, with row estimates and sizes.
func (h *Handlers) SQLSchema(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := h.app.DB()

	type column struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Nullable bool   `json:"nullable"`
		Default  string `json:"default,omitempty"`
	}
	type table struct {
		Name      string   `json:"name"`
		Kind      string   `json:"kind"` // table, view, materialized_view
		RowsEst   int64    `json:"rows_estimate"`
		SizeBytes int64    `json:"size_bytes"`
		Columns   []column `json:"columns"`
		Indexes   []string `json:"indexes"`
	}

	tables := map[string]*table{}
	order := []string{}

	rows, err := db.Query(ctx, `
		SELECT c.relname,
		       CASE c.relkind WHEN 'r' THEN 'table' WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized_view' END,
		       c.reltuples::bigint,
		       pg_total_relation_size(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r','v','m')
		ORDER BY c.relname`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	for rows.Next() {
		t := &table{Columns: []column{}, Indexes: []string{}}
		if err := rows.Scan(&t.Name, &t.Kind, &t.RowsEst, &t.SizeBytes); err != nil {
			continue
		}
		if t.RowsEst < 0 {
			t.RowsEst = 0
		}
		tables[t.Name] = t
		order = append(order, t.Name)
	}
	rows.Close()

	rows, err = db.Query(ctx, `
		SELECT table_name, column_name, data_type, is_nullable = 'YES', COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	for rows.Next() {
		var tableName string
		var col column
		if err := rows.Scan(&tableName, &col.Name, &col.Type, &col.Nullable, &col.Default); err != nil {
			continue
		}
		if t, ok := tables[tableName]; ok {
			t.Columns = append(t.Columns, col)
		}
	}
	rows.Close()

	rows, err = db.Query(ctx, `
		SELECT tablename, indexdef FROM pg_indexes WHERE schemaname = 'public' ORDER BY indexname`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	for rows.Next() {
		var tableName, indexDef string
		if err := rows.Scan(&tableName, &indexDef); err != nil {
			continue
		}
		if t, ok := tables[tableName]; ok {
			t.Indexes = append(t.Indexes, indexDef)
		}
	}
	rows.Close()

	result := make([]*table, 0, len(order))
	for _, name := range order {
		result = append(result, tables[name])
	}
	writeOK(w, map[string]any{"schema": "public", "tables": result})
}
