// Package query provides a query builder for record listing with filtering,
// sorting, pagination, field expansion, and relation expansion.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/jackc/pgx/v5"
)

// Params represents query parameters from the HTTP request.
type Params struct {
	Filter  string `json:"filter"`
	Sort    string `json:"sort"`
	Expand  string `json:"expand"`
	Fields  string `json:"fields"`
	Page    int    `json:"page"`
	PerPage int    `json:"perPage"`
	SkipTotal bool `json:"skipTotal"`
}

// DefaultParams creates query params with defaults.
func DefaultParams() Params {
	return Params{
		Page:    1,
		PerPage: 30,
	}
}

// ParseParams extracts query params from a URL query string.
func ParseParams(rawQuery string) Params {
	p := DefaultParams()

	if rawQuery == "" {
		return p
	}

	// Parse the raw query string manually (net/url.ParseQuery but simple)
	pairs := strings.Split(rawQuery, "&")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, val := kv[0], kv[1]

		switch key {
		case "filter":
			p.Filter = val
		case "sort":
			p.Sort = val
		case "expand":
			p.Expand = val
		case "fields":
			p.Fields = val
		case "page":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				p.Page = n
			}
		case "perPage":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				if n > 500 {
					n = 500
				}
				p.PerPage = n
			}
		case "skipTotal":
			p.SkipTotal = val == "1" || val == "true"
		}
	}

	return p
}

// Builder builds and executes database queries for collection records.
type Builder struct {
	db          *database.DB
	collection  *collection.Collection
	params      Params
	whereClause string
	whereArgs   []any
	orderClause string
}

// NewBuilder creates a query builder for the given collection.
func NewBuilder(db *database.DB, coll *collection.Collection) *Builder {
	return &Builder{
		db:         db,
		collection: coll,
		params:     DefaultParams(),
	}
}

// WithParams sets the query parameters.
func (b *Builder) WithParams(p Params) *Builder {
	b.params = p
	b.params.Page = max(1, p.Page)
	b.params.PerPage = max(1, min(500, p.PerPage))
	return b
}

// WithAccessRule adds a filter to enforce collection-level access rules.
func (b *Builder) WithAccessRule(rule string) *Builder {
	if strings.TrimSpace(rule) == "" {
		return b
	}
	if b.whereClause == "" || b.whereClause == "TRUE" {
		b.whereClause = ""
	}

	expr, err := filter.ParseFilter(rule)
	if err != nil {
		return b
	}

	sql, args, err := filter.FilterToSQL(expr, nil)
	if err != nil {
		return b
	}

	if b.whereClause != "" {
		b.whereClause = fmt.Sprintf("(%s) AND (%s)", b.whereClause, sql)
	} else {
		b.whereClause = sql
	}
	b.whereArgs = append(b.whereArgs, args...)
	return b
}

// BuildWhere creates the WHERE clause from the filter parameter and access rules.
func (b *Builder) BuildWhere(ctx context.Context) error {
	// Apply client filter
	if strings.TrimSpace(b.params.Filter) != "" {
		expr, err := filter.ParseFilter(b.params.Filter)
		if err != nil {
			return fmt.Errorf("invalid filter: %w", err)
		}

		sql, args, err := filter.FilterToSQL(expr, nil)
		if err != nil {
			return fmt.Errorf("invalid filter: %w", err)
		}

		if b.whereClause != "" && b.whereClause != "TRUE" {
			b.whereClause = fmt.Sprintf("(%s) AND (%s)", b.whereClause, sql)
		} else {
			b.whereClause = sql
		}
		b.whereArgs = append(b.whereArgs, args...)
	}

	// Build sort
	b.buildSort()

	return nil
}

func (b *Builder) buildSort() {
	if b.params.Sort == "" {
		b.orderClause = `"created_at" DESC`
		return
	}

	// Parse sort parameter: +/-fieldname or fieldname,-fieldname2
	parts := strings.Split(b.params.Sort, ",")
	var orders []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		direction := "ASC"
		if part[0] == '-' {
			direction = "DESC"
			part = part[1:]
		} else if part[0] == '+' {
			part = part[1:]
		}

		// Validate field exists
		fieldName := part
		// Simple SQL injection prevention: only allow alphanumeric and underscore
		for _, ch := range fieldName {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return // invalid field, use default
			}
		}

		orders = append(orders, fmt.Sprintf(`"%s" %s`, fieldName, direction))
	}

	if len(orders) > 0 {
		b.orderClause = strings.Join(orders, ", ")
	}
}

