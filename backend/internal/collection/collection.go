package collection

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/jackc/pgx/v5"
)

// CollectionType defines the type of collection.
type CollectionType string

const (
	TypeBase CollectionType = "base"
	TypeAuth CollectionType = "auth"
	TypeView CollectionType = "view"
)

// FieldType defines the type of a field.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldNumber   FieldType = "number"
	FieldBool     FieldType = "bool"
	FieldEmail    FieldType = "email"
	FieldURL      FieldType = "url"
	FieldDate     FieldType = "date"
	FieldSelect   FieldType = "select"
	FieldJSON     FieldType = "json"
	FieldFile     FieldType = "file"
	FieldRelation FieldType = "relation"
	FieldPassword FieldType = "password"
	FieldEditor   FieldType = "editor"
	FieldGeoPoint FieldType = "geo_point"
	FieldAutoDate FieldType = "autodate"
)

// SchemaField defines a single field in a collection schema.
type SchemaField struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     FieldType      `json:"type"`
	System   bool           `json:"system"`
	Required bool           `json:"required"`
	Unique   bool           `json:"unique"`
	Options  map[string]any `json:"options"`
}

// Collection represents a dynamic collection/table/view definition.
type Collection struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	Name       string         `json:"name"`
	Type       CollectionType `json:"type"`
	Schema     []SchemaField  `json:"schema"`
	ListRule   string         `json:"list_rule"`
	ViewRule   string         `json:"view_rule"`
	CreateRule string         `json:"create_rule"`
	UpdateRule string         `json:"update_rule"`
	DeleteRule string         `json:"delete_rule"`
	ViewQuery  string         `json:"view_query,omitempty"` // SQL query for view collections
	Indexes    []string       `json:"indexes"`
	System     bool           `json:"system"`
	Options    map[string]any `json:"options"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// Record represents a row in a dynamic collection.
type Record map[string]any

// Service manages collections and their underlying PostgreSQL tables/views.
type Service struct {
	db *database.DB
}

// NewService creates a new collection service.
func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// CreateCollection creates a new collection and its underlying table/view.
func (s *Service) CreateCollection(ctx context.Context, coll *Collection) error {
	if coll.ID == "" {
		coll.ID = uuid.New().String()
	}
	coll.CreatedAt = time.Now()
	coll.UpdatedAt = coll.CreatedAt

	switch coll.Type {
	case TypeView:
		if err := s.createView(ctx, coll); err != nil {
			return err
		}
	default:
		if err := s.createTable(ctx, coll); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return s.insertCollectionMeta(ctx, coll)
}

// insertCollectionMeta stores collection metadata.
func (s *Service) insertCollectionMeta(ctx context.Context, coll *Collection) error {
	schemaJSON, _ := json.Marshal(coll.Schema)
	indexesJSON, _ := json.Marshal(coll.Indexes)
	optionsJSON, _ := json.Marshal(coll.Options)

	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO _collections (id, tenant_id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, view_query, indexes, options, system)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		coll.ID, coll.TenantID, coll.Name, coll.Type, schemaJSON,
		coll.ListRule, coll.ViewRule, coll.CreateRule, coll.UpdateRule, coll.DeleteRule,
		coll.ViewQuery, indexesJSON, optionsJSON, coll.System,
	)
	if err != nil {
		s.dropTable(ctx, coll.Name)
		return fmt.Errorf("failed to insert collection metadata: %w", err)
	}

	return nil
}

// UpdateCollection updates a collection and syncs its table schema.
func (s *Service) UpdateCollection(ctx context.Context, coll *Collection) error {
	coll.UpdatedAt = time.Now()

	old, err := s.GetCollection(ctx, coll.ID)
	if err != nil {
		return err
	}

	// Handle type-specific changes
	switch coll.Type {
	case TypeView:
		if old.Type == TypeView {
			if err := s.updateView(ctx, coll); err != nil {
				return err
			}
		} else {
			s.dropTable(ctx, old.Name)
			if err := s.createView(ctx, coll); err != nil {
				return err
			}
		}
	default:
		if old.Type == TypeView {
			s.dropView(ctx, old.Name)
			if err := s.createTable(ctx, coll); err != nil {
				return err
			}
		} else {
			if err := s.syncTable(ctx, old, coll); err != nil {
				return fmt.Errorf("failed to sync table schema: %w", err)
			}
		}
	}

	schemaJSON, _ := json.Marshal(coll.Schema)
	indexesJSON, _ := json.Marshal(coll.Indexes)
	optionsJSON, _ := json.Marshal(coll.Options)

	_, err = s.db.Pool.Exec(ctx, `
		UPDATE _collections SET
			name = $2, type = $3, schema = $4, list_rule = $5, view_rule = $6,
			create_rule = $7, update_rule = $8, delete_rule = $9, view_query = $10,
			indexes = $11, options = $12, updated_at = $13
		WHERE id = $1`,
		coll.ID, coll.Name, coll.Type, schemaJSON,
		coll.ListRule, coll.ViewRule, coll.CreateRule, coll.UpdateRule, coll.DeleteRule,
		coll.ViewQuery, indexesJSON, optionsJSON, coll.UpdatedAt,
	)
	return err
}

// DeleteCollection deletes a collection and its underlying table/view.
func (s *Service) DeleteCollection(ctx context.Context, id string) error {
	coll, err := s.GetCollection(ctx, id)
	if err != nil {
		return err
	}

	switch coll.Type {
	case TypeView:
		s.dropView(ctx, coll.Name)
	default:
		s.dropTable(ctx, coll.Name)
	}

	_, err = s.db.Pool.Exec(ctx, "DELETE FROM _collections WHERE id = $1", id)
	return err
}

// GetCollection retrieves a collection by ID.
func (s *Service) GetCollection(ctx context.Context, id string) (*Collection, error) {
	return s.scanCollection(ctx,
		`SELECT id, tenant_id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, COALESCE(view_query,''),
			indexes, options, system, created_at, updated_at
		FROM _collections WHERE id = $1`, id)
}

// GetCollectionByName retrieves a collection by name.
func (s *Service) GetCollectionByName(ctx context.Context, name string) (*Collection, error) {
	return s.scanCollection(ctx,
		`SELECT id, tenant_id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, COALESCE(view_query,''),
			indexes, options, system, created_at, updated_at
		FROM _collections WHERE name = $1`, name)
}

// ListCollections lists all collections for a tenant.
func (s *Service) ListCollections(ctx context.Context, tenantID string) ([]*Collection, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, tenant_id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, COALESCE(view_query,''),
			indexes, options, system, created_at, updated_at
		FROM _collections WHERE tenant_id = $1
		ORDER BY system DESC, created_at ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []*Collection
	for rows.Next() {
		coll := s.scanCollectionRow(rows)
		if coll != nil {
			collections = append(collections, coll)
		}
	}
	return collections, nil
}

func (s *Service) scanCollection(ctx context.Context, sql string, args ...any) (*Collection, error) {
	row := s.db.Pool.QueryRow(ctx, sql, args...)
	coll := &Collection{}
	var schemaJSON, indexesJSON, optionsJSON []byte
	err := row.Scan(
		&coll.ID, &coll.TenantID, &coll.Name, &coll.Type,
		&schemaJSON, &coll.ListRule, &coll.ViewRule, &coll.CreateRule,
		&coll.UpdateRule, &coll.DeleteRule, &coll.ViewQuery,
		&indexesJSON, &coll.Options, &coll.System,
		&coll.CreatedAt, &coll.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(schemaJSON, &coll.Schema)
	json.Unmarshal(indexesJSON, &coll.Indexes)
	json.Unmarshal(optionsJSON, &coll.Options)
	return coll, nil
}

func (s *Service) scanCollectionRow(rows pgx.Rows) *Collection {
	coll := &Collection{}
	var schemaJSON, indexesJSON, optionsJSON []byte
	if err := rows.Scan(
		&coll.ID, &coll.TenantID, &coll.Name, &coll.Type,
		&schemaJSON, &coll.ListRule, &coll.ViewRule, &coll.CreateRule,
		&coll.UpdateRule, &coll.DeleteRule, &coll.ViewQuery,
		&indexesJSON, &coll.Options, &coll.System,
		&coll.CreatedAt, &coll.UpdatedAt,
	); err != nil {
		return nil
	}
	json.Unmarshal(schemaJSON, &coll.Schema)
	json.Unmarshal(indexesJSON, &coll.Indexes)
	json.Unmarshal(optionsJSON, &coll.Options)
	return coll
}

// ---------------------------------------------------------------------------
// View collections
// ---------------------------------------------------------------------------

func (s *Service) createView(ctx context.Context, coll *Collection) error {
	if coll.ViewQuery == "" {
		return fmt.Errorf("view collection %q requires a view_query", coll.Name)
	}
	sql := fmt.Sprintf("CREATE VIEW %s AS %s", s.quoteIdent(coll.Name), coll.ViewQuery)
	return s.db.Exec(ctx, sql)
}

func (s *Service) updateView(ctx context.Context, coll *Collection) error {
	if err := s.dropView(ctx, coll.Name); err != nil {
		return err
	}
	return s.createView(ctx, coll)
}

func (s *Service) dropView(ctx context.Context, name string) error {
	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s CASCADE", s.quoteIdent(name))
	return s.db.Exec(ctx, sql)
}

// ---------------------------------------------------------------------------
// Table operations
// ---------------------------------------------------------------------------

func (s *Service) createTable(ctx context.Context, coll *Collection) error {
	columns := s.buildColumnDefs(coll.Schema)
	sql := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", s.quoteIdent(coll.Name), strings.Join(columns, ",\n  "))
	return s.db.Exec(ctx, sql)
}

func (s *Service) dropTable(ctx context.Context, name string) error {
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", s.quoteIdent(name))
	return s.db.Exec(ctx, sql)
}

func (s *Service) syncTable(ctx context.Context, old, new *Collection) error {
	if old.Name != new.Name {
		sql := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", s.quoteIdent(old.Name), s.quoteIdent(new.Name))
		if err := s.db.Exec(ctx, sql); err != nil {
			return err
		}
	}

	oldFields := make(map[string]SchemaField)
	for _, f := range old.Schema {
		oldFields[f.Name] = f
	}

	for _, newField := range new.Schema {
		oldField, exists := oldFields[newField.Name]
		if !exists {
			if err := s.addColumn(ctx, new.Name, newField); err != nil {
				return err
			}
		} else if !fieldsEqual(oldField, newField) {
			if err := s.alterColumn(ctx, new.Name, newField); err != nil {
				return err
			}
		}
	}

	for _, oldField := range old.Schema {
		found := false
		for _, newField := range new.Schema {
			if oldField.Name == newField.Name {
				found = true
				break
			}
		}
		if !found {
			if err := s.dropColumn(ctx, new.Name, oldField.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) addColumn(ctx context.Context, tableName string, field SchemaField) error {
	colDef := s.fieldToColumnDef(field)
	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", s.quoteIdent(tableName), colDef)
	return s.db.Exec(ctx, sql)
}

func (s *Service) alterColumn(ctx context.Context, tableName string, field SchemaField) error {
	sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
		s.quoteIdent(tableName), s.quoteIdent(field.Name),
		s.fieldToPGType(field.Type), s.quoteIdent(field.Name), s.fieldToPGType(field.Type))
	return s.db.Exec(ctx, sql)
}

func (s *Service) dropColumn(ctx context.Context, tableName, colName string) error {
	sql := fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s CASCADE",
		s.quoteIdent(tableName), s.quoteIdent(colName))
	return s.db.Exec(ctx, sql)
}

// ---------------------------------------------------------------------------
// Record CRUD with filter engine integration
// ---------------------------------------------------------------------------

// CreateRecord inserts a new record.
func (s *Service) CreateRecord(ctx context.Context, coll *Collection, data map[string]any) (Record, error) {
	recordID, ok := data["id"].(string)
	if !ok || recordID == "" {
		recordID = uuid.New().String()
		data["id"] = recordID
	}

	columns := []string{}
	values := []any{}
	placeholders := []string{}
	i := 0

	for k, v := range data {
		if k == "created_at" || k == "updated_at" {
			continue
		}
		i++
		columns = append(columns, s.quoteIdent(k))
		values = append(values, v)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
	}

	i++
	columns = append(columns, "created_at", "updated_at")
	placeholders = append(placeholders, fmt.Sprintf("$%d", i), fmt.Sprintf("$%d", i+1))
	values = append(values, time.Now(), time.Now())

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
		s.quoteIdent(coll.Name), strings.Join(columns, ", "), strings.Join(placeholders, ", "))

	rows, err := s.db.Pool.Query(ctx, sql, values...)
	if err != nil {
		return nil, fmt.Errorf("insert failed: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("no rows returned from insert")
	}

	rValues, err := rows.Values()
	if err != nil {
		return data, nil
	}

	fieldDescs := rows.FieldDescriptions()
	record := make(Record)
	for i, fd := range fieldDescs {
		record[string(fd.Name)] = rValues[i]
	}
	return record, nil
}

// CreateRecords inserts multiple records in a transaction.
func (s *Service) CreateRecords(ctx context.Context, coll *Collection, records []map[string]any) ([]Record, error) {
	var results []Record
	err := s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		for _, data := range records {
			recordID, ok := data["id"].(string)
			if !ok || recordID == "" {
				recordID = uuid.New().String()
				data["id"] = recordID
			}

			columns := []string{}
			values := []any{}
			placeholders := []string{}
			i := 0
			for k, v := range data {
				if k == "created_at" || k == "updated_at" {
					continue
				}
				i++
				columns = append(columns, s.quoteIdent(k))
				values = append(values, v)
				placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			}
			i++
			columns = append(columns, "created_at", "updated_at")
			placeholders = append(placeholders, fmt.Sprintf("$%d", i), fmt.Sprintf("$%d", i+1))
			values = append(values, time.Now(), time.Now())

			sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
				s.quoteIdent(coll.Name), strings.Join(columns, ", "), strings.Join(placeholders, ", "))

			rows, err := tx.Query(ctx, sql, values...)
			if err != nil {
				return fmt.Errorf("batch insert failed: %w", err)
			}

			if rows.Next() {
				rValues, _ := rows.Values()
				fieldDescs := rows.FieldDescriptions()
				record := make(Record)
				for i, fd := range fieldDescs {
					record[string(fd.Name)] = rValues[i]
				}
				results = append(results, record)
			}
			rows.Close()
		}
		return nil
	})
	return results, err
}

// ListRecords queries records with filter, sort, and pagination using the filter engine.
func (s *Service) ListRecords(ctx context.Context, coll *Collection, filterStr, sort string, page, perPage int) ([]Record, int64, error) {
	if perPage <= 0 {
		perPage = 30
	}
	if perPage > 200 {
		perPage = 200
	}
	if page <= 0 {
		page = 1
	}

	// Build WHERE clause using filter engine
	whereClause := "TRUE"
	var filterArgs []any
	if filterStr != "" {
		expr, err := filter.ParseFilter(filterStr)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid filter: %w", err)
		}
		sql, vals, err := filter.FilterToSQL(expr, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid filter: %w", err)
		}
		whereClause = sql
		filterArgs = vals
	}

	offset := (page - 1) * perPage

	// For view collections, we can't use WHERE on the view query itself, wrap it
	source := s.quoteIdent(coll.Name)

	// Count
	var total int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", source, whereClause)
	err := s.db.Pool.QueryRow(ctx, countSQL, filterArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Query records
	querySQL := fmt.Sprintf("SELECT * FROM %s WHERE %s", source, whereClause)
	if sort != "" {
		querySQL += " ORDER BY " + sort
	} else {
		querySQL += " ORDER BY created_at DESC"
	}
	querySQL += fmt.Sprintf(" LIMIT %d OFFSET %d", perPage, offset)

	rows, err := s.db.Pool.Query(ctx, querySQL, filterArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	records := []Record{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, 0, err
		}
		record := make(Record)
		for i, fd := range fieldDescs {
			record[string(fd.Name)] = values[i]
		}
		records = append(records, record)
	}

	return records, total, nil
}

// GetRecord fetches a single record by ID.
func (s *Service) GetRecord(ctx context.Context, coll *Collection, recordID string) (Record, error) {
	sql := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", s.quoteIdent(coll.Name))
	rows, err := s.db.Pool.Query(ctx, sql, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("record not found: %s", recordID)
	}

	values, err := rows.Values()
	if err != nil {
		return nil, err
	}

	fieldDescs := rows.FieldDescriptions()
	record := make(Record)
	for i, fd := range fieldDescs {
		record[string(fd.Name)] = values[i]
	}
	return record, nil
}

// UpdateRecord updates an existing record.
func (s *Service) UpdateRecord(ctx context.Context, coll *Collection, recordID string, data map[string]any) error {
	delete(data, "id")
	delete(data, "created_at")
	data["updated_at"] = time.Now()

	sets := []string{}
	values := []any{recordID}
	i := 1
	for k, v := range data {
		i++
		sets = append(sets, fmt.Sprintf("%s = $%d", s.quoteIdent(k), i))
		values = append(values, v)
	}

	sql := fmt.Sprintf("UPDATE %s SET %s WHERE id = $1",
		s.quoteIdent(coll.Name), strings.Join(sets, ", "))
	return s.db.Exec(ctx, sql, values...)
}

// DeleteRecord deletes a record by ID.
func (s *Service) DeleteRecord(ctx context.Context, coll *Collection, recordID string) error {
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.quoteIdent(coll.Name))
	return s.db.Exec(ctx, sql, recordID)
}

// CreateBatch creates multiple records atomically in a transaction.
func (s *Service) CreateBatch(ctx context.Context, coll *Collection, records []map[string]any) ([]Record, error) {
	return s.CreateRecords(ctx, coll, records)
}

// UpdateBatch updates multiple records atomically.
func (s *Service) UpdateBatch(ctx context.Context, coll *Collection, updates map[string]map[string]any) error {
	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		for id, data := range updates {
			delete(data, "id")
			delete(data, "created_at")
			data["updated_at"] = time.Now()
			sets := []string{}
			values := []any{id}
			i := 1
			for k, v := range data {
				i++
				sets = append(sets, fmt.Sprintf("%s = $%d", s.quoteIdent(k), i))
				values = append(values, v)
			}
			sql := fmt.Sprintf("UPDATE %s SET %s WHERE id = $1",
				s.quoteIdent(coll.Name), strings.Join(sets, ", "))
			if err := s.db.Exec(ctx, sql, values...); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteBatch deletes multiple records atomically.
func (s *Service) DeleteBatch(ctx context.Context, coll *Collection, ids []string) error {
	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		for _, id := range ids {
			sql := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.quoteIdent(coll.Name))
			if err := s.db.Exec(ctx, sql, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Relation expansion
// ---------------------------------------------------------------------------

func (s *Service) ExpandRecords(ctx context.Context, coll *Collection, records []Record, expand string) {
	expandFields := strings.Split(expand, ",")
	for _, fieldName := range expandFields {
		fieldName = strings.TrimSpace(fieldName)
		for _, field := range coll.Schema {
			if field.Name == fieldName && field.Type == FieldRelation {
				s.expandRelationField(ctx, records, field)
			}
		}
	}
}

func (s *Service) ExpandRecord(ctx context.Context, coll *Collection, record Record, expand string) {
	s.ExpandRecords(ctx, coll, []Record{record}, expand)
}

func (s *Service) expandRelationField(ctx context.Context, records []Record, field SchemaField) {
	relatedCollection, _ := field.Options["collection"].(string)
	if relatedCollection == "" {
		return
	}

	ids := make(map[string]bool)
	for _, record := range records {
		switch v := record[field.Name].(type) {
		case string:
			if v != "" {
				ids[v] = true
			}
		case []any:
			for _, id := range v {
				if s, ok := id.(string); ok && s != "" {
					ids[s] = true
				}
			}
		}
	}

	if len(ids) == 0 {
		return
	}

	idList := make([]string, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}

	placeholders := make([]string, len(idList))
	args := make([]any, len(idList))
	for i, id := range idList {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	sql := fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)",
		s.quoteIdent(relatedCollection), strings.Join(placeholders, ","))

	rows, err := s.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	relatedMap := make(map[string]Record)
	for rows.Next() {
		values, _ := rows.Values()
		record := make(Record)
		for i, fd := range fieldDescs {
			record[string(fd.Name)] = values[i]
		}
		if id, ok := record["id"].(string); ok {
			relatedMap[id] = record
		}
	}

	for _, record := range records {
		if _, ok := record["expand"]; !ok {
			record["expand"] = make(map[string]any)
		}
		expandMap := record["expand"].(map[string]any)

		switch v := record[field.Name].(type) {
		case string:
			if r, ok := relatedMap[v]; ok {
				expandMap[field.Name] = r
			}
		case []any:
			var expanded []Record
			for _, id := range v {
				if s, ok := id.(string); ok {
					if r, ok := relatedMap[s]; ok {
						expanded = append(expanded, r)
					}
				}
			}
			expandMap[field.Name] = expanded
		}
	}
}

// ---------------------------------------------------------------------------
// Column builders
// ---------------------------------------------------------------------------

func (s *Service) buildColumnDefs(fields []SchemaField) []string {
	columns := []string{"id TEXT PRIMARY KEY DEFAULT gen_random_uuid()"}
	for _, f := range fields {
		if f.Name == "id" {
			continue
		}
		columns = append(columns, s.fieldToColumnDef(f))
	}
	columns = append(columns,
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
	)
	return columns
}

func (s *Service) fieldToColumnDef(field SchemaField) string {
	pgType := s.fieldToPGType(field.Type)
	def := fmt.Sprintf("%s %s", s.quoteIdent(field.Name), pgType)
	if field.Required {
		def += " NOT NULL"
	}
	if defVal, ok := field.Options["default"]; ok {
		def += fmt.Sprintf(" DEFAULT %v", defVal)
	}
	return def
}

func (s *Service) fieldToPGType(ft FieldType) string {
	switch ft {
	case FieldText, FieldEmail, FieldURL, FieldSelect, FieldPassword, FieldEditor, FieldRelation:
		return "TEXT"
	case FieldNumber:
		return "DOUBLE PRECISION"
	case FieldBool:
		return "BOOLEAN"
	case FieldDate, FieldAutoDate:
		return "TIMESTAMPTZ"
	case FieldJSON, FieldFile, FieldGeoPoint:
		return "JSONB"
	default:
		return "TEXT"
	}
}

func fieldsEqual(a, b SchemaField) bool {
	return a.Type == b.Type && a.Required == b.Required && a.Unique == b.Unique
}

func (s *Service) quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Exports for testing
func (s *Service) QuoteIdentExport(name string) string           { return s.quoteIdent(name) }
func (s *Service) FieldToPGTypeExport(ft FieldType) string        { return s.fieldToPGType(ft) }
func (s *Service) BuildColumnDefsExport(fields []SchemaField) []string { return s.buildColumnDefs(fields) }
func FieldsEqualExport(a, b SchemaField) bool                     { return fieldsEqual(a, b) }

// GetAuthMethods returns the available authentication methods for an auth collection.
func (s *Service) GetAuthMethods(coll *Collection) map[string]bool {
	methods := map[string]bool{
		"password": false,
		"otp":      false,
		"oauth2":   false,
	}

	for _, field := range coll.Schema {
		switch field.Type {
		case FieldPassword:
			methods["password"] = true
		case FieldEmail:
			methods["otp"] = true
		}
	}

	// OAuth2 is available if configured globally
	if coll.Options != nil {
		if oauthProviders, ok := coll.Options["oauthProviders"]; ok {
			if providers, ok := oauthProviders.([]any); ok && len(providers) > 0 {
				methods["oauth2"] = true
			}
		}
	}

	return methods
}
