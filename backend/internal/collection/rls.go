package collection

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/filter"
)

// This file compiles collection access rules into PostgreSQL row-level
// security policies so the locked-by-default rule model is also enforced
// inside Postgres for direct connections via a restricted role.
//
// Parser choice: rules are parsed with filter.ParseFilter — the exact parser
// the runtime evaluator uses — and the resulting AST is translated here.
// filter.SQLBuilder cannot be reused directly because policies must be
// self-contained SQL (no $N parameters) and because @request.* macros must
// become current_setting() expressions instead of pre-resolved literal
// values. The operator translation in compileComparison mirrors
// filter.SQLBuilder.buildComparison one-to-one (ILIKE for ~, jsonb @> for
// ?=, IS NULL for = null, ...) so policy semantics match the API layer.

// DefaultRLSRole is the restricted role policies target when RLSOptions.Role
// is empty.
const DefaultRLSRole = "gresbase_client"

// RLSPolicy is a single compiled CREATE POLICY statement (no trailing
// semicolon).
type RLSPolicy struct {
	Collection string
	Operation  string // SELECT, INSERT, UPDATE, DELETE
	Name       string // unquoted policy name: gresbase_<collection>_<op>
	SQL        string
}

// RLSWarning reports a semantic caveat produced during compilation.
type RLSWarning struct {
	Collection string
	Operation  string
	Message    string
}

// RLSOptions configures policy compilation.
type RLSOptions struct {
	// Role the policies apply to (TO <role>). Defaults to DefaultRLSRole.
	Role string
	// ForceLockUnsupported emits USING (false) (with an explanatory SQL
	// comment) instead of returning an error when a rule uses placeholders
	// that cannot be expressed in RLS (e.g. @request.body.*).
	ForceLockUnsupported bool
}

func (o RLSOptions) role() string {
	if o.Role == "" {
		return DefaultRLSRole
	}
	return o.Role
}

// UnsupportedPlaceholderError is returned when a rule references request
// context that does not exist inside a plain database session (request body,
// query string, headers, HTTP method). Callers must skip the policy (RLS
// default-deny then applies) or force-lock it — never emit a permissive
// fallback.
type UnsupportedPlaceholderError struct {
	Collection   string
	Operation    string
	Placeholders []string
}

func (e *UnsupportedPlaceholderError) Error() string {
	return fmt.Sprintf("rls: collection %q %s rule uses placeholders not expressible in row-level security: %s",
		e.Collection, e.Operation, strings.Join(e.Placeholders, ", "))
}

// rlsMacros maps @request.* placeholders to session GUC reads. missing_ok is
// true so an unset setting yields NULL; NULL comparisons are never TRUE, so
// policies fail closed for sessions that did not set the auth context.
//
// Known divergence (fail-closed direction): the runtime resolves
// @request.auth.id to "" for unauthenticated requests, so `owner !=
// @request.auth.id` passes at the API layer; in SQL the unset GUC is NULL and
// the row stays hidden.
var rlsMacros = map[string]string{
	"@request.auth.id":         "current_setting('gresbase.auth_id', true)",
	"@request.auth.email":      "current_setting('gresbase.auth_email', true)",
	"@request.auth.role":       "current_setting('gresbase.auth_role', true)",
	"@request.auth.verified":   "current_setting('gresbase.auth_verified', true)::boolean",
	"@request.auth.collection": "current_setting('gresbase.auth_collection', true)",
	"@now":                     "now()",
}

// ---------------------------------------------------------------------------
// Expression compiler
// ---------------------------------------------------------------------------

type rlsCompiler struct {
	unsupported []string
}

func (c *rlsCompiler) noteUnsupported(placeholder string) {
	for _, p := range c.unsupported {
		if p == placeholder {
			return
		}
	}
	c.unsupported = append(c.unsupported, placeholder)
}

