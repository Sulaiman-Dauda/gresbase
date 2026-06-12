package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/gresbase/gresbase/internal/query"
)

const (
	aggregateMaxFuncs   = 10
	aggregateMaxGroupBy = 5
	aggregateMaxLimit   = 1000
	aggregateDefLimit   = 100
)

var aggregateSpecRe = regexp.MustCompile(`^(count|sum|avg|min|max)(?::([A-Za-z_][A-Za-z0-9_]*))?$`)

// RecordsAggregate handles GET /api/v1/records/{collection}/aggregate.
//
//	?aggregate=count,sum:amount,avg:price&groupBy=status&filter=...&sort=-count&limit=100
//
// Read access is governed by the collection list rule, exactly like RecordsList:
// the resolved rule is compiled into the WHERE clause so aggregates never count
// rows the requester could not list.
func (h *Handlers) RecordsAggregate(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}
	if !requireAPIKeyPermission(w, r, "records.read") {
		return
	}

	ruleFilter, allowed := h.evaluateRuleWhere(r, coll.ListRule, nil)
	if !allowed {
		writeError(w, 403, "Access denied")
		return
	}

	q := r.URL.Query()

	fieldTypes := map[string]collection.FieldType{
		"id":         collection.FieldText,
		"created_at": collection.FieldDate,
		"updated_at": collection.FieldDate,
	}
	for _, f := range coll.Schema {
		fieldTypes[f.Name] = f.Type
	}

	aggSpecs := splitCommaParam(q.Get("aggregate"))
	if len(aggSpecs) == 0 {
		writeError(w, 400, "Missing aggregate parameter (e.g. aggregate=count,sum:amount)")
		return
	}
	if len(aggSpecs) > aggregateMaxFuncs {
		writeError(w, 400, fmt.Sprintf("Too many aggregate functions (max %d)", aggregateMaxFuncs))
		return
	}

	var selectCols []string
	aliases := map[string]bool{}
	for _, spec := range aggSpecs {
		m := aggregateSpecRe.FindStringSubmatch(spec)
		if m == nil {
			writeError(w, 400, "Invalid aggregate spec "+strconv.Quote(spec)+": use count, sum:field, avg:field, min:field or max:field")
			return
		}
		fn, field := m[1], m[2]
		if fn == "count" {
			if field != "" {
				writeError(w, 400, "count takes no field; use aggregate=count")
				return
			}
			selectCols = append(selectCols, `COUNT(*) AS "count"`)
			aliases["count"] = true
			continue
		}
		if field == "" {
			writeError(w, 400, fn+" requires a field (e.g. "+fn+":amount)")
			return
		}
		ft, ok := fieldTypes[field]
		if !ok {
			writeError(w, 400, "Unknown field "+strconv.Quote(field))
			return
		}
		if (fn == "sum" || fn == "avg") && ft != collection.FieldNumber {
			writeError(w, 400, fn+" requires a number field, "+strconv.Quote(field)+" is "+string(ft))
			return
		}
		alias := fn + "_" + field
		selectCols = append(selectCols, fmt.Sprintf("%s(%s) AS %s", strings.ToUpper(fn), query.QuoteIdent(field), query.QuoteIdent(alias)))
		aliases[alias] = true
	}

	groupFields := splitCommaParam(q.Get("groupBy"))
	if len(groupFields) > aggregateMaxGroupBy {
		writeError(w, 400, fmt.Sprintf("Too many groupBy fields (max %d)", aggregateMaxGroupBy))
		return
	}
	groupSet := map[string]bool{}
	var groupCols []string
	for _, field := range groupFields {
		if _, ok := fieldTypes[field]; !ok {
			writeError(w, 400, "Unknown groupBy field "+strconv.Quote(field))
			return
		}
		groupSet[field] = true
		groupCols = append(groupCols, query.QuoteIdent(field))
	}

	whereSQL := "TRUE"
	var whereArgs []any
	appendWhere := func(expr string) error {
		parsed, err := filter.ParseFilter(expr)
		if err != nil || parsed == nil {
			if err == nil {
				return nil
			}
			return err
		}
		sql, args, err := filter.FilterToSQLOffset(parsed, nil, len(whereArgs)+1)
		if err != nil {
			return err
		}
		if whereSQL == "TRUE" {
			whereSQL = sql
		} else {
			whereSQL = "(" + whereSQL + ") AND (" + sql + ")"
		}
		whereArgs = append(whereArgs, args...)
		return nil
	}
	if ruleFilter != "" {
		if err := appendWhere(ruleFilter); err != nil {
			// Rule that fails to compile must fail closed.
			writeError(w, 403, "Access denied")
			return
		}
	}
	if f := q.Get("filter"); strings.TrimSpace(f) != "" {
		if err := appendWhere(f); err != nil {
			writeError(w, 400, "Invalid filter: "+err.Error())
			return
		}
	}

	var orderParts []string
	for _, s := range splitCommaParam(q.Get("sort")) {
		dir := "ASC"
		if strings.HasPrefix(s, "-") {
			dir = "DESC"
			s = s[1:]
		} else if strings.HasPrefix(s, "+") {
			s = s[1:]
		}
		if !aliases[s] && !groupSet[s] {
			writeError(w, 400, "Sort field "+strconv.Quote(s)+" must be an aggregate alias or a groupBy field")
			return
		}
		orderParts = append(orderParts, query.QuoteIdent(s)+" "+dir)
	}

	limit := aggregateDefLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, aggregateMaxLimit)
		}
	}

	cols := append(append([]string{}, groupCols...), selectCols...)
	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(cols, ", "), query.QuoteIdent(coll.Name), whereSQL)
	if len(groupCols) > 0 {
		sql += " GROUP BY " + strings.Join(groupCols, ", ")
	}
	if len(orderParts) > 0 {
		sql += " ORDER BY " + strings.Join(orderParts, ", ")
	}
	sql = fmt.Sprintf("SELECT row_to_json(t) FROM (%s LIMIT %d) t", sql, limit)

	rows, err := h.app.DB().ReadQuery(r.Context(), sql, whereArgs...)
	if err != nil {
		writeInternalError(w, "Aggregate query failed", err)
		return
	}
	defer rows.Close()

	items := []map[string]any{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		items = append(items, item)
	}

	writeOK(w, map[string]any{"items": items})
}

func splitCommaParam(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
