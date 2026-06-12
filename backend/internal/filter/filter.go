// Package filter provides a powerful expression-based filter engine for collections.
// It supports parsing, validating, and translating filter expressions to SQL WHERE clauses,
// as well as evaluating expressions against in-memory records.
//
// Filter syntax examples:
//
//	title = "hello"
//	status = "active" && created > "2024-01-01"
//	tags ?= "go" || tags ?= "rust"
//	email ~ "@gmail.com"
//	role != "admin"
//	age >= 18 && age <= 65
package filter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// AST Types
// ---------------------------------------------------------------------------

// Kind represents the type of an AST node.
type Kind int

const (
	KindBinOp  Kind = iota // AND, OR
	KindCompOp             // =, !=, ~, !~, >, <, >=, <=, ?=, ?!=, ?~, ?!~
	KindIdent              // field identifier
	KindValue              // literal value (string, number, bool, null)
	KindParen              // parenthesized sub-expression
	KindNot                // unary NOT
)

func (k Kind) String() string {
	switch k {
	case KindBinOp:
		return "BinOp"
	case KindCompOp:
		return "CompOp"
	case KindIdent:
		return "Ident"
	case KindValue:
		return "Value"
	case KindParen:
		return "Paren"
	case KindNot:
		return "Not"
	default:
		return fmt.Sprintf("Kind(%d)", k)
	}
}

// Op represents a binary or comparison operator.
type Op string

const (
	OpAnd    Op = "&&"
	OpOr     Op = "||"
	OpEq     Op = "="
	OpNeq    Op = "!="
	OpGt     Op = ">"
	OpGte    Op = ">="
	OpLt     Op = "<"
	OpLte    Op = "<="
	OpLike   Op = "~"   // contains (LIKE %value%)
	OpNLike  Op = "!~"  // not contains
	OpIn     Op = "?="  // value in array/JSON
	OpNIn    Op = "?!=" // value not in array/JSON
	OpILike  Op = "?~"  // array contains like
	OpNILike Op = "?!~" // array not contains like
)

// Precedence returns operator precedence (higher = binds tighter).
func (op Op) Precedence() int {
	switch op {
	case OpAnd:
		return 1
	case OpOr:
		return 0
	default:
		return 2 // comparison ops
	}
}

// Expr is an AST node in a filter expression.
type Expr struct {
	Kind  Kind
	Op    Op     // for BinOp and CompOp
	Ident string // for Ident
	Value any    // for Value
	Left  *Expr  // for BinOp, CompOp, Not, Paren
	Right *Expr  // for BinOp, CompOp
}

// String returns a debug representation of the expression tree.
func (e *Expr) String() string {
	if e == nil {
		return "<nil>"
	}
	switch e.Kind {
	case KindBinOp, KindCompOp:
		return fmt.Sprintf("(%s %s %s)", e.Left, e.Op, e.Right)
	case KindIdent:
		return e.Ident
	case KindValue:
		return fmt.Sprintf("%v", e.Value)
	case KindParen:
		return fmt.Sprintf("(%s)", e.Left)
	case KindNot:
		return fmt.Sprintf("!%s", e.Left)
	default:
		return fmt.Sprintf("<%s>", e.Kind)
	}
}

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokBool
	tokNull
	tokAnd    // &&
	tokOr     // ||
	tokEq     // =
	tokNeq    // !=
	tokGt     // >
	tokGte    // >=
	tokLt     // <
	tokLte    // <=
	tokLike   // ~
	tokNLike  // !~
	tokIn     // ?=
	tokNIn    // ?!=
	tokILike  // ?~
	tokNILike // ?!~
	tokLParen // (
	tokRParen // )
	tokNot    // !
	tokComma  // ,
)

type token struct {
	kind  tokenKind
	value string
	pos   int
}

type tokenizer struct {
	input []rune
	pos   int
}

func newTokenizer(s string) *tokenizer {
	return &tokenizer{input: []rune(s), pos: 0}
}