// List executes the query and returns records with pagination.
func (b *Builder) List(ctx context.Context) ([]map[string]any, int, error) {
	if err := b.BuildWhere(ctx); err != nil {
		return nil, 0, err
	}

	tableName := b.collection.Name

	whereSQL := b.whereClause
	if whereSQL == "" {
		whereSQL = "TRUE"
	}

	// Count total (unless skipped)
	total := 0
	if !b.params.SkipTotal {
		countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s",
			QuoteIdent(tableName), whereSQL)
		if err := b.db.Pool.QueryRow(ctx, countSQL, b.whereArgs...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count query: %w", err)
		}
	}

	// Build SELECT
	offset := (b.params.Page - 1) * b.params.PerPage

	selectSQL := fmt.Sprintf(
		"SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE %s ORDER BY %s LIMIT %d OFFSET %d) t",
		QuoteIdent(tableName), whereSQL, b.orderClause, b.params.PerPage, offset,
	)

	rows, err := b.db.Pool.Query(ctx, selectSQL, b.whereArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list query: %w", err)
	}
	defer rows.Close()

	var records []map[string]any
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(raw, &record); err != nil {
			continue
		}
		records = append(records, record)
	}

	// Expand relations if requested
	if b.params.Expand != "" && len(records) > 0 {
		if err := b.expandRelations(ctx, records); err != nil {
			// Non-fatal - return records without expanded relations
		}
	}

	return records, total, nil
}

// Get fetches a single record by ID.
func (b *Builder) Get(ctx context.Context, id string) (map[string]any, error) {
	tableName := b.collection.Name
	selectSQL := fmt.Sprintf("SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE id = $1) t",
		QuoteIdent(tableName))

	var raw json.RawMessage
	err := b.db.Pool.QueryRow(ctx, selectSQL, id).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("record not found")
		}
		return nil, err
	}

	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}

	// Expand relations
	if b.params.Expand != "" {
		records := []map[string]any{record}
		if err := b.expandRelations(ctx, records); err == nil {
			record = records[0]
		}
	}

	return record, nil
}

// Create inserts a new record.
func (b *Builder) Create(ctx context.Context, record map[string]any) (map[string]any, error) {
	tableName := b.collection.Name

	// Build INSERT
	columns := []string{}
	placeholders := []string{}
	values := []any{}

	i := 1
	for key, val := range record {
		columns = append(columns, QuoteIdent(key))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		values = append(values, val)
		i++
	}

	sql := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) RETURNING row_to_json(%s.*)",
		QuoteIdent(tableName),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
		QuoteIdent(tableName),
	)

	var raw json.RawMessage
	if err := b.db.Pool.QueryRow(ctx, sql, values...).Scan(&raw); err != nil {
		return nil, fmt.Errorf("create record: %w", err)
	}

	var result map[string]any
	json.Unmarshal(raw, &result)
	return result, nil
}

// Update modifies an existing record.
func (b *Builder) Update(ctx context.Context, id string, record map[string]any) (map[string]any, error) {
	tableName := b.collection.Name

	if len(record) == 0 {
		return b.Get(ctx, id)
	}

	// Add updated_at if present in schema
	// Check if collection has updated_at field
	hasUpdatedAt := false
	for _, f := range b.collection.Schema {
		if f.Name == "updated_at" {
			hasUpdatedAt = true
			break
		}
	}

	setClauses := []string{}
	values := []any{id}
	i := 2

	for key, val := range record {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", QuoteIdent(key), i))
		values = append(values, val)
		i++
	}

	if hasUpdatedAt {
		setClauses = append(setClauses, fmt.Sprintf(`"updated_at" = NOW()`))
	}

	sql := fmt.Sprintf(
		"UPDATE %s SET %s WHERE id = $1 RETURNING row_to_json(%s.*)",
		QuoteIdent(tableName),
		strings.Join(setClauses, ", "),
		QuoteIdent(tableName),
	)

	var raw json.RawMessage
	if err := b.db.Pool.QueryRow(ctx, sql, values...).Scan(&raw); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("record not found")
		}
		return nil, fmt.Errorf("update record: %w", err)
	}

	var result map[string]any
	json.Unmarshal(raw, &result)
	return result, nil
}

