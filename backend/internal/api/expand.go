package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/gresbase/gresbase/internal/query"
)

const (
	maxExpandDepth       = 6
	backRelationFetchCap = 1000
)

// applyExpand resolves the ?expand= parameter for a set of records with the
// target collection's access rules enforced: forward relations honor the
// target's view rule, back-relations (`comments_via_post`) honor the target's
// list rule, and locked targets are silently skipped for non-superusers.
// Nested paths (`post.author`, `comments_via_post.user`) expand level by
// level, each level rule-checked, up to maxExpandDepth.
func (h *Handlers) applyExpand(r *http.Request, coll *collection.Collection, records []map[string]any, expand string) {
	if strings.TrimSpace(expand) == "" || len(records) == 0 {
		return
	}
	for _, token := range strings.Split(expand, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		h.expandPath(r, coll, records, strings.Split(token, "."), 0)
	}
}

func (h *Handlers) expandPath(r *http.Request, coll *collection.Collection, records []map[string]any, path []string, depth int) {
	if depth >= maxExpandDepth || len(path) == 0 || len(records) == 0 {
		return
	}
	head := path[0]

	var (
		childColl *collection.Collection
		children  []map[string]any
	)
	if field := collectionSchemaField(coll, head); field != nil && field.Type == collection.FieldRelation {
		childColl, children = h.expandForward(r, records, field)
	} else if target, relField := h.resolveBackRelation(r.Context(), coll, head); target != nil {
		childColl, children = h.expandBack(r, records, target, relField, head)
	} else {
		return
	}

	if len(path) > 1 && childColl != nil {
		h.expandPath(r, childColl, children, path[1:], depth+1)
	}
}

func (h *Handlers) expandForward(r *http.Request, records []map[string]any, field *collection.SchemaField) (*collection.Collection, []map[string]any) {
	target := h.resolveRelationTarget(r.Context(), field)
	if target == nil {
		return nil, nil
	}
	ruleExpr, allowed := h.expandRuleExpr(r, target.ViewRule)
	if !allowed {
		return nil, nil
	}

	idSet := map[string]struct{}{}
	for _, rec := range records {
		for _, id := range query.RelationIDs(rec[field.Name]) {
			idSet[id] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return target, nil
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	related := h.fetchExpandRows(r.Context(),
		fmt.Sprintf("SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE id = ANY($1)) t", query.QuoteIdent(target.Name)),
		ids)
	relatedMap := make(map[string]map[string]any, len(related))
	for _, rec := range related {
		if ruleExpr != nil {
			if ok, err := filter.FilterMatches(ruleExpr, rec); err != nil || !ok {
				continue
			}
		}
		id := strings.TrimSpace(fmt.Sprint(rec["id"]))
		if id == "" {
			continue
		}
		rec["_label"] = query.RelationLabel(rec, field)
		relatedMap[id] = rec
	}

	single := query.RelationMaxSelect(field) <= 1
	var children []map[string]any
	seen := map[string]bool{}
	collect := func(id string, child map[string]any) {
		if !seen[id] {
			seen[id] = true
			children = append(children, child)
		}
	}
	for _, rec := range records {
		ids := query.RelationIDs(rec[field.Name])
		if single {
			if len(ids) > 0 {
				if child, ok := relatedMap[ids[0]]; ok {
					recordExpandMap(rec)[field.Name] = child
					collect(ids[0], child)
				}
			}
			continue
		}
		expanded := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			if child, ok := relatedMap[id]; ok {
				expanded = append(expanded, child)
				collect(id, child)
			}
		}
		if len(expanded) > 0 {
			recordExpandMap(rec)[field.Name] = expanded
		}
	}
	return target, children
}

// resolveBackRelation parses a `targetCollection_via_relationField` expand
// token and returns the target collection and its relation field, provided
// that field actually points back at coll.
func (h *Handlers) resolveBackRelation(ctx context.Context, coll *collection.Collection, token string) (*collection.Collection, *collection.SchemaField) {
	idx := strings.LastIndex(token, "_via_")
	if idx <= 0 {
		return nil, nil
	}
	targetName, fieldName := token[:idx], token[idx+len("_via_"):]
	if fieldName == "" {
		return nil, nil
	}
	target, err := h.app.Collections().GetCollectionByName(ctx, targetName)
	if err != nil {
		return nil, nil
	}
	field := collectionSchemaField(target, fieldName)
	if field == nil || field.Type != collection.FieldRelation {
		return nil, nil
	}
	rel := h.resolveRelationTarget(ctx, field)
	if rel == nil || rel.ID != coll.ID {
		return nil, nil
	}
	return target, field
}

func (h *Handlers) expandBack(r *http.Request, records []map[string]any, target *collection.Collection, relField *collection.SchemaField, token string) (*collection.Collection, []map[string]any) {
	ruleExpr, allowed := h.expandRuleExpr(r, target.ListRule)
	if !allowed {
		return nil, nil
	}

	parentIDs := make([]string, 0, len(records))
	for _, rec := range records {
		if id := strings.TrimSpace(fmt.Sprint(rec["id"])); id != "" && id != "<nil>" {
			parentIDs = append(parentIDs, id)
		}
	}
	if len(parentIDs) == 0 {
		return target, nil
	}

	col := query.QuoteIdent(relField.Name)
	match := col + " = ANY($1)"
	if query.RelationMaxSelect(relField) != 1 {
		match = fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s) rel_id WHERE rel_id = ANY($1))", col)
	}
	sql := fmt.Sprintf(
		"SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE %s ORDER BY created_at DESC LIMIT %d) t",
		query.QuoteIdent(target.Name), match, backRelationFetchCap,
	)
	rows := h.fetchExpandRows(r.Context(), sql, parentIDs)

	buckets := make(map[string][]map[string]any)
	var children []map[string]any
	for _, rec := range rows {
		if ruleExpr != nil {
			if ok, err := filter.FilterMatches(ruleExpr, rec); err != nil || !ok {
				continue
			}
		}
		children = append(children, rec)
		for _, pid := range query.RelationIDs(rec[relField.Name]) {
			buckets[pid] = append(buckets[pid], rec)
		}
	}
	for _, rec := range records {
		id := strings.TrimSpace(fmt.Sprint(rec["id"]))
		if kids := buckets[id]; len(kids) > 0 {
			recordExpandMap(rec)[token] = kids
		}
	}
	return target, children
}

