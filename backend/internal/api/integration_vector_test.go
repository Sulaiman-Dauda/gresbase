package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
)

// TestIntegration_VectorSearch exercises the full pgvector path: create a
// collection with a vector field, insert embeddings, and run a similarity
// search. Skips automatically when the PostgreSQL under test lacks pgvector
// (e.g. the embedded build), so it is a no-op locally and real in CI.
func TestIntegration_VectorSearch(t *testing.T) {
	env := newIntegrationEnv(t)
	if !env.app.DB().HasVectorSupport(context.Background()) {
		t.Skip("pgvector not available on this PostgreSQL; skipping vector search test")
	}
	adminToken := env.createAdminToken(t)

	public := ""
	coll := &collection.Collection{
		Name: "documents",
		Type: collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
			{Name: "embedding", Type: collection.FieldVector, Options: map[string]any{
				"dimensions": float64(3),
				"distance":   "cosine",
			}},
		},
		ListRule: &public,
	}
	if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create vector collection: %v", err)
	}

	seed := []struct {
		title string
		vec   []any
	}{
		{"apple", []any{1.0, 0.0, 0.0}},
		{"banana", []any{0.9, 0.1, 0.0}},
		{"car", []any{0.0, 0.0, 1.0}},
	}
	for _, s := range seed {
		if _, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{
			"title":     s.title,
			"embedding": s.vec,
		}); err != nil {
			t.Fatalf("insert %s: %v", s.title, err)
		}
	}

	// Query close to "apple"; nearest should be apple, then banana.
	resp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/documents/search-vector", map[string]any{
		"field":  "embedding",
		"vector": []float64{1.0, 0.0, 0.0},
		"limit":  2,
	}, map[string]string{"Authorization": "Bearer " + adminToken})
	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, resp, &body)
		t.Fatalf("vector search status = %d body=%v", resp.StatusCode, body)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	readJSONBody(t, resp, &body)
	if len(body.Items) != 2 {
		t.Fatalf("expected 2 results, got %d", len(body.Items))
	}
	if body.Items[0]["title"] != "apple" {
		t.Fatalf("expected nearest result to be apple, got %v", body.Items[0]["title"])
	}
	if _, ok := body.Items[0]["_distance"]; !ok {
		t.Fatalf("expected _distance annotation on results")
	}

	// Anonymous search must respect the (public) list rule and also succeed here.
	anon := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/documents/search-vector", map[string]any{
		"field":  "embedding",
		"vector": []float64{0.0, 0.0, 1.0},
		"limit":  1,
	}, nil)
	if anon.StatusCode != http.StatusOK {
		t.Fatalf("anon vector search status = %d", anon.StatusCode)
	}
	anon.Body.Close()
}