// Delete removes a record.
func (b *Builder) Delete(ctx context.Context, id string) error {
	tableName := b.collection.Name
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = $1", QuoteIdent(tableName))
	result, err := b.db.Pool.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("record not found")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Relation Expansion
// ---------------------------------------------------------------------------

func (b *Builder) expandRelations(ctx context.Context, records []map[string]any) error {
	if b.params.Expand == "" {
		return nil
	}

	expandFields := strings.Split(b.params.Expand, ",")

	// Map of collection names to relation fields
	relFieldMap := make(map[string]string) // fieldName -> relatedCollectionName
	for _, sf := range b.collection.Schema {
		if sf.Type == collection.FieldRelation {
			if collName, ok := sf.Options["collection_id"].(string); ok {
				relFieldMap[sf.Name] = collName
			}
		}
	}

	for _, ef := range expandFields {
		ef = strings.TrimSpace(ef)
		if ef == "" {
			continue
		}

		relatedColl, ok := relFieldMap[ef]
		if !ok {
			continue
		}

		// For each record, fetch the related record(s)
		for _, record := range records {
			relValue, exists := record[ef]
			if !exists || relValue == nil {
				continue
			}

			// Handle both single and multiple relations
			var ids []string
			switch v := relValue.(type) {
			case string:
				if v != "" {
					ids = []string{v}
				}
			case []any:
				for _, item := range v {
					ids = append(ids, fmt.Sprintf("%v", item))
				}
			case []string:
				ids = v
			}

			if len(ids) == 0 {
				continue
			}

			// Fetch related records
			placeholders := make([]string, len(ids))
			args := make([]any, len(ids))
			for i, id := range ids {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
				args[i] = id
			}

			sql := fmt.Sprintf(
				"SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE id IN (%s)) t",
				QuoteIdent(relatedColl),
				strings.Join(placeholders, ", "),
			)

			rows, err := b.db.Pool.Query(ctx, sql, args...)
			if err != nil {
				continue
			}

			var relatedRecords []map[string]any
			for rows.Next() {
				var raw json.RawMessage
				if err := rows.Scan(&raw); err != nil {
					continue
				}
				var rec map[string]any
				json.Unmarshal(raw, &rec)
				relatedRecords = append(relatedRecords, rec)
			}
			rows.Close()

			// Attach to record under "expand" key
			if record["@expand"] == nil {
				record["@expand"] = make(map[string]any)
			}
			expandMap := record["@expand"].(map[string]any)

			// If it's a single relation (MaxSelect == 1), return single object
			sf := b.getSchemaField(ef)
			if sf != nil {
				if maxSelect, ok := sf.Options["max_select"]; ok {
					if ms, ok := maxSelect.(float64); ok && ms == 1 && len(relatedRecords) > 0 {
						expandMap[ef] = relatedRecords[0]
						continue
					}
				}
			}

			expandMap[ef] = relatedRecords
		}
	}

	return nil
}

func (b *Builder) getSchemaField(name string) *collection.SchemaField {
	for i, sf := range b.collection.Schema {
		if sf.Name == name {
			return &b.collection.Schema[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

// ListResponse is the standardized response for list operations.
type ListResponse struct {
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalItems int              `json:"totalItems"`
	TotalPages int              `json:"totalPages"`
	Items      []map[string]any `json:"items"`
}

// NewListResponse creates a paginated response.
func NewListResponse(items []map[string]any, total, page, perPage int) ListResponse {
	totalPages := 1
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	return ListResponse{
		Page:       page,
		PerPage:    perPage,
		TotalItems: total,
		TotalPages: totalPages,
		Items:      items,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