func (t *tokenizer) next() token {
	t.skipWhitespace()
	if t.pos >= len(t.input) {
		return token{kind: tokEOF}
	}

	ch := t.input[t.pos]

	// Three-character operators
	if t.pos+2 < len(t.input) {
		three := string(t.input[t.pos : t.pos+3])
		switch three {
		case "?!=":
			t.pos += 3
			return token{kind: tokNIn, value: "?!=", pos: t.pos}
		case "?!~":
			t.pos += 3
			return token{kind: tokNILike, value: "?!~", pos: t.pos}
		}
	}

	// Two-character operators
	if t.pos+1 < len(t.input) {
		two := string(t.input[t.pos : t.pos+2])
		switch two {
		case "&&":
			t.pos += 2
			return token{kind: tokAnd, value: "&&", pos: t.pos}
		case "||":
			t.pos += 2
			return token{kind: tokOr, value: "||", pos: t.pos}
		case "!=":
			t.pos += 2
			return token{kind: tokNeq, value: "!=", pos: t.pos}
		case ">=":
			t.pos += 2
			return token{kind: tokGte, value: ">=", pos: t.pos}
		case "<=":
			t.pos += 2
			return token{kind: tokLte, value: "<=", pos: t.pos}
		case "!~":
			t.pos += 2
			return token{kind: tokNLike, value: "!~", pos: t.pos}
		case "?=":
			t.pos += 2
			return token{kind: tokIn, value: "?=", pos: t.pos}
		case "?~":
			t.pos += 2
			return token{kind: tokILike, value: "?~", pos: t.pos}
		case "?!=":
			t.pos += 2
			return token{kind: tokNIn, value: "?!=", pos: t.pos}
		case "?!~":
			t.pos += 2
			return token{kind: tokNILike, value: "?!~", pos: t.pos}
		}
	}

	switch ch {
	case '=':
		t.pos++
		return token{kind: tokEq, value: "=", pos: t.pos}
	case '>':
		t.pos++
		return token{kind: tokGt, value: ">", pos: t.pos}
	case '<':
		t.pos++
		return token{kind: tokLt, value: "<", pos: t.pos}
	case '~':
		t.pos++
		return token{kind: tokLike, value: "~", pos: t.pos}
	case '(':
		t.pos++
		return token{kind: tokLParen, value: "(", pos: t.pos}
	case ')':
		t.pos++
		return token{kind: tokRParen, value: ")", pos: t.pos}
	case '!':
		t.pos++
		return token{kind: tokNot, value: "!", pos: t.pos}
	case ',':
		t.pos++
		return token{kind: tokComma, value: ",", pos: t.pos}
	case '"':
		return t.readString()
	case '\'':
		return t.readSingleQuoteString()
	}

	// Numbers
	if ch >= '0' && ch <= '9' || ch == '-' && t.pos+1 < len(t.input) && t.input[t.pos+1] >= '0' && t.input[t.pos+1] <= '9' {
		return t.readNumber()
	}

	// Identifiers
	return t.readIdent()
}

func (t *tokenizer) skipWhitespace() {
	for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\n' || t.input[t.pos] == '\r') {
		t.pos++
	}
}

func (t *tokenizer) readString() token {
	t.pos++ // skip opening "
	start := t.pos
	for t.pos < len(t.input) && t.input[t.pos] != '"' {
		if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
			t.pos++
		}
		t.pos++
	}
	value := string(t.input[start:t.pos])
	if t.pos < len(t.input) {
		t.pos++ // skip closing "
	}
	return token{kind: tokString, value: value}
}

func (t *tokenizer) readSingleQuoteString() token {
	t.pos++ // skip opening '
	start := t.pos
	for t.pos < len(t.input) && t.input[t.pos] != '\'' {
		t.pos++
	}
	value := string(t.input[start:t.pos])
	if t.pos < len(t.input) {
		t.pos++ // skip closing '
	}
	return token{kind: tokString, value: value}
}