func (c *rlsCompiler) compile(e *filter.Expr) (string, error) {
	if e == nil {
		return "", fmt.Errorf("nil expression node")
	}
	switch e.Kind {
	case filter.KindBinOp:
		left, err := c.compile(e.Left)
		if err != nil {
			return "", err
		}
		right, err := c.compile(e.Right)
		if err != nil {
			return "", err
		}
		switch e.Op {
		case filter.OpAnd:
			return fmt.Sprintf("(%s AND %s)", left, right), nil
		case filter.OpOr:
			return fmt.Sprintf("(%s OR %s)", left, right), nil
		}
		return "", fmt.Errorf("unknown binary operator: %s", e.Op)
	case filter.KindParen:
		inner, err := c.compile(e.Left)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s)", inner), nil
	case filter.KindNot:
		inner, err := c.compile(e.Left)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("NOT (%s)", inner), nil
	case filter.KindCompOp:
		return c.compileComparison(e)
	default:
		return "", fmt.Errorf("cannot compile node kind %s to RLS", e.Kind)
	}
}

// identSQL resolves an identifier: @-placeholders become current_setting()
// expressions, everything else a quoted column reference. Unknown
// @-placeholders are recorded and a NULL stand-in returned; the caller
// discards the SQL when any placeholder was unsupported.
func (c *rlsCompiler) identSQL(ident string) string {
	if strings.HasPrefix(ident, "@") {
		if sql, ok := rlsMacros[ident]; ok {
			return sql
		}
		c.noteUnsupported(ident)
		return "NULL"
	}
	return database.QuoteIdent(ident)
}

func (c *rlsCompiler) compileComparison(e *filter.Expr) (string, error) {
	if e.Left == nil || e.Left.Kind != filter.KindIdent {
		return "", fmt.Errorf("left side of comparison must be a field identifier")
	}
	left := c.identSQL(e.Left.Ident)

	if e.Right == nil {
		return "", fmt.Errorf("missing right side of comparison")
	}

	switch e.Right.Kind {
	case filter.KindValue:
		return compileValueComparison(left, e.Op, e.Right.Value)
	case filter.KindIdent:
		rightIsMacro := strings.HasPrefix(e.Right.Ident, "@")
		right := c.identSQL(e.Right.Ident)
		return compileExprComparison(left, e.Op, right, rightIsMacro)
	default:
		return "", fmt.Errorf("right side of comparison must be a value or identifier")
	}
}

// compileValueComparison mirrors filter.SQLBuilder.buildComparison with the
// parameter value inlined as an escaped literal.
func compileValueComparison(left string, op filter.Op, value any) (string, error) {
	switch op {
	case filter.OpEq:
		if value == nil {
			return fmt.Sprintf("%s IS NULL", left), nil
		}
		return fmt.Sprintf("%s = %s", left, sqlValue(value)), nil
	case filter.OpNeq:
		if value == nil {
			return fmt.Sprintf("%s IS NOT NULL", left), nil
		}
		return fmt.Sprintf("%s != %s", left, sqlValue(value)), nil
	case filter.OpGt, filter.OpGte, filter.OpLt, filter.OpLte:
		return fmt.Sprintf("%s %s %s", left, op, sqlValue(value)), nil
	case filter.OpLike:
		// Runtime uses ILIKE '%value%' (case-insensitive contains).
		return fmt.Sprintf("%s ILIKE %s", left, sqlString(fmt.Sprintf("%%%v%%", value))), nil
	case filter.OpNLike:
		return fmt.Sprintf("%s NOT ILIKE %s", left, sqlString(fmt.Sprintf("%%%v%%", value))), nil
	case filter.OpIn:
		return fmt.Sprintf("%s::jsonb @> to_jsonb(%s::text)", left, sqlString(fmt.Sprintf("%v", value))), nil
	case filter.OpNIn:
		return fmt.Sprintf("NOT (%s::jsonb @> to_jsonb(%s::text))", left, sqlString(fmt.Sprintf("%v", value))), nil
	case filter.OpILike:
		return fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s::jsonb) elem WHERE elem ILIKE %s)",
			left, sqlString(fmt.Sprintf("%%%v%%", value))), nil
	case filter.OpNILike:
		return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s::jsonb) elem WHERE elem ILIKE %s)",
			left, sqlString(fmt.Sprintf("%%%v%%", value))), nil
	default:
		return "", fmt.Errorf("unknown comparison operator: %s", op)
	}
}

