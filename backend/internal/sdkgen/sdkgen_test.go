package sdkgen

import (
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
)

func baseCollection() *collection.Collection {
	return &collection.Collection{
		Name: "articles",
		Type: collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
			{Name: "views", Type: collection.FieldNumber},
			{Name: "published", Type: collection.FieldBool},
			{Name: "metadata", Type: collection.FieldJSON},
			{Name: "cover", Type: collection.FieldFile},
			{Name: "author", Type: collection.FieldRelation},
			{Name: "location", Type: collection.FieldGeoPoint},
			{Name: "embedding", Type: collection.FieldVector},
			{
				Name: "status",
				Type: collection.FieldSelect,
				Options: map[string]any{
					"values": []any{"draft", "published", "archived"},
				},
			},
			{Name: "tag", Type: collection.FieldSelect}, // no options -> string
		},
	}
}

func authCollection() *collection.Collection {
	return &collection.Collection{
		Name: "users",
		Type: collection.TypeAuth,
		Schema: []collection.SchemaField{
			{Name: "name", Type: collection.FieldText},
		},
	}
}

func systemCollection() *collection.Collection {
	return &collection.Collection{
		Name: "_collections",
		Type: collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "name", Type: collection.FieldText},
		},
	}
}

func TestGenerate_InterfacesAndFieldTypes(t *testing.T) {
	out := Generate([]*collection.Collection{baseCollection(), authCollection()}, "http://localhost:8090/api/v1")

	contains := []string{
		// Interface names.
		"export interface Articles {",
		"export interface Users {",
		// System record fields.
		"id: string;",
		"created_at: string;",
		"updated_at: string;",
		// Field type mappings.
		"title: string;",                           // required text -> no optional marker
		"views?: number;",                          // number, optional
		"published?: boolean;",                     // bool
		"metadata?: any;",                          // json
		"cover?: string | string[];",               // file
		"author?: string | string[];",              // relation
		"location?: { lat: number; lng: number };", // geo_point
		"embedding?: number[];",                    // vector
		// Select with options -> string union.
		`status?: "draft" | "published" | "archived";`,
		// Select without options -> string.
		"tag?: string;",
		// Auth extras.
		"email?: string;",
		"verified?: boolean;",
		// Client class + methods.
		"export class GresbaseTyped {",
		"get articles() {",
		"get users() {",
		"list(opts?: ListOptions)",
		"getOne(id: string",
		"create(data: Partial<Articles>)",
		"update(id: string, data: Partial<Articles>)",
		"delete(id: string)",
		// Base URL embedded.
		"http://localhost:8090/api/v1",
	}

	for _, want := range contains {
		if !strings.Contains(out, want) {
			t.Errorf("generated output missing %q\n---\n%s", want, out)
		}
	}
}

func TestGenerate_SkipsSystemCollections(t *testing.T) {
	out := Generate([]*collection.Collection{systemCollection(), baseCollection()}, "")

	if strings.Contains(out, "_collections") {
		t.Errorf("system collection should be skipped, got:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "interface collections") {
		t.Errorf("system collection should not produce an interface, got:\n%s", out)
	}
	if !strings.Contains(out, "export interface Articles {") {
		t.Errorf("non-system collection should still be generated, got:\n%s", out)
	}
}

func TestGenerate_EmptyCollections(t *testing.T) {
	out := Generate(nil, "http://example.com")

	if !strings.Contains(out, "export class GresbaseTyped {") {
		t.Errorf("empty input should still emit a client class, got:\n%s", out)
	}
	if !strings.Contains(out, "export interface ListResult<T>") {
		t.Errorf("empty input should still emit shared types, got:\n%s", out)
	}
	if strings.Contains(out, "export interface ") && strings.Count(out, "export interface ") > 2 {
		// Only the two shared interfaces (ListOptions, ListResult) expected.
		t.Errorf("empty input should not emit record interfaces, got:\n%s", out)
	}
}

func TestGenerate_VectorAndSelectUnionExact(t *testing.T) {
	out := Generate([]*collection.Collection{baseCollection()}, "")

	if !strings.Contains(out, "embedding?: number[];") {
		t.Errorf("vector field should map to number[], got:\n%s", out)
	}
	if !strings.Contains(out, `status?: "draft" | "published" | "archived";`) {
		t.Errorf("select with options should be a string union, got:\n%s", out)
	}
}

func TestInterfaceAndMethodNaming(t *testing.T) {
	cases := []struct {
		name       string
		wantIface  string
		wantMethod string
	}{
		{"blog_posts", "BlogPosts", "blogPosts"},
		{"users", "Users", "users"},
		{"order-items", "OrderItems", "orderItems"},
	}
	for _, tc := range cases {
		if got := interfaceName(tc.name); got != tc.wantIface {
			t.Errorf("interfaceName(%q) = %q, want %q", tc.name, got, tc.wantIface)
		}
		if got := methodName(tc.name); got != tc.wantMethod {
			t.Errorf("methodName(%q) = %q, want %q", tc.name, got, tc.wantMethod)
		}
	}
}