func (t *tokenizer) readNumber() token {
	start := t.pos
	if t.pos < len(t.input) && t.input[t.pos] == '-' {
		t.pos++
	}
	for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
		t.pos++
	}
	if t.pos < len(t.input) && t.input[t.pos] == '.' {
		t.pos++
		for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
			t.pos++
		}
	}
	return token{kind: tokNumber, value: string(t.input[start:t.pos])}
}

func (t *tokenizer) readIdent() token {
	start := t.pos
	for t.pos < len(t.input) {
		ch := t.input[t.pos]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '.' || ch == '@' {
			t.pos++
		} else {
			break
		}
	}
	value := string(t.input[start:t.pos])

	// Keyword detection
	switch strings.ToLower(value) {
	case "true":
		return token{kind: tokBool, value: "true"}
	case "false":
		return token{kind: tokBool, value: "false"}
	case "null", "nil":
		return token{kind: tokNull, value: "null"}
	case "and":
		return token{kind: tokAnd, value: "&&"}
	case "or":
		return token{kind: tokOr, value: "||"}
	}

	return token{kind: tokIdent, value: value}
}

// ---------------------------------------------------------------------------
// Parser (Pratt parser for operator precedence)
// ---------------------------------------------------------------------------

type parser struct {
	tok  *tokenizer
	cur  token
	next token
	err  error
}

func newParser(s string) *parser {
	tok := newTokenizer(s)
	p := &parser{tok: tok}
	p.cur = tok.next()
	p.next = tok.next()
	return p
}

func (p *parser) advance() {
	p.cur = p.next
	p.next = p.tok.next()
}

func (p *parser) parse() (*Expr, error) {
	expr := p.parseOr()
	if p.err != nil {
		return nil, p.err
	}
	if p.cur.kind != tokEOF {
		return nil, fmt.Errorf("unexpected token: %v at position %d", p.cur.value, p.cur.pos)
	}
	return expr, nil
}

func (p *parser) parseOr() *Expr {
	left := p.parseAnd()
	for p.cur.kind == tokOr {
		op := OpOr
		p.advance()
		right := p.parseAnd()
		left = &Expr{Kind: KindBinOp, Op: op, Left: left, Right: right}
	}
	return left
}

func (p *parser) parseAnd() *Expr {
	left := p.parseNot()
	for p.cur.kind == tokAnd {
		op := OpAnd
		p.advance()
		right := p.parseNot()
		left = &Expr{Kind: KindBinOp, Op: op, Left: left, Right: right}
	}
	return left
}

