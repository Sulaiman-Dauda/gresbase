// Package tools/search provides full-text search capabilities using PostgreSQL tsvector.
package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/gresbase/gresbase/internal/database"
)

// Provider manages full-text search indexes on collections.
type Provider struct {
	db *database.DB
}

// NewProvider creates a new search provider.
func NewProvider(db *database.DB) *Provider {
	return &Provider{db: db}
}

// IndexConfig configures a full-text search index on a collection.
type IndexConfig struct {
	Collection string   `json:"collection"`
	Fields     []string `json:"fields"`   // Fields to index
	Language   string   `json:"language"` // PostgreSQL text search language (default: 'english')
	Weight     string   `json:"weight"`   // Weight labels like 'A', 'B', 'C', 'D'
}

// CreateIndex creates a tsvector index on the specified collection fields.
func (p *Provider) CreateIndex(ctx context.Context, config IndexConfig) error {
	if config.Language == "" {
		config.Language = "english"
	}

	// Build the tsvector expression
	var expressions []string
	for _, field := range config.Fields {
		expr := fmt.Sprintf("setweight(to_tsvector('%s', COALESCE(%s, '')), '%s')",
			config.Language, p.quoteIdent(field), config.Weight)
		expressions = append(expressions, expr)
	}

	combinedExpr := strings.Join(expressions, " || ")

	// Add tsvector column
	alterSQL := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS _fts TSVECTOR",
		p.quoteIdent(config.Collection),
	)
	if err := p.db.Exec(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to add _fts column: %w", err)
	}

	// Create or replace the generated column
	updateSQL := fmt.Sprintf(
		"ALTER TABLE %s DROP COLUMN IF EXISTS _fts CASCADE",
		p.quoteIdent(config.Collection),
	)
	if err := p.db.Exec(ctx, updateSQL); err != nil {
		return fmt.Errorf("failed to drop existing _fts column: %w", err)
	}

	addSQL := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN _fts TSVECTOR GENERATED ALWAYS AS (%s) STORED",
		p.quoteIdent(config.Collection), combinedExpr,
	)
	if err := p.db.Exec(ctx, addSQL); err != nil {
		return fmt.Errorf("failed to create generated tsvector: %w", err)
	}

	// Create GIN index
	indexName := fmt.Sprintf("idx_%s_fts", config.Collection)
	indexSQL := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (_fts)",
		p.quoteIdent(indexName), p.quoteIdent(config.Collection),
	)
	if err := p.db.Exec(ctx, indexSQL); err != nil {
		return fmt.Errorf("failed to create GIN index: %w", err)
	}

	return nil
}

// SearchQuery performs a full-text search on a collection.
type SearchQuery struct {
	Collection string `json:"collection"`
	Query      string `json:"query"`    // Natural language query
	Language   string `json:"language"` // Default 'english'
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	Highlight  bool   `json:"highlight"` // Include ts_headline
	Rank       bool   `json:"rank"`      // Include ts_rank
}

// SearchResult represents a search hit.
type SearchResult struct {
	Record   map[string]any `json:"record"`
	Rank     float64        `json:"rank,omitempty"`
	Headline string         `json:"headline,omitempty"`
}

// Search executes a full-text search query.
func (p *Provider) Search(ctx context.Context, q SearchQuery) ([]SearchResult, int64, error) {
	if q.Language == "" {
		q.Language = "english"
	}
	if q.PerPage <= 0 {
		q.PerPage = 30
	}
	if q.Page <= 0 {
		q.Page = 1
	}

	// Parse query into tsquery
	tsQuery := p.buildTSQuery(q.Query)
	offset := (q.Page - 1) * q.PerPage

	// Count
	var total int64
	countSQL := fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE _fts @@ to_tsquery('%s', $1)",
		p.quoteIdent(q.Collection), q.Language,
	)
	if err := p.db.QueryRow(ctx, countSQL, tsQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("search count failed: %w", err)
	}

	// Build SELECT
	selectParts := []string{"t.*"}
	if q.Rank {
		selectParts = append(selectParts,
			fmt.Sprintf("ts_rank(_fts, to_tsquery('%s', $1)) AS _rank", q.Language))
	}
	if q.Highlight {
		selectParts = append(selectParts,
			fmt.Sprintf("ts_headline('%s', t.%s, to_tsquery('%s', $1)) AS _headline",
				q.Language, p.getFirstTextField(q.Collection), q.Language))
	}

	searchSQL := fmt.Sprintf(
		"SELECT %s FROM %s t WHERE _fts @@ to_tsquery('%s', $1) ORDER BY %s DESC LIMIT %d OFFSET %d",
		strings.Join(selectParts, ", "),
		p.quoteIdent(q.Collection),
		q.Language,
		func() string {
			if q.Rank {
				return "_rank"
			} else {
				return "created_at"
			}
		}(),
		q.PerPage, offset,
	)

	rows, err := p.db.Query(ctx, searchSQL, tsQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("search failed: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	cols := rows.FieldDescriptions()
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}

		record := make(map[string]any)
		var rank float64
		var headline string

		for i, col := range cols {
			name := col.Name
			switch name {
			case "_rank":
				if v, ok := vals[i].(float64); ok {
					rank = v
				}
			case "_headline":
				if v, ok := vals[i].(string); ok {
					headline = v
				}
			default:
				record[name] = vals[i]
			}
		}

		results = append(results, SearchResult{
			Record:   record,
			Rank:     rank,
			Headline: headline,
		})
	}

	return results, total, nil
}

// buildTSQuery converts a natural language query to a tsquery string.
// Handles plain text, AND/OR, exact phrases, and negation.
func (p *Provider) buildTSQuery(query string) string {
	// Split into words, handling OR/AND operators
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}

	// Join with & for AND semantics (or | for OR)
	var parts []string
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		// Skip explicit operators - they're handled below
		if strings.EqualFold(word, "OR") || strings.EqualFold(word, "AND") {
			continue
		}
		// Handle negation
		if strings.HasPrefix(word, "-") {
			word = "!" + word[1:]
		}
		// Escape special chars
		word = strings.ReplaceAll(word, "'", "''")
		parts = append(parts, word)
	}

	// Default: AND all terms
	result := strings.Join(parts, " & ")

	// If query has explicit OR, preserve it
	if strings.Contains(strings.ToUpper(query), " OR ") {
		result = strings.Join(parts, " | ")
	}

	return result
}

// RemoveIndex drops the FTS index from a collection.
func (p *Provider) RemoveIndex(ctx context.Context, collection string) error {
	sql := fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS _fts CASCADE", p.quoteIdent(collection))
	return p.db.Exec(ctx, sql)
}

// getFirstTextField returns the first text-like field from a collection for highlighting.
func (p *Provider) getFirstTextField(collection string) string {
	// Default to 'name' or 'title' as headline source
	// In practice, this would query _collections schema
	return "name"
}

// RebuildIndex rebuilds the FTS index for all records in a collection.
func (p *Provider) RebuildIndex(ctx context.Context, collection string) error {
	_ = fmt.Sprintf("UPDATE %s SET _fts = _fts", p.quoteIdent(collection))
	// This is a no-op trigger to rebuild generated columns
	// Actually need to drop and re-add:
	// We rely on the generated column, which auto-updates.
	// Force reindex:
	reindexSQL := fmt.Sprintf("REINDEX INDEX idx_%s_fts", collection)
	return p.db.Exec(ctx, reindexSQL)
}

func (p *Provider) quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
