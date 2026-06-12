package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
)

// TestSteals_AggregateExpandAnonymous covers the June "steal pass" surfaces:
// the aggregate endpoint (list-rule enforced), rule-enforced forward and
// back-relation expansion, and anonymous record auth (opt-in only).
func TestSteals_AggregateExpandAnonymous(t *testing.T) {
	env := newIntegrationEnv(t)
	adminToken := env.createAdminToken(t)
	adminHeaders := map[string]string{"Authorization": "Bearer " + adminToken}

	public := strPtr("")

	// authors: locked (no rules). posts/comments: public reads.
	authors := &collection.Collection{
		Name: "authors", Type: collection.TypeBase,
		Schema: []collection.SchemaField{{Name: "name", Type: collection.FieldText}},
	}
	if err := env.app.Collections().CreateCollection(context.Background(), authors); err != nil {
		t.Fatalf("create authors: %v", err)
	}
	posts := &collection.Collection{
		Name: "posts", Type: collection.TypeBase,
		ListRule: public, ViewRule: public,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText},
			{Name: "amount", Type: collection.FieldNumber},
			{Name: "status", Type: collection.FieldText},
			{Name: "author", Type: collection.FieldRelation, Options: map[string]any{"collection_name": "authors", "max_select": 1}},
		},
	}
	if err := env.app.Collections().CreateCollection(context.Background(), posts); err != nil {
		t.Fatalf("create posts: %v", err)
	}
	comments := &collection.Collection{
		Name: "comments", Type: collection.TypeBase,
		ListRule: public, ViewRule: public,
		Schema: []collection.SchemaField{
			{Name: "body", Type: collection.FieldText},
			{Name: "post", Type: collection.FieldRelation, Options: map[string]any{"collection_name": "posts", "max_select": 1}},
		},
	}
	if err := env.app.Collections().CreateCollection(context.Background(), comments); err != nil {
		t.Fatalf("create comments: %v", err)
	}

	createRecord := func(coll string, data map[string]any) string {
		t.Helper()
		resp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/"+coll, data, adminHeaders)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("create %s record: status %d", coll, resp.StatusCode)
		}
		var body map[string]any
		readJSONBody(t, resp, &body)
		id, _ := body["id"].(string)
		if id == "" {
			t.Fatalf("create %s record: no id in %v", coll, body)
		}
		return id
	}

	authorID := createRecord("authors", map[string]any{"name": "secret author"})
	postID := createRecord("posts", map[string]any{"title": "p1", "amount": 10, "status": "paid", "author": authorID})
	createRecord("posts", map[string]any{"title": "p2", "amount": 5, "status": "paid"})
	createRecord("posts", map[string]any{"title": "p3", "amount": 7, "status": "open"})
	createRecord("comments", map[string]any{"body": "c1", "post": postID})
	createRecord("comments", map[string]any{"body": "c2", "post": postID})

	t.Run("aggregate honors list rule", func(t *testing.T) {
		// Public collection: anonymous aggregation works.
		resp := doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/records/posts/aggregate?aggregate=count,sum:amount&groupBy=status&sort=-count", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("public aggregate: status %d", resp.StatusCode)
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		readJSONBody(t, resp, &body)
		if len(body.Items) != 2 {
			t.Fatalf("expected 2 status groups, got %v", body.Items)
		}
		first := body.Items[0]
		if first["status"] != "paid" || first["count"] != float64(2) || first["sum_amount"] != float64(15) {
			t.Fatalf("unexpected paid group: %v", first)
		}

		// Locked collection: anonymous aggregation is denied.
		resp = doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/records/authors/aggregate?aggregate=count", nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("locked aggregate should be 403, got %d", resp.StatusCode)
		}
		resp.Body.Close()

		// Unknown fields are rejected before touching SQL.
		resp = doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/records/posts/aggregate?aggregate=sum:nope", nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("unknown aggregate field should be 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("expand enforces target collection rules", func(t *testing.T) {
		// Anonymous: post is readable but the locked author must not expand.
		resp := doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/records/posts/"+postID+"?expand=author", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("public view: status %d", resp.StatusCode)
		}
		var anonBody map[string]any
		readJSONBody(t, resp, &anonBody)
		if expand, ok := anonBody["expand"].(map[string]any); ok {
			if _, leaked := expand["author"]; leaked {
				t.Fatalf("locked relation leaked through expand: %v", expand)
			}
		}

		// Admin: expansion works.
		resp = doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/records/posts/"+postID+"?expand=author", nil, adminHeaders)
		var adminBody map[string]any
		readJSONBody(t, resp, &adminBody)
		expand, _ := adminBody["expand"].(map[string]any)
		author, _ := expand["author"].(map[string]any)
		if author == nil || author["name"] != "secret author" {
			t.Fatalf("admin expand should include author: %v", adminBody["expand"])
		}
	})

	t.Run("back-relation expand", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/records/posts/"+postID+"?expand=comments_via_post", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("view with back-relation expand: status %d", resp.StatusCode)
		}
		var body map[string]any
		readJSONBody(t, resp, &body)
		expand, _ := body["expand"].(map[string]any)
		kids, _ := expand["comments_via_post"].([]any)
		if len(kids) != 2 {
			t.Fatalf("expected 2 comments via back-relation, got %v", expand)
		}
	})

	t.Run("anonymous auth is opt-in", func(t *testing.T) {
		makeAuthCollection := func(name string, allow bool) {
			t.Helper()
			coll := &collection.Collection{
				Name: name, Type: collection.TypeAuth,
				Schema: []collection.SchemaField{
					{Name: "email", Type: collection.FieldEmail},
					{Name: "password", Type: collection.FieldPassword},
				},
				Options: map[string]any{"allowAnonymous": allow},
			}
			if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
				t.Fatalf("create auth collection %s: %v", name, err)
			}
		}
		makeAuthCollection("members", true)
		makeAuthCollection("staff", false)

		// Opted-in collection issues anonymous tokens.
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/members/auth/auth-with-anonymous", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("anonymous auth on opted-in collection: status %d", resp.StatusCode)
		}
		var authBody map[string]any
		readJSONBody(t, resp, &authBody)
		if authBody["token"] == nil || authBody["record"] == nil {
			t.Fatalf("anonymous auth response incomplete: %v", authBody)
		}

		// Default-off collection refuses.
		resp = doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/staff/auth/auth-with-anonymous", nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("anonymous auth without opt-in should be 403, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})
}