func (p *parser) parseNot() *Expr {
	if p.cur.kind == tokNot {
		p.advance()
		expr := p.parseComparison()
		return &Expr{Kind: KindNot, Left: expr}
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() *Expr {
	left := p.parsePrimary()

	switch p.cur.kind {
	case tokEq, tokNeq, tokGt, tokGte, tokLt, tokLte, tokLike, tokNLike, tokIn, tokNIn, tokILike, tokNILike:
		op := p.tokenToOp()
		p.advance()
		right := p.parsePrimary()
		return &Expr{Kind: KindCompOp, Op: op, Left: left, Right: right}
	}

	return left
}

func (p *parser) tokenToOp() Op {
	switch p.cur.kind {
	case tokEq:
		return OpEq
	case tokNeq:
		return OpNeq
	case tokGt:
		return OpGt
	case tokGte:
		return OpGte
	case tokLt:
		return OpLt
	case tokLte:
		return OpLte
	case tokLike:
		return OpLike
	case tokNLike:
		return OpNLike
	case tokIn:
		return OpIn
	case tokNIn:
		return OpNIn
	case tokILike:
		return OpILike
	case tokNILike:
		return OpNILike
	default:
		return OpEq
	}
}

func (p *parser) parsePrimary() *Expr {
	switch p.cur.kind {
	case tokIdent:
		expr := &Expr{Kind: KindIdent, Ident: p.cur.value}
		p.advance()
		return expr
	case tokString:
		expr := &Expr{Kind: KindValue, Value: p.cur.value}
		p.advance()
		return expr
	case tokNumber:
		v, _ := strconv.ParseFloat(p.cur.value, 64)
		expr := &Expr{Kind: KindValue, Value: v}
		p.advance()
		return expr
	case tokBool:
		v := p.cur.value == "true"
		expr := &Expr{Kind: KindValue, Value: v}
		p.advance()
		return expr
	case tokNull:
		expr := &Expr{Kind: KindValue, Value: nil}
		p.advance()
		return expr
	case tokLParen:
		p.advance()
		expr := p.parseOr()
		if p.cur.kind != tokRParen {
			p.err = fmt.Errorf("expected closing parenthesis at position %d", p.cur.pos)
			return nil
		}
		p.advance()
		return &Expr{Kind: KindParen, Left: expr}
	default:
		p.err = fmt.Errorf("unexpected token: %v at position %d", p.cur.value, p.cur.pos)
		return nil
	}
}

// ---------------------------------------------------------------------------
// SQL Generator (translates AST to PostgreSQL WHERE clause)
// ---------------------------------------------------------------------------

// SQLBuilder converts a filter expression to a SQL WHERE clause.
type SQLBuilder struct {
	// placeholders is the current parameter counter
	placeholders int
	// startPlaceholder is the first parameter number to emit ($1 by default);
	// callers combining multiple compiled fragments into one statement must
	// offset each fragment past the args already collected.
	startPlaceholder int
	// params collects query parameters
	params []any
	// fieldMapping maps filter field names to SQL column expressions
	fieldMapping map[string]string
}

// NewSQLBuilder creates a SQL builder with default field mapping (direct column names).
func NewSQLBuilder() *SQLBuilder {
	return &SQLBuilder{
		placeholders:     1,
		startPlaceholder: 1,
		params:           make([]any, 0),
		fieldMapping:     make(map[string]string),
	}
}

// WithParamOffset sets the first placeholder number emitted by Build.
func (b *SQLBuilder) WithParamOffset(start int) *SQLBuilder {
	if start > 0 {
		b.startPlaceholder = start
	}
	return b
}

// WithFieldMapping sets a custom field name to SQL expression mapping.
func (b *SQLBuilder) WithFieldMapping(mapping map[string]string) *SQLBuilder {
	b.fieldMapping = mapping
	return b
}

// Build generates a SQL WHERE clause from a parsed expression.
// Returns the SQL fragment and parameter values.
func (b *SQLBuilder) Build(expr *Expr) (string, []any, error) {
	if expr == nil {
		return "TRUE", nil, nil
	}

	b.params = b.params[:0]
	b.placeholders = b.startPlaceholder

	sql, err := b.buildNode(expr)
	if err != nil {
		return "", nil, err
	}

	if sql == "" {
		return "TRUE", nil, nil
	}

	return sql, b.params, nil
}

func (b *SQLBuilder) buildNode(e *Expr) (string, error) {
	if e == nil {
		return "", fmt.Errorf("nil expression node")
	}

	switch e.Kind {
	case KindBinOp:
		left, err := b.buildNode(e.Left)
		if err != nil {
			return "", err
		}
		right, err := b.buildNode(e.Right)
		if err != nil {
			return "", err
		}
		switch e.Op {
		case OpAnd:
			return fmt.Sprintf("(%s AND %s)", left, right), nil
		case OpOr:
			return fmt.Sprintf("(%s OR %s)", left, right), nil
		default:
			return "", fmt.Errorf("unknown binary operator: %s", e.Op)
		}

	case KindCompOp:
		return b.buildComparison(e)

	case KindParen:
		inner, err := b.buildNode(e.Left)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s)", inner), nil

	case KindNot:
		inner, err := b.buildNode(e.Left)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("NOT (%s)", inner), nil

	default:
		return "", fmt.Errorf("cannot build SQL for node kind: %s", e.Kind)
	}
}

func (b *SQLBuilder) buildComparison(e *Expr) (string, error) {
	if e.Left == nil || e.Left.Kind != KindIdent {
		return "", fmt.Errorf("left side of comparison must be a field identifier")
	}

	fieldName := e.Left.Ident
	colExpr := b.resolveColumn(fieldName)

	value := e.Right.Value
	if e.Right == nil {
		value = nil
	} else if e.Right.Kind == KindValue {
		value = e.Right.Value
	} else if e.Right.Kind == KindIdent {
		// comparing two fields
		otherCol := b.resolveColumn(e.Right.Ident)
		return b.buildFieldToField(colExpr, e.Op, otherCol), nil
	}

	switch e.Op {
	case OpEq:
		if value == nil {
			return fmt.Sprintf("%s IS NULL", colExpr), nil
		}
		p := b.addParam(value)
		return fmt.Sprintf("%s = $%d", colExpr, p), nil

	case OpNeq:
		if value == nil {
			return fmt.Sprintf("%s IS NOT NULL", colExpr), nil
		}
		p := b.addParam(value)
		return fmt.Sprintf("%s != $%d", colExpr, p), nil

	case OpGt:
		p := b.addParam(value)
		return fmt.Sprintf("%s > $%d", colExpr, p), nil

	case OpGte:
		p := b.addParam(value)
		return fmt.Sprintf("%s >= $%d", colExpr, p), nil

	case OpLt:
		p := b.addParam(value)
		return fmt.Sprintf("%s < $%d", colExpr, p), nil

	case OpLte:
		p := b.addParam(value)
		return fmt.Sprintf("%s <= $%d", colExpr, p), nil

	case OpLike:
		p := b.addParam(fmt.Sprintf("%%%v%%", value))
		return fmt.Sprintf("%s ILIKE $%d", colExpr, p), nil

	case OpNLike:
		p := b.addParam(fmt.Sprintf("%%%v%%", value))
		return fmt.Sprintf("%s NOT ILIKE $%d", colExpr, p), nil

	case OpIn:
		p := b.addParam(fmt.Sprintf("%v", value))
		return fmt.Sprintf("%s::jsonb @> to_jsonb($%d::text)", colExpr, p), nil

	case OpNIn:
		p := b.addParam(fmt.Sprintf("%v", value))
		return fmt.Sprintf("NOT (%s::jsonb @> to_jsonb($%d::text))", colExpr, p), nil

	case OpILike:
		p := b.addParam(fmt.Sprintf("%%%v%%", value))
		return fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s::jsonb) elem WHERE elem ILIKE $%d)", colExpr, p), nil

	case OpNILike:
		p := b.addParam(fmt.Sprintf("%%%v%%", value))
		return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s::jsonb) elem WHERE elem ILIKE $%d)", colExpr, p), nil

	default:
		return "", fmt.Errorf("unknown comparison operator: %s", e.Op)
	}
}

