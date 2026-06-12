package collection

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// Vector field option keys.
const (
	vectorOptDimensions = "dimensions"
	vectorOptDistance   = "distance" // cosine | l2 | inner
	vectorOptIndex      = "index"    // none | hnsw | ivfflat
)

// VectorDistance identifies a pgvector distance metric.
type VectorDistance string

const (
	DistanceCosine VectorDistance = "cosine"
	DistanceL2     VectorDistance = "l2"
	DistanceInner  VectorDistance = "inner"
)

// vectorDistanceConfig maps a distance metric to its pgvector operator and
// index operator class.
var vectorDistanceConfig = map[VectorDistance]struct {
	operator string
	opClass  string
}{
	DistanceCosine: {"<=>", "vector_cosine_ops"},
	DistanceL2:     {"<->", "vector_l2_ops"},
	DistanceInner:  {"<#>", "vector_ip_ops"},
}

// isVectorField reports whether the named field is a vector field.
func (s *Service) isVectorField(coll *Collection, name string) bool {
	if coll == nil {
		return false
	}
	for _, f := range coll.Schema {
		if f.Name == name {
			return f.Type == FieldVector
		}
	}
	return false
}

// vectorFields returns all vector fields in a collection.
func vectorFields(coll *Collection) []SchemaField {
	var out []SchemaField
	for _, f := range coll.Schema {
		if f.Type == FieldVector {
			out = append(out, f)
		}
	}
	return out
}

// vectorDimensions returns the configured dimension count (0 if unspecified).
func vectorDimensions(field SchemaField) int {
	switch v := field.Options[vectorOptDimensions].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// vectorDistanceOf returns the configured distance metric (cosine by default).
func vectorDistanceOf(field SchemaField) VectorDistance {
	if raw, ok := field.Options[vectorOptDistance].(string); ok {
		switch VectorDistance(strings.ToLower(strings.TrimSpace(raw))) {
		case DistanceL2:
			return DistanceL2
		case DistanceInner:
			return DistanceInner
		case DistanceCosine:
			return DistanceCosine
		}
	}
	return DistanceCosine
}

// vectorIndexKind returns the configured index kind ("hnsw" by default, "none" to skip).
func vectorIndexKind(field SchemaField) string {
	if raw, ok := field.Options[vectorOptIndex].(string); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "none", "":
			if raw == "" {
				return "hnsw"
			}
			return "none"
		case "ivfflat":
			return "ivfflat"
		case "hnsw":
			return "hnsw"
		}
	}
	return "hnsw"
}

// vectorColumnType returns the SQL column type for a vector field.
func vectorColumnType(field SchemaField) string {
	if dim := vectorDimensions(field); dim > 0 {
		return fmt.Sprintf("vector(%d)", dim)
	}
	return "vector"
}

// encodeVector converts a value into pgvector's text representation "[1,2,3]".
// Accepts []float64, []float32, []any of numbers, or an already-formatted string.
func encodeVector(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", nil
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			return trimmed, nil
		}
		return "", fmt.Errorf("vector string must be in [n,n,...] format")
	case []float64:
		return floatsToVector(v), nil
	case []float32:
		out := make([]float64, len(v))
		for i, f := range v {
			out[i] = float64(f)
		}
		return floatsToVector(out), nil
	case []any:
		out := make([]float64, 0, len(v))
		for _, item := range v {
			f, err := toFloat(item)
			if err != nil {
				return "", err
			}
			out = append(out, f)
		}
		return floatsToVector(out), nil
	default:
		return "", fmt.Errorf("vector value must be an array of numbers, got %T", value)
	}
}

func floatsToVector(vals []float64) string {
	parts := make([]string, len(vals))
	for i, f := range vals {
		parts[i] = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	default:
		return 0, fmt.Errorf("vector element must be numeric, got %T", v)
	}
}

// ensureVectorExtension creates the pgvector extension. Returns a friendly
// error when the extension is not available on this PostgreSQL.
func (s *Service) ensureVectorExtension(ctx context.Context, runner dbRunner) error {
	if _, err := runner.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("pgvector is not available on this PostgreSQL (vector fields require the 'vector' extension; install it or use an external PostgreSQL that ships pgvector): %w", err)
	}
	return nil
}

