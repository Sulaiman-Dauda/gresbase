package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryBuilder provides a fluent, safe SQL query builder for PostgreSQL.
// It generates parameterized queries to prevent SQL injection while
// providing a readable, chainable API similar to PocketBase's dbx.
type QueryBuilder struct {
	pool         *pgxpool.Pool
	table        string
	columns      []string
	whereParts   []string
	whereArgs    []any
	orderParts   []string
	limitVal     int
	offsetVal    int
	joinParts    []string
	groupParts   []string
	havingParts  []string
	havingArgs   []any
	setParts     []string
	setArgs      []any
	returning    []string
	distinct     bool
	paramCounter int
	err          error
}

// NewQuery creates a new QueryBuilder targeting the given table.
func NewQuery(pool *pgxpool.Pool, table string) *QueryBuilder {
	return &QueryBuilder{
		pool:         pool,
		table:        table,
		paramCounter: 0,
	}
}

// Select sets the columns for a SELECT query.
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder {
	qb.columns = columns
	return qb
}

// Distinct adds DISTINCT to the SELECT query.
func (qb *QueryBuilder) Distinct() *QueryBuilder {
	qb.distinct = true
	return qb
}

// Where adds a WHERE condition with parameterized arguments.
// Example: qb.Where("age > $", 18) -> WHERE age > $1
func (qb *QueryBuilder) Where(condition string, args ...any) *QueryBuilder {
	qb.paramCounter++
	cond := strings.Replace(condition, "$", fmt.Sprintf("$%d", qb.paramCounter), 1)
	qb.whereParts = append(qb.whereParts, cond)
	qb.whereArgs = append(qb.whereArgs, args...)
	return qb
}

// WhereRaw adds a raw WHERE condition without parameterization.
// Use with caution — only for trusted inputs.
func (qb *QueryBuilder) WhereRaw(condition string) *QueryBuilder {
	qb.whereParts = append(qb.whereParts, condition)
	return qb
}

