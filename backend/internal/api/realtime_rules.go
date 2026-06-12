package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/gresbase/gresbase/internal/realtime"
)

// collectionCacheTTL bounds how stale a collection definition used for
// realtime rule checks may be. Rule changes propagate within this window.
const collectionCacheTTL = 5 * time.Second

// realtimeRuleChecker enforces collection access rules on realtime record
// event delivery. Wildcard subscriptions ({collection}/*) are checked against
// the list rule; single-record subscriptions against the view rule. Rules
// follow the same locked-by-default semantics as the REST handlers.
type realtimeRuleChecker struct {
	app   *app.App
	rules *RuleEvaluator

	mu    sync.Mutex
	cache map[string]cachedCollection
}

type cachedCollection struct {
	coll      *collection.Collection
	fetchedAt time.Time
}

// newRealtimeRuleChecker creates the rule checker the realtime hub consults
// before delivering record events to subscribers.
func newRealtimeRuleChecker(application *app.App) realtime.RuleChecker {
	return &realtimeRuleChecker{
		app:   application,
		rules: NewRuleEvaluator(),
		cache: make(map[string]cachedCollection),
	}
}

// CanReceiveRecord implements realtime.RuleChecker.
func (c *realtimeRuleChecker) CanReceiveRecord(collectionName, recordID string, record map[string]any, auth *realtime.AuthInfo, wildcard bool, filterExpr string) bool {
	rc := ruleContextFromRealtimeAuth(auth)

	if !rc.IsAdmin {
		coll := c.collectionByName(collectionName)
		if coll == nil {
			return false
		}
		rule := coll.ViewRule
		if wildcard {
			rule = coll.ListRule
		}
		if rule == nil {
			return false // locked: superusers only
		}
		if strings.TrimSpace(*rule) != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			allowed := c.rules.EvaluateRuleBool(ctx, *rule, rc, record)
			cancel()
			if !allowed {
				return false
			}
		}
	}

	// The subscriber's own filter must also match (applies to admins too).
	if strings.TrimSpace(filterExpr) != "" {
		expr, err := filter.ParseFilter(filterExpr)
		if err != nil {
			return false
		}
		matched, err := filter.FilterMatchesWithResolver(expr, record, func(key string) (any, error) {
			return c.rules.resolveRequestValue(key, rc), nil
		})
		if err != nil || !matched {
			return false
		}
	}

	return true
}

// collectionByName returns the collection definition, served from a short
// TTL cache so per-subscriber rule checks do not hammer the database.
func (c *realtimeRuleChecker) collectionByName(name string) *collection.Collection {
	now := time.Now()

	c.mu.Lock()
	if entry, ok := c.cache[name]; ok && now.Sub(entry.fetchedAt) < collectionCacheTTL {
		c.mu.Unlock()
		return entry.coll
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	coll, err := c.app.Collections().GetCollectionByName(ctx, name)
	if err != nil {
		coll = nil // negative results are cached too, to bound lookups
	}

	c.mu.Lock()
	c.cache[name] = cachedCollection{coll: coll, fetchedAt: now}
	c.mu.Unlock()
	return coll
}

// ruleContextFromRealtimeAuth converts realtime auth state into a rule context.
func ruleContextFromRealtimeAuth(auth *realtime.AuthInfo) *RuleContext {
	rc := &RuleContext{Query: map[string]string{}, Body: map[string]any{}}
	if auth == nil {
		return rc
	}
	rc.IsAdmin = auth.AdminID != ""
	rc.AdminID = auth.AdminID
	rc.IsRecordAuth = auth.RecordID != ""
	rc.RecordID = auth.RecordID
	rc.CollectionID = auth.Collection
	rc.Role = auth.Role
	rc.Email = auth.Email
	rc.TenantID = auth.TenantID
	rc.Verified = auth.Verified
	rc.Anonymous = auth.Anonymous
	return rc
}