// ensureVectorIndexes creates an approximate-nearest-neighbour index for each
// vector field that requests one. Index build failures are logged and skipped
// (search still works via exact scan) rather than failing the whole operation.
func (s *Service) ensureVectorIndexes(ctx context.Context, runner dbRunner, coll *Collection) {
	for _, field := range vectorFields(coll) {
		kind := vectorIndexKind(field)
		if kind == "none" {
			continue
		}
		cfg, ok := vectorDistanceConfig[vectorDistanceOf(field)]
		if !ok {
			continue
		}
		idxName := fmt.Sprintf("idx_%s_%s_vec", coll.Name, field.Name)
		var stmt string
		switch kind {
		case "ivfflat":
			stmt = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING ivfflat (%s %s) WITH (lists = 100)",
				s.quoteIdent(idxName), s.quoteIdent(coll.Name), s.quoteIdent(field.Name), cfg.opClass)
		default: // hnsw
			stmt = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (%s %s)",
				s.quoteIdent(idxName), s.quoteIdent(coll.Name), s.quoteIdent(field.Name), cfg.opClass)
		}
		if _, err := runner.Exec(ctx, stmt); err != nil {
			// hnsw needs pgvector >= 0.5.0; fall back to ivfflat.
			if kind == "hnsw" {
				fallback := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING ivfflat (%s %s) WITH (lists = 100)",
					s.quoteIdent(idxName), s.quoteIdent(coll.Name), s.quoteIdent(field.Name), cfg.opClass)
				if _, ferr := runner.Exec(ctx, fallback); ferr != nil {
					log.Warn().Str("collection", coll.Name).Str("field", field.Name).Err(ferr).Msg("vector index skipped")
				}
				continue
			}
			log.Warn().Str("collection", coll.Name).Str("field", field.Name).Err(err).Msg("vector index skipped")
		}
	}
}

// applyUserIndexes executes the admin-defined index expressions stored on the
// collection. Each statement is run independently; a failure is logged and
// skipped so one malformed index never blocks a collection save.
func (s *Service) applyUserIndexes(ctx context.Context, runner dbRunner, coll *Collection) {
	for _, raw := range coll.Indexes {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		// Only allow CREATE [UNIQUE] INDEX statements; never anything else.
		upper := strings.ToUpper(stmt)
		if !strings.HasPrefix(upper, "CREATE INDEX") && !strings.HasPrefix(upper, "CREATE UNIQUE INDEX") {
			log.Warn().Str("collection", coll.Name).Str("index", stmt).Msg("ignored non-CREATE-INDEX expression")
			continue
		}
		if strings.Contains(stmt, ";") {
			log.Warn().Str("collection", coll.Name).Str("index", stmt).Msg("ignored index with multiple statements")
			continue
		}
		if _, err := runner.Exec(ctx, stmt); err != nil {
			log.Warn().Str("collection", coll.Name).Str("index", stmt).Err(err).Msg("index skipped")
		}
	}
}

// VectorSearchOptions configures a similarity search.
type VectorSearchOptions struct {
	Field     string
	Vector    any // []float64 / []any / "[..]" string
	Limit     int
	Distance  string // optional override of the field's configured metric
	Where     string // additional SQL WHERE fragment (already parameterized)
	WhereArgs []any
}

// VectorSearch returns records ordered by similarity to the query vector,
// annotating each with a numeric "_distance". Results respect any extra WHERE
// clause supplied by the caller (used for access-rule enforcement).
func (s *Service) VectorSearch(ctx context.Context, coll *Collection, opts VectorSearchOptions) ([]Record, error) {
	if coll == nil {
		return nil, fmt.Errorf("collection is required")
	}
	var field *SchemaField
	for i := range coll.Schema {
		if coll.Schema[i].Name == opts.Field && coll.Schema[i].Type == FieldVector {
			field = &coll.Schema[i]
			break
		}
	}
	if field == nil {
		return nil, fmt.Errorf("%q is not a vector field", opts.Field)
	}

	distance := vectorDistanceOf(*field)
	if opts.Distance != "" {
		distance = VectorDistance(strings.ToLower(opts.Distance))
	}
	cfg, ok := vectorDistanceConfig[distance]
	if !ok {
		return nil, fmt.Errorf("unsupported distance metric %q", distance)
	}

	encoded, err := encodeVector(opts.Vector)
	if err != nil {
		return nil, err
	}
	if encoded == "" {
		return nil, fmt.Errorf("query vector is required")
	}
	if dim := vectorDimensions(*field); dim > 0 {
		if got := vectorLen(encoded); got != dim {
			return nil, fmt.Errorf("query vector has %d dimensions, expected %d", got, dim)
		}
	}

	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	where := "TRUE"
	args := []any{encoded}
	if strings.TrimSpace(opts.Where) != "" {
		where = opts.Where
		args = append(args, opts.WhereArgs...)
	}

	distExpr := fmt.Sprintf("%s %s $1::vector", s.quoteIdent(field.Name), cfg.operator)
	sql := fmt.Sprintf("SELECT *, (%s) AS _distance FROM %s WHERE %s ORDER BY %s LIMIT %d",
		distExpr, s.quoteIdent(coll.Name), where, distExpr, limit)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	records := []Record{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		record := make(Record)
		for i, fd := range fieldDescs {
			record[string(fd.Name)] = values[i]
		}
		records = append(records, s.normalizeRecord(coll, record))
	}
	return records, nil
}

func vectorLen(encoded string) int {
	trimmed := strings.Trim(strings.TrimSpace(encoded), "[]")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, ",") + 1
}