// compileExprComparison handles a column or macro on the right side. Macros
// are value-like (the runtime substitutes them with literals before SQL
// compilation), so contains/array operators are supported by building the
// pattern in SQL. Plain column-to-column is limited to the six ordering
// operators, matching filter.SQLBuilder.buildFieldToField — except that the
// runtime silently degrades other operators to TRUE, which would be a
// permissive policy; here that is a hard error instead.
func compileExprComparison(left string, op filter.Op, right string, rightIsMacro bool) (string, error) {
	switch op {
	case filter.OpEq, filter.OpNeq, filter.OpGt, filter.OpGte, filter.OpLt, filter.OpLte:
		return fmt.Sprintf("%s %s %s", left, op, right), nil
	}
	if !rightIsMacro {
		return "", fmt.Errorf("rls: operator %s is not supported between two columns", op)
	}
	switch op {
	case filter.OpLike:
		return fmt.Sprintf("%s ILIKE ('%%' || %s || '%%')", left, right), nil
	case filter.OpNLike:
		return fmt.Sprintf("%s NOT ILIKE ('%%' || %s || '%%')", left, right), nil
	case filter.OpIn:
		return fmt.Sprintf("%s::jsonb @> to_jsonb(%s::text)", left, right), nil
	case filter.OpNIn:
		return fmt.Sprintf("NOT (%s::jsonb @> to_jsonb(%s::text))", left, right), nil
	case filter.OpILike:
		return fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s::jsonb) elem WHERE elem ILIKE ('%%' || %s || '%%'))",
			left, right), nil
	case filter.OpNILike:
		return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s::jsonb) elem WHERE elem ILIKE ('%%' || %s || '%%'))",
			left, right), nil
	default:
		return "", fmt.Errorf("unknown comparison operator: %s", op)
	}
}

// sqlString escapes a string as a PostgreSQL literal (single-quote doubling).
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func sqlValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return sqlString(v)
	default:
		return sqlString(fmt.Sprintf("%v", v))
	}
}

// compileRuleExpr compiles a rule expression to an inline SQL boolean
// expression. The unsupported slice is non-empty when the rule references
// placeholders that cannot exist in a database session; the SQL is invalid in
// that case and must be discarded.
func compileRuleExpr(rule string) (sql string, unsupported []string, err error) {
	expr, err := filter.ParseFilter(rule)
	if err != nil {
		return "", nil, fmt.Errorf("rls: invalid rule %q: %w", rule, err)
	}
	if expr == nil {
		return "true", nil, nil
	}
	c := &rlsCompiler{}
	sql, err = c.compile(expr)
	if err != nil {
		return "", nil, err
	}
	if len(c.unsupported) > 0 {
		return "", c.unsupported, nil
	}
	return sql, nil, nil
}

// ---------------------------------------------------------------------------
// Policy compilation
// ---------------------------------------------------------------------------

type rlsOpResult struct {
	using       string // empty = clause not used by this operation
	check       string
	unsupported []string
}

func rlsPolicyName(collectionName, op string) string {
	return "gresbase_" + collectionName + "_" + strings.ToLower(op)
}

// ruleBoolExpr applies the tri-state model: nil → locked → false, "" →
// public → true, expression → compiled SQL.
func ruleBoolExpr(rule *string) (string, []string, error) {
	if rule == nil {
		return "false", nil, nil
	}
	if strings.TrimSpace(*rule) == "" {
		return "true", nil, nil
	}
	return compileRuleExpr(*rule)
}