// WhereIn adds a WHERE ... IN (...) clause.
func (qb *QueryBuilder) WhereIn(column string, values []any) *QueryBuilder {
	if len(values) == 0 {
		qb.err = fmt.Errorf("WHERE IN requires at least one value")
		return qb
	}
	placeholders := make([]string, len(values))
	for i := range values {
		qb.paramCounter++
		placeholders[i] = fmt.Sprintf("$%d", qb.paramCounter)
	}
	qb.whereParts = append(qb.whereParts, fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")))
	qb.whereArgs = append(qb.whereArgs, values...)
	return qb
}

// WhereNotIn adds a WHERE ... NOT IN (...) clause.
func (qb *QueryBuilder) WhereNotIn(column string, values []any) *QueryBuilder {
	if len(values) == 0 {
		return qb
	}
	placeholders := make([]string, len(values))
	for i := range values {
		qb.paramCounter++
		placeholders[i] = fmt.Sprintf("$%d", qb.paramCounter)
	}
	qb.whereParts = append(qb.whereParts, fmt.Sprintf("%s NOT IN (%s)", column, strings.Join(placeholders, ", ")))
	qb.whereArgs = append(qb.whereArgs, values...)
	return qb
}

// WhereNull adds a WHERE ... IS NULL clause.
func (qb *QueryBuilder) WhereNull(column string) *QueryBuilder {
	qb.whereParts = append(qb.whereParts, fmt.Sprintf("%s IS NULL", column))
	return qb
}

// WhereNotNull adds a WHERE ... IS NOT NULL clause.
func (qb *QueryBuilder) WhereNotNull(column string) *QueryBuilder {
	qb.whereParts = append(qb.whereParts, fmt.Sprintf("%s IS NOT NULL", column))
	return qb
}

// WhereJSONContains adds a WHERE clause checking if a JSONB column contains a value.
func (qb *QueryBuilder) WhereJSONContains(column string, value any) *QueryBuilder {
	qb.paramCounter++
	jsonBytes, _ := json.Marshal(value)
	qb.whereParts = append(qb.whereParts, fmt.Sprintf("%s @> $%d", column, qb.paramCounter))
	qb.whereArgs = append(qb.whereArgs, string(jsonBytes))
	return qb
}

// And adds an AND conjunction to the WHERE clause.
func (qb *QueryBuilder) And() *QueryBuilder {
	if len(qb.whereParts) > 0 {
		qb.whereParts = append(qb.whereParts, "AND")
	}
	return qb
}

// Or adds an OR conjunction to the WHERE clause.
func (qb *QueryBuilder) Or() *QueryBuilder {
	if len(qb.whereParts) > 0 {
		qb.whereParts = append(qb.whereParts, "OR")
	}
	return qb
}

// OpenParen opens a parenthesis group in the WHERE clause.
func (qb *QueryBuilder) OpenParen() *QueryBuilder {
	qb.whereParts = append(qb.whereParts, "(")
	return qb
}

// CloseParen closes a parenthesis group in the WHERE clause.
func (qb *QueryBuilder) CloseParen() *QueryBuilder {
	qb.whereParts = append(qb.whereParts, ")")
	return qb
}

// OrderBy sets the ORDER BY clause.
func (qb *QueryBuilder) OrderBy(clauses ...string) *QueryBuilder {
	qb.orderParts = append(qb.orderParts, clauses...)
	return qb
}

// Limit sets the LIMIT clause.
func (qb *QueryBuilder) Limit(n int) *QueryBuilder {
	qb.limitVal = n
	return qb
}

// Offset sets the OFFSET clause.
func (qb *QueryBuilder) Offset(n int) *QueryBuilder {
	qb.offsetVal = n
	return qb
}

// Join adds a JOIN clause.
func (qb *QueryBuilder) Join(joinClause string) *QueryBuilder {
	qb.joinParts = append(qb.joinParts, joinClause)
	return qb
}

// GroupBy adds a GROUP BY clause.
func (qb *QueryBuilder) GroupBy(columns ...string) *QueryBuilder {
	qb.groupParts = append(qb.groupParts, columns...)
	return qb
}

// Having adds a HAVING condition.
func (qb *QueryBuilder) Having(condition string, args ...any) *QueryBuilder {
	qb.paramCounter++
	cond := strings.Replace(condition, "$", fmt.Sprintf("$%d", qb.paramCounter), 1)
	qb.havingParts = append(qb.havingParts, cond)
	qb.havingArgs = append(qb.havingArgs, args...)
	return qb
}

// Set adds a SET clause for UPDATE queries.
func (qb *QueryBuilder) Set(column string, value any) *QueryBuilder {
	qb.paramCounter++
	qb.setParts = append(qb.setParts, fmt.Sprintf("%s = $%d", column, qb.paramCounter))
	qb.setArgs = append(qb.setArgs, value)
	return qb
}

// SetRaw adds a raw SET clause (e.g., "updated_at = NOW()").
func (qb *QueryBuilder) SetRaw(expr string) *QueryBuilder {
	qb.setParts = append(qb.setParts, expr)
	return qb
}

// Returning adds a RETURNING clause.
func (qb *QueryBuilder) Returning(columns ...string) *QueryBuilder {
	qb.returning = columns
	return qb
}

// BuildSelect builds the SELECT query string and args.
func (qb *QueryBuilder) BuildSelect() (string, []any, error) {
	if qb.err != nil {
		return "", nil, qb.err
	}

	var b strings.Builder
	b.WriteString("SELECT ")

	if qb.distinct {
		b.WriteString("DISTINCT ")
	}

	if len(qb.columns) == 0 {
		b.WriteString("*")
	} else {
		b.WriteString(strings.Join(qb.columns, ", "))
	}

	b.WriteString(" FROM ")
	b.WriteString(qb.table)

	if len(qb.joinParts) > 0 {
		b.WriteString(" ")
		b.WriteString(strings.Join(qb.joinParts, " "))
	}

	if len(qb.whereParts) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(qb.whereParts, " "))
	}

	if len(qb.groupParts) > 0 {
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(qb.groupParts, ", "))
	}

	if len(qb.havingParts) > 0 {
		b.WriteString(" HAVING ")
		b.WriteString(strings.Join(qb.havingParts, " "))
	}

	if len(qb.orderParts) > 0 {
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(qb.orderParts, ", "))
	}

	if qb.limitVal > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d", qb.limitVal))
	}

	if qb.offsetVal > 0 {
		b.WriteString(fmt.Sprintf(" OFFSET %d", qb.offsetVal))
	}

	args := append(qb.whereArgs, qb.havingArgs...)
	return b.String(), args, nil
}

// BuildUpdate builds the UPDATE query string and args.
func (qb *QueryBuilder) BuildUpdate() (string, []any, error) {
	if qb.err != nil {
		return "", nil, qb.err
	}
	if len(qb.setParts) == 0 {
		return "", nil, fmt.Errorf("UPDATE requires at least one SET clause")
	}

	var b strings.Builder
	b.WriteString("UPDATE ")
	b.WriteString(qb.table)
	b.WriteString(" SET ")
	b.WriteString(strings.Join(qb.setParts, ", "))

	if len(qb.whereParts) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(qb.whereParts, " "))
	}

	if len(qb.returning) > 0 {
		b.WriteString(" RETURNING ")
		b.WriteString(strings.Join(qb.returning, ", "))
	}

	// Args: SET args come first, then WHERE args
	args := append(qb.setArgs, qb.whereArgs...)
	return b.String(), args, nil
}