func (b *SQLBuilder) buildFieldToField(col string, op Op, otherCol string) string {
	switch op {
	case OpEq:
		return fmt.Sprintf("%s = %s", col, otherCol)
	case OpNeq:
		return fmt.Sprintf("%s != %s", col, otherCol)
	case OpGt:
		return fmt.Sprintf("%s > %s", col, otherCol)
	case OpGte:
		return fmt.Sprintf("%s >= %s", col, otherCol)
	case OpLt:
		return fmt.Sprintf("%s < %s", col, otherCol)
	case OpLte:
		return fmt.Sprintf("%s <= %s", col, otherCol)
	default:
		return "TRUE"
	}
}

func (b *SQLBuilder) resolveColumn(fieldName string) string {
	if b.fieldMapping != nil {
		if mapped, ok := b.fieldMapping[fieldName]; ok {
			return mapped
		}
	}
	// Default: quote and prefix with table alias if present
	return QuoteIdent(fieldName)
}

func (b *SQLBuilder) addParam(v any) int {
	b.params = append(b.params, v)
	n := b.placeholders
	b.placeholders++
	return n
}

// QuoteIdent quotes a PostgreSQL identifier.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// ---------------------------------------------------------------------------
// Rule Evaluator (evaluates expressions against in-memory records)
// ---------------------------------------------------------------------------

// RuleEvaluator evaluates filter expressions against record data in Go.
type RuleEvaluator struct {
	// Resolver resolves special @request and @collection variables.
	Resolver func(key string) (any, error)
}