// selectUsingExpr merges ListRule and ViewRule. Postgres has a single SELECT
// policy while Gresbase distinguishes list from view, so the policy is the OR
// of both rules (a nil/locked rule contributes false, i.e. is dropped from
// the OR). Direct SQL readers therefore get the union of list+view access; a
// warning is emitted whenever the two rules differ.
func selectUsingExpr(coll *Collection) (string, []string, error) {
	list, view := coll.ListRule, coll.ViewRule
	if list == nil && view == nil {
		return "false", nil, nil
	}

	var parts []string
	var unsupported []string
	for _, rule := range []*string{list, view} {
		if rule == nil {
			continue
		}
		expr, unsup, err := ruleBoolExpr(rule)
		if err != nil {
			return "", nil, err
		}
		if len(unsup) > 0 {
			unsupported = append(unsupported, unsup...)
			continue
		}
		if expr == "true" {
			return "true", nil, nil
		}
		parts = append(parts, expr)
	}
	if len(unsupported) > 0 {
		return "", dedupeStrings(unsupported), nil
	}
	if len(parts) == 1 {
		return parts[0], nil, nil
	}
	return fmt.Sprintf("(%s OR %s)", parts[0], parts[1]), nil, nil
}

func dedupeStrings(in []string) []string {
	out := in[:0]
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func buildPolicySQL(coll *Collection, opts RLSOptions, operation, using, check string) RLSPolicy {
	name := rlsPolicyName(coll.Name, operation)
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE POLICY %s ON %s FOR %s TO %s",
		database.QuoteIdent(name), database.QuoteIdent(coll.Name), operation, database.QuoteIdent(opts.role()))
	if using != "" {
		fmt.Fprintf(&b, " USING (%s)", using)
	}
	if check != "" {
		fmt.Fprintf(&b, " WITH CHECK (%s)", check)
	}
	return RLSPolicy{Collection: coll.Name, Operation: operation, Name: name, SQL: b.String()}
}

func viewSkipWarning(coll *Collection) RLSWarning {
	return RLSWarning{
		Collection: coll.Name,
		Message:    "view collections are skipped: PostgreSQL row-level security does not apply to views; restrict access to the underlying tables or revoke the view from the client role",
	}
}

// compileCollectionPolicies produces the four operation policies for a base
// or auth collection. Operations whose rules use unsupported placeholders are
// returned as UnsupportedPlaceholderError values (unless
// opts.ForceLockUnsupported, in which case a USING (false) policy with an
// explanatory comment is emitted plus a warning).
func compileCollectionPolicies(coll *Collection, opts RLSOptions) ([]RLSPolicy, []RLSWarning, []*UnsupportedPlaceholderError, error) {
	var policies []RLSPolicy
	var warnings []RLSWarning
	var unsupportedErrs []*UnsupportedPlaceholderError

	selectExpr, selectUnsup, err := selectUsingExpr(coll)
	if err != nil {
		return nil, nil, nil, err
	}
	if !rulePtrEqual(coll.ListRule, coll.ViewRule) {
		warnings = append(warnings, RLSWarning{
			Collection: coll.Name,
			Operation:  "SELECT",
			Message:    "list and view rules differ but PostgreSQL has a single SELECT policy; the policy is the OR of both rules (a locked rule contributes false), so direct SQL readers get the union of list+view access",
		})
	}

	// usesUsing/usesCheck encode the clause shape per operation: SELECT and
	// DELETE take USING only, INSERT takes WITH CHECK only, UPDATE takes
	// both (same expression). The shape must never be derived from the
	// compiled expression being non-empty — an unsupported rule compiles to
	// "" and a CREATE POLICY without clauses defaults to permissive.
	type opSpec struct {
		operation            string
		expr                 string
		usesUsing, usesCheck bool
		unsupported          []string
	}
	specs := []opSpec{
		{operation: "SELECT", expr: selectExpr, usesUsing: true, unsupported: selectUnsup},
	}

	createExpr, createUnsup, err := ruleBoolExpr(coll.CreateRule)
	if err != nil {
		return nil, nil, nil, err
	}
	specs = append(specs, opSpec{operation: "INSERT", expr: createExpr, usesCheck: true, unsupported: createUnsup})

	updateExpr, updateUnsup, err := ruleBoolExpr(coll.UpdateRule)
	if err != nil {
		return nil, nil, nil, err
	}
	specs = append(specs, opSpec{operation: "UPDATE", expr: updateExpr, usesUsing: true, usesCheck: true, unsupported: updateUnsup})

	deleteExpr, deleteUnsup, err := ruleBoolExpr(coll.DeleteRule)
	if err != nil {
		return nil, nil, nil, err
	}
	specs = append(specs, opSpec{operation: "DELETE", expr: deleteExpr, usesUsing: true, unsupported: deleteUnsup})

	for _, spec := range specs {
		expr := spec.expr
		if len(spec.unsupported) > 0 {
			if !opts.ForceLockUnsupported {
				unsupportedErrs = append(unsupportedErrs, &UnsupportedPlaceholderError{
					Collection:   coll.Name,
					Operation:    spec.operation,
					Placeholders: spec.unsupported,
				})
				continue
			}
			// Fail closed: lock the operation rather than approximating it.
			expr = fmt.Sprintf("false /* gresbase: rule uses unsupported placeholders: %s */",
				strings.Join(spec.unsupported, ", "))
			warnings = append(warnings, RLSWarning{
				Collection: coll.Name,
				Operation:  spec.operation,
				Message: fmt.Sprintf("rule uses placeholders not expressible in RLS (%s); policy force-locked to false",
					strings.Join(spec.unsupported, ", ")),
			})
		}
		var using, check string
		if spec.usesUsing {
			using = expr
		}
		if spec.usesCheck {
			check = expr
		}
		policies = append(policies, buildPolicySQL(coll, opts, spec.operation, using, check))
	}

	return policies, warnings, unsupportedErrs, nil
}

func rulePtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// CompileRLS compiles all access rules of a collection into RLS policies.
// View collections produce no policies, only a warning. If any rule uses
// placeholders not expressible in RLS and opts.ForceLockUnsupported is false,
// an *UnsupportedPlaceholderError is returned and no policies are emitted.
func CompileRLS(coll *Collection, opts RLSOptions) ([]RLSPolicy, []RLSWarning, error) {
	if coll == nil {
		return nil, nil, fmt.Errorf("rls: collection is required")
	}
	if coll.Type == TypeView {
		return nil, []RLSWarning{viewSkipWarning(coll)}, nil
	}
	policies, warnings, unsupported, err := compileCollectionPolicies(coll, opts)
	if err != nil {
		return nil, warnings, err
	}
	if len(unsupported) > 0 {
		return nil, warnings, unsupported[0]
	}
	return policies, warnings, nil
}

// ---------------------------------------------------------------------------
// Script generation and application
// ---------------------------------------------------------------------------

// rlsCollectionStatements returns the executable statements (no trailing
// semicolons, no comment-only statements) for one collection. Policies with
// unsupported placeholders are skipped with a warning when not force-locked;
// RLS default-deny then applies for that operation, which is fail closed.
func rlsCollectionStatements(coll *Collection, opts RLSOptions) ([]string, []RLSWarning, error) {
	if coll == nil {
		return nil, nil, nil
	}
	if coll.Type == TypeView {
		return nil, []RLSWarning{viewSkipWarning(coll)}, nil
	}

	policies, warnings, unsupported, err := compileCollectionPolicies(coll, opts)
	if err != nil {
		return nil, warnings, err
	}
	for _, u := range unsupported {
		warnings = append(warnings, RLSWarning{
			Collection: u.Collection,
			Operation:  u.Operation,
			Message: fmt.Sprintf("policy skipped: rule uses placeholders not expressible in RLS (%s); with row-level security enabled and no policy, PostgreSQL denies the operation (fail closed)",
				strings.Join(u.Placeholders, ", ")),
		})
	}

	table := database.QuoteIdent(coll.Name)
	role := database.QuoteIdent(opts.role())
	stmts := []string{
		fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO %s", table, role),
		fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", table),
	}
	// Drop all four gresbase policies (not only the ones recreated) so a rule
	// edit that makes a policy unsupported cannot leave a stale, more
	// permissive policy behind.
	for _, op := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		stmts = append(stmts, fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s",
			database.QuoteIdent(rlsPolicyName(coll.Name, op)), table))
	}
	for _, p := range policies {
		stmts = append(stmts, p.SQL)
	}
	return stmts, warnings, nil
}