// BuildInsert builds the INSERT query string and args.
func (qb *QueryBuilder) BuildInsert(columns []string, values []any) (string, []any, error) {
	if qb.err != nil {
		return "", nil, qb.err
	}
	if len(columns) != len(values) {
		return "", nil, fmt.Errorf("column count (%d) != value count (%d)", len(columns), len(values))
	}

	placeholders := make([]string, len(values))
	args := make([]any, 0, len(values))
	for i, v := range values {
		qb.paramCounter++
		placeholders[i] = fmt.Sprintf("$%d", qb.paramCounter)
		args = append(args, v)
	}

	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(qb.table)
	b.WriteString(" (")
	b.WriteString(strings.Join(columns, ", "))
	b.WriteString(") VALUES (")
	b.WriteString(strings.Join(placeholders, ", "))
	b.WriteString(")")

	if len(qb.returning) > 0 {
		b.WriteString(" RETURNING ")
		b.WriteString(strings.Join(qb.returning, ", "))
	}

	return b.String(), args, nil
}

// BuildDelete builds the DELETE query string and args.
func (qb *QueryBuilder) BuildDelete() (string, []any, error) {
	if qb.err != nil {
		return "", nil, qb.err
	}

	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(qb.table)

	if len(qb.whereParts) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(qb.whereParts, " "))
	}

	if len(qb.returning) > 0 {
		b.WriteString(" RETURNING ")
		b.WriteString(strings.Join(qb.returning, ", "))
	}

	return b.String(), qb.whereArgs, nil
}

// BuildCount builds a COUNT query string and args.
func (qb *QueryBuilder) BuildCount() (string, []any, error) {
	if qb.err != nil {
		return "", nil, qb.err
	}

	var b strings.Builder
	b.WriteString("SELECT COUNT(*) FROM ")
	b.WriteString(qb.table)

	if len(qb.joinParts) > 0 {
		b.WriteString(" ")
		b.WriteString(strings.Join(qb.joinParts, " "))
	}

	if len(qb.whereParts) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(qb.whereParts, " "))
	}

	return b.String(), qb.whereArgs, nil
}

// ---------------------------------------------------------------------------
// Execution methods
// ---------------------------------------------------------------------------

// Execute runs the built SELECT query and returns pgx.Rows.
func (qb *QueryBuilder) Execute(ctx context.Context) (pgx.Rows, error) {
	sql, args, err := qb.BuildSelect()
	if err != nil {
		return nil, err
	}
	return qb.pool.Query(ctx, sql, args...)
}

// ExecuteRow runs the built SELECT query and returns a single row.
func (qb *QueryBuilder) ExecuteRow(ctx context.Context) pgx.Row {
	sql, args, err := qb.BuildSelect()
	if err != nil {
		return &errorRow{err: err}
	}
	return qb.pool.QueryRow(ctx, sql, args...)
}

// ExecuteUpdate runs the built UPDATE query.
func (qb *QueryBuilder) ExecuteUpdate(ctx context.Context) (pgx.Rows, error) {
	sql, args, err := qb.BuildUpdate()
	if err != nil {
		return nil, err
	}
	if len(qb.returning) > 0 {
		return qb.pool.Query(ctx, sql, args...)
	}
	_, execErr := qb.pool.Exec(ctx, sql, args...)
	return nil, execErr
}

// ExecuteInsert runs the built INSERT query.
func (qb *QueryBuilder) ExecuteInsert(ctx context.Context, columns []string, values []any) (pgx.Rows, error) {
	sql, args, err := qb.BuildInsert(columns, values)
	if err != nil {
		return nil, err
	}
	if len(qb.returning) > 0 {
		return qb.pool.Query(ctx, sql, args...)
	}
	_, execErr := qb.pool.Exec(ctx, sql, args...)
	return nil, execErr
}

// ExecuteDelete runs the built DELETE query.
func (qb *QueryBuilder) ExecuteDelete(ctx context.Context) (pgx.Rows, error) {
	sql, args, err := qb.BuildDelete()
	if err != nil {
		return nil, err
	}
	if len(qb.returning) > 0 {
		return qb.pool.Query(ctx, sql, args...)
	}
	_, execErr := qb.pool.Exec(ctx, sql, args...)
	return nil, execErr
}

// ExecuteCount runs a COUNT query and returns the total.
func (qb *QueryBuilder) ExecuteCount(ctx context.Context) (int64, error) {
	sql, args, err := qb.BuildCount()
	if err != nil {
		return 0, err
	}
	var count int64
	err = qb.pool.QueryRow(ctx, sql, args...).Scan(&count)
	return count, err
}

// errorRow is a pgx.Row that always returns an error. Used when BuildSelect fails.
type errorRow struct {
	err error
}

func (r *errorRow) Scan(dest ...any) error {
	return r.err
}