// NewRuleEvaluator creates a rule evaluator with no special resolver.
func NewRuleEvaluator() *RuleEvaluator {
	return &RuleEvaluator{}
}

// Evaluate checks if a record matches the given filter expression.
func (e *RuleEvaluator) Evaluate(expr *Expr, record map[string]any) (bool, error) {
	if expr == nil {
		return true, nil
	}
	return e.evalNode(expr, record)
}

func (e *RuleEvaluator) evalNode(node *Expr, record map[string]any) (bool, error) {
	switch node.Kind {
	case KindBinOp:
		left, err := e.evalNode(node.Left, record)
		if err != nil {
			return false, err
		}
		switch node.Op {
		case OpAnd:
			if !left {
				return false, nil
			}
			return e.evalNode(node.Right, record)
		case OpOr:
			if left {
				return true, nil
			}
			return e.evalNode(node.Right, record)
		}

	case KindNot:
		v, err := e.evalNode(node.Left, record)
		if err != nil {
			return false, err
		}
		return !v, nil

	case KindCompOp:
		return e.evalComparison(node, record)

	case KindParen:
		return e.evalNode(node.Left, record)
	}

	return false, fmt.Errorf("cannot evaluate node kind: %s", node.Kind)
}

func (e *RuleEvaluator) evalComparison(node *Expr, record map[string]any) (bool, error) {
	if node.Left == nil || node.Left.Kind != KindIdent {
		return false, fmt.Errorf("expected field identifier on left side of comparison")
	}

	fieldName := node.Left.Ident
	var fieldVal any

	// Handle special @request.xxx references
	if strings.HasPrefix(fieldName, "@") {
		if e.Resolver != nil {
			var err error
			fieldVal, err = e.Resolver(fieldName)
			if err != nil {
				return false, err
			}
		}
	} else {
		var ok bool
		fieldVal, ok = record[fieldName]
		if !ok {
			// Field not present — treat as nil
			fieldVal = nil
		}
	}

	rightVal := node.Right.Value
	if node.Right.Kind == KindIdent {
		if strings.HasPrefix(node.Right.Ident, "@") {
			if e.Resolver != nil {
				var err error
				rightVal, err = e.Resolver(node.Right.Ident)
				if err != nil {
					return false, err
				}
			} else {
				rightVal = nil
			}
		} else {
			rightVal = record[node.Right.Ident]
		}
	}

	return compare(fieldVal, node.Op, rightVal)
}

func compare(left any, op Op, right any) (bool, error) {
	switch op {
	case OpEq:
		return isEqual(left, right), nil
	case OpNeq:
		return !isEqual(left, right), nil
	case OpGt, OpGte, OpLt, OpLte:
		return compareNumbers(left, op, right)
	case OpLike:
		return stringContains(left, right)
	case OpNLike:
		v, err := stringContains(left, right)
		return !v, err
	case OpIn:
		return arrayContains(left, right)
	case OpNIn:
		v, err := arrayContains(left, right)
		return !v, err
	default:
		return false, fmt.Errorf("unsupported operator: %s", op)
	}
}

func isEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	sa := fmt.Sprintf("%v", a)
	sb := fmt.Sprintf("%v", b)
	return sa == sb
}

func compareNumbers(a any, op Op, b any) (bool, error) {
	fa, oka := toFloat64(a)
	fb, okb := toFloat64(b)
	if !oka || !okb {
		// Try string comparison
		sa := fmt.Sprintf("%v", a)
		sb := fmt.Sprintf("%v", b)
		switch op {
		case OpGt:
			return sa > sb, nil
		case OpGte:
			return sa >= sb, nil
		case OpLt:
			return sa < sb, nil
		case OpLte:
			return sa <= sb, nil
		}
		return false, nil
	}

	switch op {
	case OpGt:
		return fa > fb, nil
	case OpGte:
		return fa >= fb, nil
	case OpLt:
		return fa < fb, nil
	case OpLte:
		return fa <= fb, nil
	default:
		return false, nil
	}
}