// RLSScript renders a full idempotent SQL script (enable RLS + drop/create
// policies + grants) for the given collections.
func RLSScript(colls []*Collection, opts RLSOptions) (string, []RLSWarning, error) {
	role := opts.role()
	var b strings.Builder
	b.WriteString("-- Gresbase row-level security policies (generated).\n")
	b.WriteString("-- Run as the table owner. Idempotent: safe to re-run after rule changes.\n")
	b.WriteString("--\n")
	b.WriteString("-- One-time restricted role setup (run manually, not part of this script):\n")
	fmt.Fprintf(&b, "--   CREATE ROLE %s LOGIN; -- add PASSWORD '...' as needed\n", database.QuoteIdent(role))
	fmt.Fprintf(&b, "--   GRANT USAGE ON SCHEMA public TO %s;\n", database.QuoteIdent(role))
	b.WriteString("--\n")
	b.WriteString("-- Each session must set its auth context before querying, e.g.:\n")
	b.WriteString("--   SELECT set_config('gresbase.auth_id', '<auth record id>', false);\n")
	b.WriteString("--   SELECT set_config('gresbase.auth_email', '<email>', false);\n")
	b.WriteString("-- Unset settings read as NULL, so rules fail closed.\n")

	var warnings []RLSWarning
	for _, coll := range colls {
		if coll == nil {
			continue
		}
		stmts, ws, err := rlsCollectionStatements(coll, opts)
		warnings = append(warnings, ws...)
		if err != nil {
			return "", warnings, err
		}
		if len(stmts) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n-- Collection: %s\n", coll.Name)
		for _, stmt := range stmts {
			b.WriteString(stmt)
			b.WriteString(";\n")
		}
	}
	return b.String(), warnings, nil
}

// ApplyRLS compiles and executes the RLS statements for the given
// collections. Statements are executed one at a time (pgx's extended
// protocol does not accept multi-statement strings; the migration runner
// splits on semicolons for the same reason, but rule expressions may contain
// semicolons inside string literals, so statements are built as a slice here
// instead of split from text).
func (s *Service) ApplyRLS(ctx context.Context, colls []*Collection, opts RLSOptions) ([]RLSWarning, error) {
	runner := s.runnerForContext(ctx)
	var warnings []RLSWarning
	for _, coll := range colls {
		stmts, ws, err := rlsCollectionStatements(coll, opts)
		warnings = append(warnings, ws...)
		if err != nil {
			return warnings, err
		}
		for _, stmt := range stmts {
			if _, err := runner.Exec(ctx, stmt); err != nil {
				return warnings, fmt.Errorf("rls: collection %q: executing %q: %w", coll.Name, stmt, err)
			}
		}
	}
	return warnings, nil
}

// RemoveRLS drops the gresbase_* policies and disables row-level security on
// the given collections' tables.
func (s *Service) RemoveRLS(ctx context.Context, colls []*Collection) error {
	runner := s.runnerForContext(ctx)
	for _, coll := range colls {
		if coll == nil || coll.Type == TypeView {
			continue
		}
		table := database.QuoteIdent(coll.Name)
		for _, op := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			stmt := fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s",
				database.QuoteIdent(rlsPolicyName(coll.Name, op)), table)
			if _, err := runner.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("rls: collection %q: %w", coll.Name, err)
			}
		}
		stmt := fmt.Sprintf("ALTER TABLE %s DISABLE ROW LEVEL SECURITY", table)
		if _, err := runner.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("rls: collection %q: %w", coll.Name, err)
		}
	}
	return nil
}
