package collection

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	collectionfields "github.com/gresbase/gresbase/internal/collection/fields"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	FieldVector   FieldType = "vector"
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
//
// Access rules follow locked-by-default semantics:
//   - nil   → locked: only superusers (admins) may perform the operation
//   - ""    → public: anyone may perform the operation
//   - "..." → filter expression evaluated against the request and record
type Collection struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Type       CollectionType `json:"type"`
	Schema     []SchemaField  `json:"schema"`
	ListRule   *string        `json:"list_rule"`
	ViewRule   *string        `json:"view_rule"`
	CreateRule *string        `json:"create_rule"`
	UpdateRule *string        `json:"update_rule"`
	DeleteRule *string        `json:"delete_rule"`
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

type dbRunner interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Service) runnerForContext(ctx context.Context) dbRunner {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx
	}
	return s.db.Pool
}

// NewService creates a new collection service.
func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// CreateCollection creates a new collection and its underlying table/view.
func (s *Service) CreateCollection(ctx context.Context, coll *Collection) error {
	if coll == nil {
		return fmt.Errorf("collection is required")
	}
	if err := s.ValidateCollectionDefinition(coll); err != nil {
		return err
	}
	if coll.ID == "" {
		coll.ID = uuid.New().String()
	}
	coll.CreatedAt = time.Now()
	coll.UpdatedAt = coll.CreatedAt

	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		switch coll.Type {
		case TypeView:
			if err := s.createViewWith(ctx, tx, coll); err != nil {
				return err
			}
		default:
			if len(vectorFields(coll)) > 0 {
				if err := s.ensureVectorExtension(ctx, tx); err != nil {
					return err
				}
			}
			if err := s.createTableWith(ctx, tx, coll); err != nil {
				return fmt.Errorf("failed to create table: %w", err)
			}
			if err := s.ensureSystemColumnsWith(ctx, tx, coll); err != nil {
				return err
			}
			s.ensureVectorIndexes(ctx, tx, coll)
			s.applyUserIndexes(ctx, tx, coll)
		}

		return s.insertCollectionMetaWith(ctx, tx, coll)
	})
}

// insertCollectionMetaWith stores collection metadata.
func (s *Service) insertCollectionMetaWith(ctx context.Context, runner dbRunner, coll *Collection) error {
	schemaJSON, _ := json.Marshal(coll.Schema)
	indexesJSON, _ := json.Marshal(coll.Indexes)
	optionsJSON, _ := json.Marshal(coll.Options)

	_, err := runner.Exec(ctx, `
		INSERT INTO _collections (id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, view_query, indexes, options, system)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		coll.ID, coll.Name, coll.Type, schemaJSON,
		coll.ListRule, coll.ViewRule, coll.CreateRule, coll.UpdateRule, coll.DeleteRule,
		coll.ViewQuery, indexesJSON, optionsJSON, coll.System,
	)
	if err != nil {
		return fmt.Errorf("failed to insert collection metadata: %w", err)
	}

	return nil
}

// UpdateCollection updates a collection and syncs its table schema.
func (s *Service) UpdateCollection(ctx context.Context, coll *Collection) error {
	if coll == nil {
		return fmt.Errorf("collection is required")
	}
	if err := s.ValidateCollectionDefinition(coll); err != nil {
		return err
	}
	coll.UpdatedAt = time.Now()

	old, err := s.GetCollection(ctx, coll.ID)
	if err != nil {
		return err
	}

	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		// Handle type-specific changes
		switch coll.Type {
		case TypeView:
			if old.Type == TypeView {
				if err := s.updateViewWith(ctx, tx, coll); err != nil {
					return err
				}
			} else {
				if err := s.dropTableWith(ctx, tx, old.Name); err != nil {
					return err
				}
				if err := s.createViewWith(ctx, tx, coll); err != nil {
					return err
				}
			}
		default:
			if len(vectorFields(coll)) > 0 {
				if err := s.ensureVectorExtension(ctx, tx); err != nil {
					return err
				}
			}
			if old.Type == TypeView {
				if err := s.dropViewWith(ctx, tx, old.Name); err != nil {
					return err
				}
				if err := s.createTableWith(ctx, tx, coll); err != nil {
					return err
				}
			} else {
				if err := s.syncTableWith(ctx, tx, old, coll); err != nil {
					return fmt.Errorf("failed to sync table schema: %w", err)
				}
			}
			if err := s.ensureSystemColumnsWith(ctx, tx, coll); err != nil {
				return err
			}
			s.ensureVectorIndexes(ctx, tx, coll)
			s.applyUserIndexes(ctx, tx, coll)
		}

		schemaJSON, _ := json.Marshal(coll.Schema)
		indexesJSON, _ := json.Marshal(coll.Indexes)
		optionsJSON, _ := json.Marshal(coll.Options)

		_, err = tx.Exec(ctx, `
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
	})
}

