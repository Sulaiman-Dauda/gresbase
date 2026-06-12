package collection

import (
	"errors"
	"strings"
	"testing"
)

func rlsStr(s string) *string { return &s }

// rlsColl builds a base collection with all rules locked unless set.
func rlsColl(name string) *Collection {
	return &Collection{Name: name, Type: TypeBase}
}

func rlsFindPolicy(t *testing.T, policies []RLSPolicy, op string) RLSPolicy {
	t.Helper()
	for _, p := range policies {
		if p.Operation == op {
			return p
		}
	}
	t.Fatalf("no %s policy found in %v", op, policies)
	return RLSPolicy{}
}

// compileDeleteUsing compiles a single rule via the DELETE policy (pure
// USING, no list/view merge) and returns the full policy SQL.
func compileDeleteUsing(t *testing.T, rule *string) string {
	t.Helper()
	coll := rlsColl("posts")
	coll.DeleteRule = rule
	policies, _, err := CompileRLS(coll, RLSOptions{})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	return rlsFindPolicy(t, policies, "DELETE").SQL
}

func TestRLSCompileExpressions(t *testing.T) {
	// Mirrors runtime semantics from internal/api/rule_evaluator_test.go and
	// internal/filter SQLBuilder: ~ is case-insensitive contains (ILIKE
	// '%v%'), ?= is jsonb containment, = null is IS NULL.
	tests := []struct {
		name string
		rule *string
		want string
	}{
		{
			name: "locked (nil) compiles to false",
			rule: nil,
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING (false)`,
		},
		{
			name: "public (empty) compiles to true",
			rule: rlsStr(""),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING (true)`,
		},
		{
			name: "owner rule maps auth id to current_setting",
			rule: rlsStr(`owner = @request.auth.id`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING ("owner" = current_setting('gresbase.auth_id', true))`,
		},
		{
			name: "compound and with macro on left side",
			rule: rlsStr(`owner = @request.auth.id && @request.auth.role = "admin"`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING (("owner" = current_setting('gresbase.auth_id', true) AND current_setting('gresbase.auth_role', true) = 'admin'))`,
		},
		{
			name: "parenthesized or inside and",
			rule: rlsStr(`(status = "active" || status = "draft") && owner = @request.auth.id`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING (((("status" = 'active' OR "status" = 'draft')) AND "owner" = current_setting('gresbase.auth_id', true)))`,
		},
		{
			name: "contains maps to ILIKE like the runtime",
			rule: rlsStr(`title ~ "hello"`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING ("title" ILIKE '%hello%')`,
		},
		{
			name: "array contains maps to jsonb containment",
			rule: rlsStr(`tags ?= "go"`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING ("tags"::jsonb @> to_jsonb('go'::text))`,
		},
		{
			name: "null comparison maps to IS NULL",
			rule: rlsStr(`deleted_at = null`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING ("deleted_at" IS NULL)`,
		},
		{
			name: "single quotes in literals are doubled",
			rule: rlsStr(`title = "it's"`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING ("title" = 'it''s')`,
		},
		{
			name: "numbers and AND keyword",
			rule: rlsStr(`age >= 18 AND verified = true`),
			want: `CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING (("age" >= 18 AND "verified" = TRUE))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compileDeleteUsing(t, tc.rule)
			if got != tc.want {
				t.Fatalf("unexpected SQL:\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestRLSPolicyShapes(t *testing.T) {
	coll := rlsColl("posts")
	coll.ListRule = rlsStr("")
	coll.ViewRule = rlsStr("")
	coll.CreateRule = rlsStr(`owner = @request.auth.id`)
	coll.UpdateRule = rlsStr(`owner = @request.auth.id`)

	policies, _, err := CompileRLS(coll, RLSOptions{Role: "app_reader"})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	if len(policies) != 4 {
		t.Fatalf("expected 4 policies, got %d", len(policies))
	}

	insert := rlsFindPolicy(t, policies, "INSERT")
	if insert.SQL != `CREATE POLICY "gresbase_posts_insert" ON "posts" FOR INSERT TO "app_reader" WITH CHECK ("owner" = current_setting('gresbase.auth_id', true))` {
		t.Fatalf("unexpected INSERT policy: %s", insert.SQL)
	}
	if strings.Contains(insert.SQL, " USING ") {
		t.Fatalf("INSERT policy must not have USING: %s", insert.SQL)
	}

	update := rlsFindPolicy(t, policies, "UPDATE")
	wantUpdate := `CREATE POLICY "gresbase_posts_update" ON "posts" FOR UPDATE TO "app_reader" USING ("owner" = current_setting('gresbase.auth_id', true)) WITH CHECK ("owner" = current_setting('gresbase.auth_id', true))`
	if update.SQL != wantUpdate {
		t.Fatalf("unexpected UPDATE policy:\n got: %s\nwant: %s", update.SQL, wantUpdate)
	}

	// DELETE rule is nil → locked.
	del := rlsFindPolicy(t, policies, "DELETE")
	if !strings.HasSuffix(del.SQL, "USING (false)") {
		t.Fatalf("expected locked DELETE policy, got: %s", del.SQL)
	}

	if insert.Name != "gresbase_posts_insert" || insert.Collection != "posts" {
		t.Fatalf("unexpected policy metadata: %+v", insert)
	}
}

func TestRLSSelectMergesListAndViewRules(t *testing.T) {
	// Postgres has a single SELECT policy; list and view rules are OR-ed.
	coll := rlsColl("posts")
	coll.ListRule = rlsStr(`owner = @request.auth.id`)
	coll.ViewRule = rlsStr(`status = "published"`)

	policies, warnings, err := CompileRLS(coll, RLSOptions{})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	sel := rlsFindPolicy(t, policies, "SELECT")
	want := `USING (("owner" = current_setting('gresbase.auth_id', true) OR "status" = 'published'))`
	if !strings.HasSuffix(sel.SQL, want) {
		t.Fatalf("unexpected SELECT policy: %s", sel.SQL)
	}

	var found bool
	for _, w := range warnings {
		if w.Operation == "SELECT" && strings.Contains(w.Message, "OR") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected list/view merge warning, got: %v", warnings)
	}

	// nil view rule contributes false and drops out of the OR.
	coll.ViewRule = nil
	policies, _, err = CompileRLS(coll, RLSOptions{})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	sel = rlsFindPolicy(t, policies, "SELECT")
	if !strings.HasSuffix(sel.SQL, `USING ("owner" = current_setting('gresbase.auth_id', true))`) {
		t.Fatalf("unexpected SELECT policy with nil view rule: %s", sel.SQL)
	}

	// Identical rules → no merge warning.
	coll.ViewRule = rlsStr(`owner = @request.auth.id`)
	_, warnings, err = CompileRLS(coll, RLSOptions{})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	for _, w := range warnings {
		if w.Operation == "SELECT" {
			t.Fatalf("unexpected warning for identical list/view rules: %+v", w)
		}
	}
}

func TestRLSUnsupportedPlaceholder(t *testing.T) {
	coll := rlsColl("posts")
	coll.CreateRule = rlsStr(`@request.body.title = "Hello"`)

	policies, _, err := CompileRLS(coll, RLSOptions{})
	if err == nil {
		t.Fatal("expected error for @request.body placeholder")
	}
	if policies != nil {
		t.Fatalf("expected no policies on error, got: %v", policies)
	}
	var unsupported *UnsupportedPlaceholderError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedPlaceholderError, got %T: %v", err, err)
	}
	if unsupported.Operation != "INSERT" || len(unsupported.Placeholders) != 1 || unsupported.Placeholders[0] != "@request.body.title" {
		t.Fatalf("unexpected error details: %+v", unsupported)
	}

	// Force-lock: emit USING/WITH CHECK (false) with a comment instead.
	policies, warnings, err := CompileRLS(coll, RLSOptions{ForceLockUnsupported: true})
	if err != nil {
		t.Fatalf("CompileRLS force-lock: %v", err)
	}
	insert := rlsFindPolicy(t, policies, "INSERT")
	if !strings.Contains(insert.SQL, "WITH CHECK (false /*") || !strings.Contains(insert.SQL, "@request.body.title") {
		t.Fatalf("expected force-locked INSERT policy with comment, got: %s", insert.SQL)
	}
	var warned bool
	for _, w := range warnings {
		if w.Operation == "INSERT" && strings.Contains(w.Message, "force-locked") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected force-lock warning, got: %v", warnings)
	}
}

func TestRLSUnsupportedPlaceholderVariants(t *testing.T) {
	for _, rule := range []string{
		`status = @request.query.status`,
		`@request.method = "POST"`,
		`@request.headers.x_token = "abc"`,
	} {
		coll := rlsColl("posts")
		coll.DeleteRule = rlsStr(rule)
		_, _, err := CompileRLS(coll, RLSOptions{})
		var unsupported *UnsupportedPlaceholderError
		if !errors.As(err, &unsupported) {
			t.Fatalf("rule %q: expected UnsupportedPlaceholderError, got %v", rule, err)
		}
	}
}

func TestRLSIdentifierQuoting(t *testing.T) {
	coll := rlsColl(`we"ird name`)
	coll.ListRule = rlsStr("")
	policies, _, err := CompileRLS(coll, RLSOptions{Role: `odd"role`})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	sel := rlsFindPolicy(t, policies, "SELECT")
	want := `CREATE POLICY "gresbase_we""ird name_select" ON "we""ird name" FOR SELECT TO "odd""role" USING (true)`
	if sel.SQL != want {
		t.Fatalf("unexpected quoting:\n got: %s\nwant: %s", sel.SQL, want)
	}
}

func TestRLSViewCollectionSkipped(t *testing.T) {
	coll := &Collection{Name: "report", Type: TypeView, ViewQuery: "SELECT 1"}
	policies, warnings, err := CompileRLS(coll, RLSOptions{})
	if err != nil {
		t.Fatalf("CompileRLS: %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("expected no policies for view collection, got: %v", policies)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "view") {
		t.Fatalf("expected view skip warning, got: %v", warnings)
	}
}

func TestRLSScriptGolden(t *testing.T) {
	posts := rlsColl("posts")
	posts.ListRule = rlsStr("")
	posts.ViewRule = rlsStr("")
	posts.CreateRule = rlsStr(`owner = @request.auth.id`)
	posts.UpdateRule = rlsStr(`owner = @request.auth.id`)
	// DeleteRule nil → locked.

	comments := rlsColl("comments")
	comments.ListRule = rlsStr(`post.owner != null && published = true`)
	// All other rules nil → locked.

	script, warnings, err := RLSScript([]*Collection{posts, comments}, RLSOptions{})
	if err != nil {
		t.Fatalf("RLSScript: %v", err)
	}

	want := `-- Gresbase row-level security policies (generated).
-- Run as the table owner. Idempotent: safe to re-run after rule changes.
--
-- One-time restricted role setup (run manually, not part of this script):
--   CREATE ROLE "gresbase_client" LOGIN; -- add PASSWORD '...' as needed
--   GRANT USAGE ON SCHEMA public TO "gresbase_client";
--
-- Each session must set its auth context before querying, e.g.:
--   SELECT set_config('gresbase.auth_id', '<auth record id>', false);
--   SELECT set_config('gresbase.auth_email', '<email>', false);
-- Unset settings read as NULL, so rules fail closed.

-- Collection: posts
GRANT SELECT, INSERT, UPDATE, DELETE ON "posts" TO "gresbase_client";
ALTER TABLE "posts" ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS "gresbase_posts_select" ON "posts";
DROP POLICY IF EXISTS "gresbase_posts_insert" ON "posts";
DROP POLICY IF EXISTS "gresbase_posts_update" ON "posts";
DROP POLICY IF EXISTS "gresbase_posts_delete" ON "posts";
CREATE POLICY "gresbase_posts_select" ON "posts" FOR SELECT TO "gresbase_client" USING (true);
CREATE POLICY "gresbase_posts_insert" ON "posts" FOR INSERT TO "gresbase_client" WITH CHECK ("owner" = current_setting('gresbase.auth_id', true));
CREATE POLICY "gresbase_posts_update" ON "posts" FOR UPDATE TO "gresbase_client" USING ("owner" = current_setting('gresbase.auth_id', true)) WITH CHECK ("owner" = current_setting('gresbase.auth_id', true));
CREATE POLICY "gresbase_posts_delete" ON "posts" FOR DELETE TO "gresbase_client" USING (false);

-- Collection: comments
GRANT SELECT, INSERT, UPDATE, DELETE ON "comments" TO "gresbase_client";
ALTER TABLE "comments" ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS "gresbase_comments_select" ON "comments";
DROP POLICY IF EXISTS "gresbase_comments_insert" ON "comments";
DROP POLICY IF EXISTS "gresbase_comments_update" ON "comments";
DROP POLICY IF EXISTS "gresbase_comments_delete" ON "comments";
CREATE POLICY "gresbase_comments_select" ON "comments" FOR SELECT TO "gresbase_client" USING (("post.owner" IS NOT NULL AND "published" = TRUE));
CREATE POLICY "gresbase_comments_insert" ON "comments" FOR INSERT TO "gresbase_client" WITH CHECK (false);
CREATE POLICY "gresbase_comments_update" ON "comments" FOR UPDATE TO "gresbase_client" USING (false) WITH CHECK (false);
CREATE POLICY "gresbase_comments_delete" ON "comments" FOR DELETE TO "gresbase_client" USING (false);
`
	if script != want {
		t.Fatalf("script mismatch:\n--- got ---\n%s\n--- want ---\n%s", script, want)
	}

	// comments: list rule set, view rule nil → divergence warning expected.
	var merge bool
	for _, w := range warnings {
		if w.Collection == "comments" && w.Operation == "SELECT" {
			merge = true
		}
	}
	if !merge {
		t.Fatalf("expected list/view divergence warning for comments, got: %v", warnings)
	}
}

func TestRLSScriptSkipsUnsupportedPolicies(t *testing.T) {
	coll := rlsColl("posts")
	coll.ListRule = rlsStr("")
	coll.CreateRule = rlsStr(`@request.body.title = "x"`)

	script, warnings, err := RLSScript([]*Collection{coll}, RLSOptions{})
	if err != nil {
		t.Fatalf("RLSScript: %v", err)
	}
	if strings.Contains(script, `"gresbase_posts_insert" ON "posts" FOR INSERT`) {
		t.Fatalf("unsupported INSERT policy must be skipped, got:\n%s", script)
	}
	// The stale-policy drop must still be present so re-runs cannot leave an
	// old permissive policy behind, and RLS stays enabled (default deny).
	if !strings.Contains(script, `DROP POLICY IF EXISTS "gresbase_posts_insert" ON "posts";`) {
		t.Fatalf("expected DROP POLICY for skipped insert policy:\n%s", script)
	}
	if !strings.Contains(script, `ALTER TABLE "posts" ENABLE ROW LEVEL SECURITY;`) {
		t.Fatalf("expected RLS to stay enabled:\n%s", script)
	}
	var skipped bool
	for _, w := range warnings {
		if w.Operation == "INSERT" && strings.Contains(w.Message, "skipped") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("expected skip warning, got: %v", warnings)
	}
}
