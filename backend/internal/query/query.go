// Package query provides a query builder for record listing with filtering,
// sorting, pagination, field expansion, and relation expansion.
package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// Params represents query parameters from the HTTP request.
type Params struct {
	Filter    string `json:"filter"`
	Sort      string `json:"sort"`
	Expand    string `json:"expand"`
	Fields    string `json:"fields"`
	Page      int    `json:"page"`
	PerPage   int    `json:"perPage"`
	SkipTotal bool   `json:"skipTotal"`
}

// DefaultParams creates query params with defaults.
func DefaultParams() Params {
	return Params{
		Page:    1,
		PerPage: 30,
	}
}

// ParseParams extracts and URL-decodes query params from a raw query string.
func ParseParams(rawQuery string) Params {
	p := DefaultParams()

	if rawQuery == "" {
		return p
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return p
	}

	if val := values.Get("filter"); val != "" {
		p.Filter = val
	}
	if val := values.Get("sort"); val != "" {
		p.Sort = val
	}
	if val := values.Get("expand"); val != "" {
		p.Expand = val
	}
	if val := values.Get("fields"); val != "" {
		p.Fields = val
	}
	if val := values.Get("page"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			p.Page = n
		}
	}
	if val := values.Get("perPage"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			p.PerPage = n
		}
	}
	if val := values.Get("skipTotal"); val != "" {
		p.SkipTotal = val == "1" || strings.EqualFold(val, "true")
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

	sql, args, err := filter.FilterToSQLOffset(expr, nil, len(b.whereArgs)+1)
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

		sql, args, err := filter.FilterToSQLOffset(expr, nil, len(b.whereArgs)+1)
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

	// Count total (unless skipped). Listing is replica-safe: it never feeds
	// a write decision, so it may route to a configured read replica.
	total := 0
	if !b.params.SkipTotal {
		countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s",
			QuoteIdent(tableName), whereSQL)
		if err := b.db.ReadQueryRow(ctx, countSQL, b.whereArgs...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count query: %w", err)
		}
	}

	// Build SELECT
	offset := (b.params.Page - 1) * b.params.PerPage

	selectSQL := fmt.Sprintf(
		"SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE %s ORDER BY %s LIMIT %d OFFSET %d) t",
		QuoteIdent(tableName), whereSQL, b.orderClause, b.params.PerPage, offset,
	)

	rows, err := b.db.ReadQuery(ctx, selectSQL, b.whereArgs...)
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
			// Non-fatal: return records without expanded relations.
			log.Warn().Err(err).Msg("query: relation expansion failed; returning unexpanded records")
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
	err := b.db.QueryRow(ctx, selectSQL, id).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
	if err := b.db.QueryRow(ctx, sql, values...).Scan(&raw); err != nil {
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
	if err := b.db.QueryRow(ctx, sql, values...).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
	result, err := b.db.ExecResult(ctx, sql, id)
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

	for _, fieldName := range strings.Split(b.params.Expand, ",") {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}

		schemaField := b.getSchemaField(fieldName)
		if schemaField == nil || schemaField.Type != collection.FieldRelation {
			continue
		}

		relatedCollectionName, err := b.resolveRelatedCollectionName(ctx, schemaField)
		if err != nil || relatedCollectionName == "" {
			continue
		}

		idSet := make(map[string]struct{})
		for _, record := range records {
			for _, id := range relationIDs(record[fieldName]) {
				idSet[id] = struct{}{}
			}
		}
		if len(idSet) == 0 {
			continue
		}

		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}

		placeholders := make([]string, len(ids))
		args := make([]any, len(ids))
		for i, id := range ids {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = id
		}

		rows, err := b.db.Query(ctx,
			fmt.Sprintf("SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE id IN (%s)) t", QuoteIdent(relatedCollectionName), strings.Join(placeholders, ", ")),
			args...,
		)
		if err != nil {
			continue
		}

		relatedMap := make(map[string]map[string]any, len(ids))
		for rows.Next() {
			var raw json.RawMessage
			if err := rows.Scan(&raw); err != nil {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal(raw, &rec); err != nil {
				continue
			}
			id := strings.TrimSpace(fmt.Sprint(rec["id"]))
			if id == "" {
				continue
			}
			rec["_label"] = queryRelationLabel(rec, schemaField)
			relatedMap[id] = rec
		}
		rows.Close()

		single := relationMaxSelect(schemaField) <= 1
		for _, record := range records {
			if record["expand"] == nil {
				record["expand"] = make(map[string]any)
			}
			expandMap, _ := record["expand"].(map[string]any)
			if expandMap == nil {
				expandMap = make(map[string]any)
				record["expand"] = expandMap
			}

			ids := relationIDs(record[fieldName])
			if single {
				if len(ids) > 0 {
					expandMap[fieldName] = relatedMap[ids[0]]
				}
				continue
			}

			expanded := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				if rec, ok := relatedMap[id]; ok {
					expanded = append(expanded, rec)
				}
			}
			expandMap[fieldName] = expanded
		}
	}

	return nil
}