// expandRuleExpr resolves an access rule for expansion: nil rule fails closed
// for non-superusers, an unparsable rule fails closed, an empty (public) or
// superuser context returns a nil expression meaning "no filtering needed".
func (h *Handlers) expandRuleExpr(r *http.Request, rule *string) (*filter.Expr, bool) {
	resolved, allowed := h.evaluateRuleWhere(r, rule, nil)
	if !allowed {
		return nil, false
	}
	if resolved == "" {
		return nil, true
	}
	expr, err := filter.ParseFilter(resolved)
	if err != nil {
		return nil, false
	}
	return expr, true
}

func (h *Handlers) resolveRelationTarget(ctx context.Context, field *collection.SchemaField) *collection.Collection {
	if field == nil || field.Options == nil {
		return nil
	}
	if name, _ := field.Options["collection_name"].(string); name != "" {
		if c, err := h.app.Collections().GetCollectionByName(ctx, name); err == nil {
			return c
		}
	}
	if name, _ := field.Options["collection"].(string); name != "" {
		if c, err := h.app.Collections().GetCollectionByName(ctx, name); err == nil {
			return c
		}
	}
	if id, _ := field.Options["collection_id"].(string); id != "" {
		if c, err := h.app.Collections().GetCollection(ctx, id); err == nil {
			return c
		}
	}
	return nil
}

func (h *Handlers) fetchExpandRows(ctx context.Context, sql string, ids []string) []map[string]any {
	rows, err := h.app.DB().ReadQuery(ctx, sql, ids)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func collectionSchemaField(coll *collection.Collection, name string) *collection.SchemaField {
	for i := range coll.Schema {
		if coll.Schema[i].Name == name {
			return &coll.Schema[i]
		}
	}
	return nil
}

func recordExpandMap(rec map[string]any) map[string]any {
	if m, ok := rec["expand"].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	rec["expand"] = m
	return m
}