func stringContains(a, b any) (bool, error) {
	sa := fmt.Sprintf("%v", a)
	sb := fmt.Sprintf("%v", b)
	return strings.Contains(strings.ToLower(sa), strings.ToLower(sb)), nil
}

func arrayContains(haystack, needle any) (bool, error) {
	ns := fmt.Sprintf("%v", needle)

	switch v := haystack.(type) {
	case []any:
		for _, item := range v {
			if fmt.Sprintf("%v", item) == ns {
				return true, nil
			}
		}
	case []string:
		for _, item := range v {
			if item == ns {
				return true, nil
			}
		}
	case string:
		// Try parsing as JSON array
		var arr []any
		if err := json.Unmarshal([]byte(v), &arr); err == nil {
			for _, item := range arr {
				if fmt.Sprintf("%v", item) == ns {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ParseFilter parses a filter expression string into an AST.
func ParseFilter(expr string) (*Expr, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, nil
	}
	return newParser(expr).parse()
}

// FilterToSQL converts a filter expression to a SQL WHERE clause with parameters.
func FilterToSQL(expr *Expr, fieldMapping map[string]string) (string, []any, error) {
	return FilterToSQLOffset(expr, fieldMapping, 1)
}

// FilterToSQLOffset converts a filter expression to SQL with placeholders
// starting at $paramOffset, for combining with already-parameterized clauses.
func FilterToSQLOffset(expr *Expr, fieldMapping map[string]string, paramOffset int) (string, []any, error) {
	b := NewSQLBuilder().WithParamOffset(paramOffset)
	if fieldMapping != nil {
		b.WithFieldMapping(fieldMapping)
	}
	return b.Build(expr)
}

// FilterMatches checks if a record matches a filter expression.
func FilterMatches(expr *Expr, record map[string]any) (bool, error) {
	e := NewRuleEvaluator()
	return e.Evaluate(expr, record)
}

// FilterMatchesWithResolver checks if a record matches with a custom variable resolver.
func FilterMatchesWithResolver(expr *Expr, record map[string]any, resolver func(key string) (any, error)) (bool, error) {
	e := &RuleEvaluator{Resolver: resolver}
	return e.Evaluate(expr, record)
}

// ValidateFields checks that all field references in the expression are valid.
func ValidateFields(expr *Expr, validFields []string) error {
	if expr == nil {
		return nil
	}
	return validateFieldsNode(expr, validFields)
}

func validateFieldsNode(node *Expr, validFields []string) error {
	switch node.Kind {
	case KindBinOp:
		if err := validateFieldsNode(node.Left, validFields); err != nil {
			return err
		}
		return validateFieldsNode(node.Right, validFields)
	case KindCompOp:
		if node.Left != nil && node.Left.Kind == KindIdent {
			fieldName := node.Left.Ident
			if strings.HasPrefix(fieldName, "@") {
				return nil
			}
			for _, f := range validFields {
				if f == fieldName {
					return nil
				}
			}
			return fmt.Errorf("unknown field: %q", fieldName)
		}
	case KindParen, KindNot:
		return validateFieldsNode(node.Left, validFields)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper for building combined WHERE clauses from multiple rules
// ---------------------------------------------------------------------------

// BuildRuleSQL constructs a SQL WHERE clause from a rule expression string.
// It handles empty rules and returns parameterized SQL.
func BuildRuleSQL(rule string, tableAlias string) (string, []any, error) {
	if strings.TrimSpace(rule) == "" {
		return "TRUE", nil, nil
	}

	expr, err := ParseFilter(rule)
	if err != nil {
		return "", nil, fmt.Errorf("invalid filter rule: %w", err)
	}

	return FilterToSQL(expr, nil)
}

// CombineRules combines multiple rule SQL fragments with AND.
func CombineRules(rules ...string) string {
	parts := make([]string, 0)
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r != "" && r != "TRUE" {
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return "TRUE"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ") AND (") + ")"
}

// Ensure imports are used
var _ = time.Now
var _ = regexp.Compile