func (b *Builder) resolveRelatedCollectionName(ctx context.Context, field *collection.SchemaField) (string, error) {
	if field == nil {
		return "", fmt.Errorf("missing relation field")
	}
	if field.Options != nil {
		if name, _ := field.Options["collection_name"].(string); name != "" {
			return name, nil
		}
		if name, _ := field.Options["collection"].(string); name != "" {
			return name, nil
		}
		if raw, _ := field.Options["collection_id"].(string); raw != "" {
			var name string
			if err := b.db.QueryRow(ctx, `SELECT name FROM _collections WHERE id = $1 OR name = $1 LIMIT 1`, raw).Scan(&name); err == nil && name != "" {
				field.Options["collection_name"] = name
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("relation target not found")
}

// RelationIDs extracts relation id values from a stored record value
// (single TEXT id, JSONB array, or string slice).
func RelationIDs(value any) []string { return relationIDs(value) }

// RelationLabel computes the display label for an expanded relation record.
func RelationLabel(record map[string]any, field *collection.SchemaField) string {
	return queryRelationLabel(record, field)
}

// RelationMaxSelect reports the max_select option of a relation field (1 = single).
func RelationMaxSelect(field *collection.SchemaField) int { return relationMaxSelect(field) }

func relationIDs(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []string:
		result := make([]string, 0, len(v))
		for _, id := range v {
			if strings.TrimSpace(id) != "" {
				result = append(result, id)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if id := strings.TrimSpace(fmt.Sprint(item)); id != "" {
				result = append(result, id)
			}
		}
		return result
	default:
		return nil
	}
}

func relationMaxSelect(field *collection.SchemaField) int {
	if field == nil || field.Options == nil {
		return 1
	}
	switch v := field.Options["max_select"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 1
	}
}

func queryRelationLabel(record map[string]any, field *collection.SchemaField) string {
	if field == nil {
		return strings.TrimSpace(fmt.Sprint(record["id"]))
	}
	displayFields := []string{}
	if field.Options != nil {
		switch raw := field.Options["display_fields"].(type) {
		case []string:
			displayFields = raw
		case []any:
			for _, item := range raw {
				value := strings.TrimSpace(fmt.Sprint(item))
				if value != "" {
					displayFields = append(displayFields, value)
				}
			}
		case string:
			for _, item := range strings.Split(raw, ",") {
				item = strings.TrimSpace(item)
				if item != "" {
					displayFields = append(displayFields, item)
				}
			}
		}
	}
	if len(displayFields) == 0 {
		for _, candidate := range []string{"title", "name", "label", "email", "username"} {
			if value := strings.TrimSpace(fmt.Sprint(record[candidate])); value != "" && value != "<nil>" {
				return value
			}
		}
		return strings.TrimSpace(fmt.Sprint(record["id"]))
	}
	parts := make([]string, 0, len(displayFields))
	for _, key := range displayFields {
		if value := strings.TrimSpace(fmt.Sprint(record[key])); value != "" && value != "<nil>" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(fmt.Sprint(record["id"]))
	}
	return strings.Join(parts, " · ")
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