// DeleteCollection deletes a collection and its underlying table/view.
func (s *Service) DeleteCollection(ctx context.Context, id string) error {
	coll, err := s.GetCollection(ctx, id)
	if err != nil {
		return err
	}

	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		switch coll.Type {
		case TypeView:
			if err := s.dropViewWith(ctx, tx, coll.Name); err != nil {
				return err
			}
		default:
			if err := s.dropTableWith(ctx, tx, coll.Name); err != nil {
				return err
			}
		}

		_, err = tx.Exec(ctx, "DELETE FROM _collections WHERE id = $1", id)
		return err
	})
}

// GetCollection retrieves a collection by ID.
func (s *Service) GetCollection(ctx context.Context, id string) (*Collection, error) {
	return s.scanCollection(ctx,
		`SELECT id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, COALESCE(view_query,''),
			indexes, options, system, created_at, updated_at
		FROM _collections WHERE id = $1`, id)
}

// GetCollectionByName retrieves a collection by name.
func (s *Service) GetCollectionByName(ctx context.Context, name string) (*Collection, error) {
	return s.scanCollection(ctx,
		`SELECT id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, COALESCE(view_query,''),
			indexes, options, system, created_at, updated_at
		FROM _collections WHERE name = $1`, name)
}

// ListCollections lists all collections.
func (s *Service) ListCollections(ctx context.Context) ([]*Collection, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, type, schema, list_rule, view_rule,
			create_rule, update_rule, delete_rule, COALESCE(view_query,''),
			indexes, options, system, created_at, updated_at
		FROM _collections
		ORDER BY system DESC, created_at ASC`)
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
	s.hydrateCollectionsMetadata(collections)
	return collections, nil
}

func (s *Service) scanCollection(ctx context.Context, sql string, args ...any) (*Collection, error) {
	row := s.db.QueryRow(ctx, sql, args...)
	coll := &Collection{}
	var schemaJSON, indexesJSON, optionsJSON []byte
	err := row.Scan(
		&coll.ID, &coll.Name, &coll.Type,
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
	s.hydrateCollectionMetadata(ctx, coll)
	return coll, nil
}

func (s *Service) scanCollectionRow(rows pgx.Rows) *Collection {
	coll := &Collection{}
	var schemaJSON, indexesJSON, optionsJSON []byte
	if err := rows.Scan(
		&coll.ID, &coll.Name, &coll.Type,
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

func (s *Service) hydrateCollectionsMetadata(collections []*Collection) {
	if len(collections) == 0 {
		return
	}

	lookup := make(map[string]string, len(collections)*2)
	for _, coll := range collections {
		lookup[coll.ID] = coll.Name
		lookup[coll.Name] = coll.Name
	}

	for _, coll := range collections {
		for i := range coll.Schema {
			field := &coll.Schema[i]
			if field.Options == nil {
				field.Options = map[string]any{}
			}
			if field.Type != FieldRelation {
				continue
			}
			if name, _ := field.Options["collection_name"].(string); name != "" {
				continue
			}
			if name, _ := field.Options["collection"].(string); name != "" {
				field.Options["collection_name"] = name
				continue
			}
			if raw, _ := field.Options["collection_id"].(string); raw != "" {
				if name := lookup[raw]; name != "" {
					field.Options["collection_name"] = name
				}
			}
		}
	}
}

func (s *Service) hydrateCollectionMetadata(ctx context.Context, coll *Collection) {
	if coll == nil {
		return
	}
	for i := range coll.Schema {
		field := &coll.Schema[i]
		if field.Options == nil {
			field.Options = map[string]any{}
		}
		if field.Type != FieldRelation {
			continue
		}
		if name, _ := field.Options["collection_name"].(string); name != "" {
			continue
		}
		if name, _ := field.Options["collection"].(string); name != "" {
			field.Options["collection_name"] = name
			continue
		}
		raw, _ := field.Options["collection_id"].(string)
		if raw == "" {
			continue
		}
		var name string
		if err := s.db.QueryRow(ctx, `SELECT name FROM _collections WHERE id = $1 OR name = $1 LIMIT 1`, raw).Scan(&name); err == nil && name != "" {
			field.Options["collection_name"] = name
		}
	}
}

// ---------------------------------------------------------------------------
// View collections
// ---------------------------------------------------------------------------

func (s *Service) createView(ctx context.Context, coll *Collection) error {
	return s.createViewWith(ctx, s.runnerForContext(ctx), coll)
}

func (s *Service) createViewWith(ctx context.Context, runner dbRunner, coll *Collection) error {
	if coll.ViewQuery == "" {
		return fmt.Errorf("view collection %q requires a view_query", coll.Name)
	}
	kind := "VIEW"
	if coll.IsMaterialized() {
		kind = "MATERIALIZED VIEW"
	}
	sql := fmt.Sprintf("CREATE %s %s AS %s", kind, s.quoteIdent(coll.Name), coll.ViewQuery)
	_, err := runner.Exec(ctx, sql)
	return err
}

// IsMaterialized reports whether a view collection is backed by a Postgres
// materialized view (options.materialized = true).
func (c *Collection) IsMaterialized() bool {
	if c.Type != TypeView || c.Options == nil {
		return false
	}
	v, _ := c.Options["materialized"].(bool)
	return v
}

// RefreshView refreshes a materialized view collection.
func (s *Service) RefreshView(ctx context.Context, coll *Collection) error {
	if !coll.IsMaterialized() {
		return fmt.Errorf("collection %q is not a materialized view", coll.Name)
	}
	_, err := s.runnerForContext(ctx).Exec(ctx,
		fmt.Sprintf("REFRESH MATERIALIZED VIEW %s", s.quoteIdent(coll.Name)))
	return err
}

func (s *Service) updateView(ctx context.Context, coll *Collection) error {
	return s.updateViewWith(ctx, s.runnerForContext(ctx), coll)
}

func (s *Service) updateViewWith(ctx context.Context, runner dbRunner, coll *Collection) error {
	if err := s.dropViewWith(ctx, runner, coll.Name); err != nil {
		return err
	}
	return s.createViewWith(ctx, runner, coll)
}

func (s *Service) dropView(ctx context.Context, name string) error {
	return s.dropViewWith(ctx, s.runnerForContext(ctx), name)
}

func (s *Service) dropViewWith(ctx context.Context, runner dbRunner, name string) error {
	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s CASCADE", s.quoteIdent(name))
	_, err := runner.Exec(ctx, sql)
	return err
}

// ---------------------------------------------------------------------------
// Table operations
// ---------------------------------------------------------------------------

func (s *Service) createTable(ctx context.Context, coll *Collection) error {
	return s.createTableWith(ctx, s.runnerForContext(ctx), coll)
}

func (s *Service) createTableWith(ctx context.Context, runner dbRunner, coll *Collection) error {
	columns := s.buildColumnDefsForCollection(coll)
	sql := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", s.quoteIdent(coll.Name), strings.Join(columns, ",\n  "))
	_, err := runner.Exec(ctx, sql)
	return err
}

func (s *Service) dropTable(ctx context.Context, name string) error {
	return s.dropTableWith(ctx, s.runnerForContext(ctx), name)
}

func (s *Service) dropTableWith(ctx context.Context, runner dbRunner, name string) error {
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", s.quoteIdent(name))
	_, err := runner.Exec(ctx, sql)
	return err
}

func (s *Service) syncTable(ctx context.Context, old, new *Collection) error {
	return s.syncTableWith(ctx, s.runnerForContext(ctx), old, new)
}

func (s *Service) syncTableWith(ctx context.Context, runner dbRunner, old, new *Collection) error {
	if old.Name != new.Name {
		sql := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", s.quoteIdent(old.Name), s.quoteIdent(new.Name))
		if _, err := runner.Exec(ctx, sql); err != nil {
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
			if err := s.addColumnWith(ctx, runner, new.Name, newField); err != nil {
				return err
			}
		} else if !fieldsEqual(oldField, newField) {
			if err := s.alterColumnWith(ctx, runner, new.Name, newField); err != nil {
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
			if err := s.dropColumnWith(ctx, runner, new.Name, oldField.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) addColumn(ctx context.Context, tableName string, field SchemaField) error {
	return s.addColumnWith(ctx, s.runnerForContext(ctx), tableName, field)
}

func (s *Service) addColumnWith(ctx context.Context, runner dbRunner, tableName string, field SchemaField) error {
	colDef := s.fieldToColumnDef(field)
	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", s.quoteIdent(tableName), colDef)
	_, err := runner.Exec(ctx, sql)
	return err
}

func (s *Service) alterColumn(ctx context.Context, tableName string, field SchemaField) error {
	return s.alterColumnWith(ctx, s.runnerForContext(ctx), tableName, field)
}

func (s *Service) alterColumnWith(ctx context.Context, runner dbRunner, tableName string, field SchemaField) error {
	pgType := s.columnTypeForField(field)
	sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
		s.quoteIdent(tableName), s.quoteIdent(field.Name),
		pgType, s.quoteIdent(field.Name), pgType)
	_, err := runner.Exec(ctx, sql)
	return err
}

func (s *Service) dropColumn(ctx context.Context, tableName, colName string) error {
	return s.dropColumnWith(ctx, s.runnerForContext(ctx), tableName, colName)
}

func (s *Service) dropColumnWith(ctx context.Context, runner dbRunner, tableName, colName string) error {
	sql := fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s CASCADE",
		s.quoteIdent(tableName), s.quoteIdent(colName))
	_, err := runner.Exec(ctx, sql)
	return err
}

func (s *Service) ensureSystemColumnsWith(ctx context.Context, runner dbRunner, coll *Collection) error {
	if coll.Type != TypeAuth {
		return nil
	}

	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s BOOLEAN NOT NULL DEFAULT FALSE",
		s.quoteIdent(coll.Name), s.quoteIdent("verified"))
	_, err := runner.Exec(ctx, sql)
	return err
}

// ---------------------------------------------------------------------------
// Record CRUD with filter engine integration
// ---------------------------------------------------------------------------

func (s *Service) marshalRecordData(coll *Collection, data map[string]any) (map[string]any, error) {
	if coll == nil || len(coll.Schema) == 0 || len(data) == 0 {
		return data, nil
	}

	schema := make([]collectionfields.SchemaField, 0, len(coll.Schema))
	for _, field := range coll.Schema {
		// Vector fields are encoded separately below; the generic field
		// registry does not model them.
		if field.Type == FieldVector {
			continue
		}
		schema = append(schema, collectionfields.SchemaField{
			ID:       field.ID,
			Name:     field.Name,
			Type:     collectionfields.FieldType(field.Type),
			System:   field.System,
			Required: field.Required,
			Unique:   field.Unique,
			Options:  field.Options,
		})
	}

	fieldsList, err := collectionfields.NewFieldsList(schema)
	if err != nil {
		return nil, err
	}

	marshaled, err := fieldsList.MarshalRecord(data)
	if err != nil {
		return nil, err
	}

	result := make(map[string]any, len(data))
	for k, v := range data {
		result[k] = v
	}
	for k, v := range marshaled {
		result[k] = v
	}

	// Encode vector fields into pgvector's text representation.
	for _, field := range coll.Schema {
		if field.Type != FieldVector {
			continue
		}
		if v, ok := result[field.Name]; ok && v != nil {
			encoded, err := encodeVector(v)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", field.Name, err)
			}
			if encoded == "" {
				result[field.Name] = nil
			} else {
				result[field.Name] = encoded
			}
		}
	}

	return result, nil
}

// valuePlaceholder returns the positional placeholder for a column, adding a
// ::vector cast for vector columns so bound text values coerce correctly.
func (s *Service) valuePlaceholder(coll *Collection, colName string, idx int) string {
	if s.isVectorField(coll, colName) {
		return fmt.Sprintf("$%d::vector", idx)
	}
	return fmt.Sprintf("$%d", idx)
}

func (s *Service) normalizeRecord(coll *Collection, record Record) Record {
	if coll == nil || record == nil {
		return record
	}

	fieldsByName := make(map[string]SchemaField, len(coll.Schema))
	for _, field := range coll.Schema {
		fieldsByName[field.Name] = field
	}

	for key, value := range record {
		field, ok := fieldsByName[key]
		if !ok {
			continue
		}
		record[key] = normalizeFieldValue(field, value)
	}

	return record
}

func normalizeFieldValue(field SchemaField, value any) any {
	switch field.Type {
	case FieldJSON, FieldGeoPoint, FieldFile, FieldRelation:
		return decodeMaybeJSONValue(value)
	case FieldDate, FieldAutoDate:
		switch v := value.(type) {
		case time.Time:
			return v.UTC().Format(time.RFC3339)
		case []byte:
			return string(v)
		default:
			return value
		}
	default:
		if raw, ok := value.([]byte); ok {
			return string(raw)
		}
		return value
	}
}

func decodeMaybeJSONValue(value any) any {
	switch v := value.(type) {
	case []byte:
		var decoded any
		if err := json.Unmarshal(v, &decoded); err == nil {
			return decoded
		}
		return string(v)
	case string:
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
			var decoded any
			if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
				return decoded
			}
		}
		return v
	default:
		return value
	}
}

// ErrViewReadOnly rejects writes to view collections. PostgreSQL silently
// accepts writes through simple auto-updatable views, so without an explicit
// check a "read-only" view would pass inserts straight to its base table.
var ErrViewReadOnly = fmt.Errorf("view collections are read-only")

// CreateRecord inserts a new record.
func (s *Service) CreateRecord(ctx context.Context, coll *Collection, data map[string]any) (Record, error) {
	if coll.Type == TypeView {
		return nil, ErrViewReadOnly
	}
	recordID, ok := data["id"].(string)
	if !ok || recordID == "" {
		recordID = uuid.New().String()
		data["id"] = recordID
	}

	data, err := s.marshalRecordData(coll, data)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
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
		placeholders = append(placeholders, s.valuePlaceholder(coll, k, i))
	}

	i++
	columns = append(columns, "created_at", "updated_at")
	placeholders = append(placeholders, fmt.Sprintf("$%d", i), fmt.Sprintf("$%d", i+1))
	values = append(values, time.Now(), time.Now())

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
		s.quoteIdent(coll.Name), strings.Join(columns, ", "), strings.Join(placeholders, ", "))

	rows, err := s.db.Query(ctx, sql, values...)
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
	return s.normalizeRecord(coll, record), nil
}

// CreateRecords inserts multiple records in a transaction.
func (s *Service) CreateRecords(ctx context.Context, coll *Collection, records []map[string]any) ([]Record, error) {
	if coll.Type == TypeView {
		return nil, ErrViewReadOnly
	}
	var results []Record
	err := s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		for _, data := range records {
			recordID, ok := data["id"].(string)
			if !ok || recordID == "" {
				recordID = uuid.New().String()
				data["id"] = recordID
			}

			data, err := s.marshalRecordData(coll, data)
			if err != nil {
				return fmt.Errorf("marshal record: %w", err)
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
				placeholders = append(placeholders, s.valuePlaceholder(coll, k, i))
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
				results = append(results, s.normalizeRecord(coll, record))
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
	err := s.db.QueryRow(ctx, countSQL, filterArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Query records
	querySQL := fmt.Sprintf("SELECT * FROM %s WHERE %s", source, whereClause)
	querySQL += " ORDER BY " + s.normalizeSort(coll, sort)
	querySQL += fmt.Sprintf(" LIMIT %d OFFSET %d", perPage, offset)

	rows, err := s.db.Query(ctx, querySQL, filterArgs...)
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
		records = append(records, s.normalizeRecord(coll, record))
	}

	return records, total, nil
}

func (s *Service) normalizeSort(coll *Collection, sort string) string {
	allowed := map[string]struct{}{
		"id":         {},
		"created_at": {},
		"updated_at": {},
	}
	if coll != nil {
		for _, field := range coll.Schema {
			allowed[field.Name] = struct{}{}
		}
		if coll.Type == TypeAuth {
			allowed["verified"] = struct{}{}
		}
	}

	var parts []string
	for _, raw := range strings.Split(sort, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}

		direction := "ASC"
		if strings.HasPrefix(part, "-") {
			direction = "DESC"
			part = strings.TrimSpace(part[1:])
		} else if strings.HasPrefix(part, "+") {
			part = strings.TrimSpace(part[1:])
		}

		if _, ok := allowed[part]; !ok {
			continue
		}
		if err := database.ValidateIdentifier(part, "sort field"); err != nil {
			continue
		}

		parts = append(parts, fmt.Sprintf("%s %s", s.quoteIdent(part), direction))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%s DESC", s.quoteIdent("created_at"))
	}

	return strings.Join(parts, ", ")
}

// GetRecord fetches a single record by ID.
func (s *Service) GetRecord(ctx context.Context, coll *Collection, recordID string) (Record, error) {
	sql := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", s.quoteIdent(coll.Name))
	rows, err := s.db.Query(ctx, sql, recordID)
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
	return s.normalizeRecord(coll, record), nil
}

// UpdateRecord updates an existing record.
func (s *Service) UpdateRecord(ctx context.Context, coll *Collection, recordID string, data map[string]any) error {
	if coll.Type == TypeView {
		return ErrViewReadOnly
	}
	delete(data, "id")
	delete(data, "created_at")

	data, err := s.marshalRecordData(coll, data)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	data["updated_at"] = time.Now()

	sets := []string{}
	values := []any{recordID}
	i := 1
	for k, v := range data {
		i++
		sets = append(sets, fmt.Sprintf("%s = %s", s.quoteIdent(k), s.valuePlaceholder(coll, k, i)))
		values = append(values, v)
	}

	sql := fmt.Sprintf("UPDATE %s SET %s WHERE id = $1",
		s.quoteIdent(coll.Name), strings.Join(sets, ", "))
	return s.db.Exec(ctx, sql, values...)
}

// DeleteRecord deletes a record by ID.
func (s *Service) DeleteRecord(ctx context.Context, coll *Collection, recordID string) error {
	if coll.Type == TypeView {
		return ErrViewReadOnly
	}
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.quoteIdent(coll.Name))
	return s.db.Exec(ctx, sql, recordID)
}

// CreateBatch creates multiple records atomically in a transaction.
func (s *Service) CreateBatch(ctx context.Context, coll *Collection, records []map[string]any) ([]Record, error) {
	return s.CreateRecords(ctx, coll, records)
}

// UpdateBatch updates multiple records atomically.
func (s *Service) UpdateBatch(ctx context.Context, coll *Collection, updates map[string]map[string]any) error {
	if coll.Type == TypeView {
		return ErrViewReadOnly
	}
	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		for id, data := range updates {
			delete(data, "id")
			delete(data, "created_at")
			data, err := s.marshalRecordData(coll, data)
			if err != nil {
				return fmt.Errorf("marshal record: %w", err)
			}
			data["updated_at"] = time.Now()
			sets := []string{}
			values := []any{id}
			i := 1
			for k, v := range data {
				i++
				sets = append(sets, fmt.Sprintf("%s = %s", s.quoteIdent(k), s.valuePlaceholder(coll, k, i)))
				values = append(values, v)
			}
			sql := fmt.Sprintf("UPDATE %s SET %s WHERE id = $1",
				s.quoteIdent(coll.Name), strings.Join(sets, ", "))
			if _, err := tx.Exec(ctx, sql, values...); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteBatch deletes multiple records atomically.
func (s *Service) DeleteBatch(ctx context.Context, coll *Collection, ids []string) error {
	if coll.Type == TypeView {
		return ErrViewReadOnly
	}
	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		for _, id := range ids {
			sql := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.quoteIdent(coll.Name))
			if _, err := tx.Exec(ctx, sql, id); err != nil {
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
	relatedCollectionName, relatedCollection, err := s.resolveRelatedCollection(ctx, field)
	if err != nil || relatedCollectionName == "" || relatedCollection == nil {
		return
	}

	ids := make(map[string]struct{})
	for _, record := range records {
		switch v := record[field.Name].(type) {
		case string:
			if v != "" {
				ids[v] = struct{}{}
			}
		case []any:
			for _, id := range v {
				if sid := strings.TrimSpace(fmt.Sprint(id)); sid != "" {
					ids[sid] = struct{}{}
				}
			}
		case []string:
			for _, id := range v {
				if id != "" {
					ids[id] = struct{}{}
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

	rows, err := s.db.Query(ctx, fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)", s.quoteIdent(relatedCollectionName), strings.Join(placeholders, ",")), args...)
	if err != nil {
		return
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	relatedMap := make(map[string]Record, len(idList))
	for rows.Next() {
		values, _ := rows.Values()
		relatedRecord := make(Record)
		for i, fd := range fieldDescs {
			relatedRecord[string(fd.Name)] = values[i]
		}
		relatedRecord = s.normalizeRecord(relatedCollection, relatedRecord)
		if id := strings.TrimSpace(fmt.Sprint(relatedRecord["id"])); id != "" {
			relatedRecord["_label"] = buildRelationLabel(relatedRecord, field)
			relatedMap[id] = relatedRecord
		}
	}

	for _, record := range records {
		if _, ok := record["expand"]; !ok {
			record["expand"] = make(map[string]any)
		}
		expandMap := record["expand"].(map[string]any)

		switch v := record[field.Name].(type) {
		case string:
			if expandedRecord, ok := relatedMap[v]; ok {
				expandMap[field.Name] = expandedRecord
			}
		case []any:
			var expanded []Record
			for _, id := range v {
				if expandedRecord, ok := relatedMap[strings.TrimSpace(fmt.Sprint(id))]; ok {
					expanded = append(expanded, expandedRecord)
				}
			}
			expandMap[field.Name] = expanded
		case []string:
			var expanded []Record
			for _, id := range v {
				if expandedRecord, ok := relatedMap[id]; ok {
					expanded = append(expanded, expandedRecord)
				}
			}
			expandMap[field.Name] = expanded
		}
	}
}

func (s *Service) resolveRelatedCollection(ctx context.Context, field SchemaField) (string, *Collection, error) {
	name, _ := field.Options["collection_name"].(string)
	if name == "" {
		if v, _ := field.Options["collection"].(string); v != "" {
			name = v
		}
	}
	if name == "" {
		if raw, _ := field.Options["collection_id"].(string); raw != "" {
			if err := s.db.QueryRow(ctx, `SELECT name FROM _collections WHERE id = $1 OR name = $1 LIMIT 1`, raw).Scan(&name); err == nil && name != "" {
				if field.Options != nil {
					field.Options["collection_name"] = name
				}
			}
		}
	}
	if name == "" {
		return "", nil, fmt.Errorf("relation target not configured")
	}
	relatedCollection, err := s.GetCollectionByName(ctx, name)
	if err != nil {
		return "", nil, err
	}
	return name, relatedCollection, nil
}

func buildRelationLabel(record Record, field SchemaField) string {
	var displayFields []string
	switch raw := field.Options["display_fields"].(type) {
	case []string:
		displayFields = raw
	case []any:
		for _, item := range raw {
			displayFields = append(displayFields, strings.TrimSpace(fmt.Sprint(item)))
		}
	case string:
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				displayFields = append(displayFields, item)
			}
		}
	}
	if len(displayFields) == 0 {
		for _, candidate := range []string{"title", "name", "label", "email", "username"} {
			if val := strings.TrimSpace(fmt.Sprint(record[candidate])); val != "" && val != "<nil>" {
				return val
			}
		}
		return strings.TrimSpace(fmt.Sprint(record["id"]))
	}
	parts := make([]string, 0, len(displayFields))
	for _, key := range displayFields {
		if val := strings.TrimSpace(fmt.Sprint(record[key])); val != "" && val != "<nil>" {
			parts = append(parts, val)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(fmt.Sprint(record["id"]))
	}
	return strings.Join(parts, " · ")
}

// ---------------------------------------------------------------------------
// Column builders
// ---------------------------------------------------------------------------

func (s *Service) buildColumnDefs(fields []SchemaField) []string {
	return s.buildColumnDefsForType(TypeBase, fields)
}

func (s *Service) buildColumnDefsForCollection(coll *Collection) []string {
	if coll == nil {
		return s.buildColumnDefs(nil)
	}
	return s.buildColumnDefsForType(coll.Type, coll.Schema)
}

func (s *Service) buildColumnDefsForType(collType CollectionType, fields []SchemaField) []string {
	columns := []string{"id TEXT PRIMARY KEY DEFAULT gen_random_uuid()"}
	for _, f := range fields {
		if f.Name == "id" {
			continue
		}
		columns = append(columns, s.fieldToColumnDef(f))
	}
	if collType == TypeAuth {
		columns = append(columns, `verified BOOLEAN NOT NULL DEFAULT FALSE`)
	}
	columns = append(columns,
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
	)
	return columns
}

func (s *Service) fieldToColumnDef(field SchemaField) string {
	pgType := s.columnTypeForField(field)
	def := fmt.Sprintf("%s %s", s.quoteIdent(field.Name), pgType)
	if field.Required {
		def += " NOT NULL"
	}
	if defSQL, err := s.defaultSQLLiteral(field); err == nil && defSQL != "" {
		def += " DEFAULT " + defSQL
	}
	return def
}

func (s *Service) columnTypeForField(field SchemaField) string {
	switch field.Type {
	case FieldRelation:
		if maxSelect, ok := maxSelectOption(field.Options); ok && maxSelect > 1 {
			return "JSONB"
		}
	case FieldFile:
		return "JSONB"
	case FieldVector:
		return vectorColumnType(field)
	}
	return s.fieldToPGType(field.Type)
}

func maxSelectOption(opts map[string]any) (int, bool) {
	if opts == nil {
		return 0, false
	}
	switch v := opts["max_select"].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func (s *Service) defaultSQLLiteral(field SchemaField) (string, error) {
	defVal, ok := field.Options["default"]
	if !ok {
		return "", nil
	}

	switch field.Type {
	case FieldText, FieldEmail, FieldURL, FieldSelect, FieldPassword, FieldEditor:
		str, ok := defVal.(string)
		if !ok {
			return "", fmt.Errorf("default for %s must be a string", field.Name)
		}
		return quoteSQLString(str), nil

	case FieldRelation:
		if maxSelect, ok := maxSelectOption(field.Options); ok && maxSelect > 1 {
			raw, err := json.Marshal(defVal)
			if err != nil {
				return "", fmt.Errorf("default for %s must be valid JSON: %w", field.Name, err)
			}
			return quoteSQLString(string(raw)) + "::jsonb", nil
		}
		str, ok := defVal.(string)
		if !ok {
			return "", fmt.Errorf("default for %s must be a string", field.Name)
		}
		return quoteSQLString(str), nil

	case FieldNumber:
		switch v := defVal.(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		case float32:
			return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
		case int:
			return strconv.Itoa(v), nil
		case int8, int16, int32, int64:
			return fmt.Sprintf("%d", v), nil
		case uint, uint8, uint16, uint32, uint64:
			return fmt.Sprintf("%d", v), nil
		default:
			return "", fmt.Errorf("default for %s must be numeric", field.Name)
		}

	case FieldBool:
		b, ok := defVal.(bool)
		if !ok {
			return "", fmt.Errorf("default for %s must be boolean", field.Name)
		}
		if b {
			return "TRUE", nil
		}
		return "FALSE", nil

	case FieldJSON, FieldFile, FieldGeoPoint:
		raw, err := json.Marshal(defVal)
		if err != nil {
			return "", fmt.Errorf("default for %s must be valid JSON: %w", field.Name, err)
		}
		return quoteSQLString(string(raw)) + "::jsonb", nil

	case FieldVector:
		return "", nil // vector fields do not support SQL defaults

	case FieldDate, FieldAutoDate:
		switch v := defVal.(type) {
		case string:
			return quoteSQLString(v), nil
		case time.Time:
			return quoteSQLString(v.UTC().Format(time.RFC3339Nano)), nil
		default:
			return "", fmt.Errorf("default for %s must be a date string", field.Name)
		}

	default:
		return quoteSQLString(fmt.Sprint(defVal)), nil
	}
}

func quoteSQLString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
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
	return database.QuoteIdent(name)
}

// Exports for testing
func (s *Service) QuoteIdentExport(name string) string     { return s.quoteIdent(name) }
func (s *Service) FieldToPGTypeExport(ft FieldType) string { return s.fieldToPGType(ft) }
func (s *Service) BuildColumnDefsExport(fields []SchemaField) []string {
	return s.buildColumnDefs(fields)
}
func FieldsEqualExport(a, b SchemaField) bool { return fieldsEqual(a, b) }

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
