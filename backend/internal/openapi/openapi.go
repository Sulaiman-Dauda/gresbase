// Package openapi provides auto-generated OpenAPI 3.1 specification
// from the Gresbase route definitions. This enables Swagger UI and
// standard API client generation.
package openapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Spec represents an OpenAPI 3.1 specification.
type Spec struct {
	OpenAPI      string              `json:"openapi"`
	Info         Info                `json:"info"`
	Servers      []Server            `json:"servers"`
	Paths        map[string]PathItem `json:"paths"`
	Components   *Components         `json:"components,omitempty"`
	Tags         []Tag               `json:"tags,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type PathItem struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Get         *Operation          `json:"get,omitempty"`
	Post        *Operation          `json:"post,omitempty"`
	Put         *Operation          `json:"put,omitempty"`
	Patch       *Operation          `json:"patch,omitempty"`
	Delete      *Operation          `json:"delete,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
}

type Operation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	OperationID string              `json:"operationId,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Security    []map[string][]string `json:"security,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

type RequestBody struct {
	Required bool              `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema *Schema `json:"schema"`
}

type Response struct {
	Description string              `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type Schema struct {
	Type       string            `json:"type,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Items      *Schema           `json:"items,omitempty"`
	Format     string            `json:"format,omitempty"`
	Example    any               `json:"example,omitempty"`
}

type Components struct {
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Schemas         map[string]Schema         `json:"schemas,omitempty"`
}

type SecurityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Name        string `json:"name,omitempty"`
	In          string `json:"in,omitempty"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Generate produces an OpenAPI spec from the routes registered on a chi router.
func Generate(version string, baseURL string, routes []RouteDef) *Spec {
	spec := &Spec{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:       "Gresbase API",
			Version:     version,
			Description: "Gresbase REST API — auto-generated OpenAPI specification. All endpoints support both admin JWT auth and API key auth.",
		},
		Servers: []Server{
			{URL: baseURL, Description: "Local server"},
		},
		Paths: make(map[string]PathItem),
		Components: &Components{
			SecuritySchemes: map[string]SecurityScheme{
				"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
				"apiKey":     {Type: "apiKey", Name: "Authorization", In: "header"},
			},
			Schemas: map[string]Schema{
				"Error": {Type: "object", Properties: map[string]*Schema{
					"code":    {Type: "integer", Format: "int32"},
					"message": {Type: "string"},
				}},
				"HealthResponse": {Type: "object", Properties: map[string]*Schema{
					"status":    {Type: "string"},
					"version":   {Type: "string"},
					"database":  {Type: "boolean"},
					"timestamp": {Type: "string", Format: "date-time"},
				}},
				"LoginRequest": {Type: "object", Properties: map[string]*Schema{
					"email":    {Type: "string", Format: "email"},
					"password": {Type: "string", Format: "password"},
				}},
				"AuthResponse": {Type: "object", Properties: map[string]*Schema{
					"token":        {Type: "string"},
					"refreshToken": {Type: "string"},
					"admin":        {Type: "object"},
				}},
				"Collection": {Type: "object", Properties: map[string]*Schema{
					"id":      {Type: "string"},
					"name":    {Type: "string"},
					"type":    {Type: "string"},
					"schema":  {Type: "array", Items: &Schema{Type: "object"}},
				}},
				"RecordResponse": {Type: "object", Properties: map[string]*Schema{
					"items":      {Type: "array", Items: &Schema{Type: "object"}},
					"page":       {Type: "integer"},
					"perPage":    {Type: "integer"},
					"totalItems": {Type: "integer"},
					"totalPages": {Type: "integer"},
				}},
			},
		},
		Tags: []Tag{
			{Name: "Health", Description: "Health check endpoints"},
			{Name: "Auth", Description: "Authentication & authorization"},
			{Name: "Admin", Description: "Admin user management"},
			{Name: "Collections", Description: "Dynamic collection management"},
			{Name: "Records", Description: "Record CRUD operations"},
			{Name: "Record Auth", Description: "End-user authentication for auth collections"},
			{Name: "Files", Description: "File upload & download"},
			{Name: "Realtime", Description: "SSE/WebSocket realtime subscriptions"},
			{Name: "ACME", Description: "ACME certificate management"},
			{Name: "Certificates", Description: "TLS certificate management"},
			{Name: "Settings", Description: "Application settings"},
			{Name: "Logs", Description: "Audit log management"},
			{Name: "API Keys", Description: "API key management"},
			{Name: "Backups", Description: "Backup & restore"},
			{Name: "Search", Description: "Full-text search"},
			{Name: "Jobs", Description: "Cron job management"},
		},
	}

	for _, route := range routes {
		pathItem := spec.Paths[route.Pattern]
		if pathItem.Summary == "" {
			pathItem.Summary = route.Summary
		}

		op := &Operation{
			Summary:     route.Summary,
			OperationID: route.OperationID,
			Tags:        []string{route.Tag},
			Responses: map[string]Response{
				"200": {Description: "Success", Content: map[string]MediaType{
					"application/json": {Schema: route.ResponseSchema},
				}},
				"400": {Description: "Bad request", Content: map[string]MediaType{
					"application/json": {Schema: &Schema{Type: "object", Properties: map[string]*Schema{
						"code":    {Type: "integer"},
						"message": {Type: "string"},
					}}},
				}},
				"401": {Description: "Unauthorized"},
			},
		}

		if route.AuthRequired {
			op.Security = []map[string][]string{{"bearerAuth": {}}}
		}

		if route.RequestSchema != nil {
			op.RequestBody = &RequestBody{
				Required: true,
				Content: map[string]MediaType{
					"application/json": {Schema: route.RequestSchema},
				},
			}
		}

		switch route.Method {
		case "GET":
			pathItem.Get = op
		case "POST":
			pathItem.Post = op
		case "PUT":
			pathItem.Put = op
		case "PATCH":
			pathItem.Patch = op
		case "DELETE":
			pathItem.Delete = op
		}

		spec.Paths[route.Pattern] = pathItem
	}

	return spec
}

// RouteDef describes a single API route for OpenAPI generation.
type RouteDef struct {
	Method         string  // GET, POST, PUT, PATCH, DELETE
	Pattern        string  // e.g., /api/v1/collections
	Summary        string  // Short description
	OperationID    string  // CamelCase operation ID
	Tag            string  // Grouping tag
	AuthRequired   bool    // Whether JWT auth is required
	RequestSchema  *Schema // Request body schema (nil if no body)
	ResponseSchema *Schema // Success response schema
}

// ExtractRoutes walks a chi router and extracts route definitions.
func ExtractRoutes(r chi.Router) []RouteDef {
	var routes []RouteDef
	walkChiRouter(r, "", &routes)
	return routes
}

func walkChiRouter(r chi.Router, prefix string, routes *[]RouteDef) {
	if err := chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		// Extract route info
		def := RouteDef{
			Method:      method,
			Pattern:     prefix + route,
			Summary:     summarizeRoute(method, route),
			OperationID: operationIDFromRoute(method, route),
			Tag:         tagFromRoute(route),
		}
		*routes = append(*routes, def)
		return nil
	}); err != nil {
		// Walk failed silently
	}
}

func summarizeRoute(method, route string) string {
	switch {
	case strings.Contains(route, "health"):
		return "Health check"
	case strings.Contains(route, "auth/login"):
		return "Admin login"
	case strings.Contains(route, "auth/refresh"):
		return "Refresh access token"
	case strings.Contains(route, "auth/register"):
		return "Register new admin"
	case strings.Contains(route, "auth/otp"):
		return "OTP authentication"
	case strings.Contains(route, "auth/magic-link"):
		return "Magic link authentication"
	case strings.Contains(route, "collections"):
		if method == "GET" {
			return "List collections"
		}
		if method == "POST" {
			return "Create collection"
		}
		return "Manage collection"
	case strings.Contains(route, "records"):
		if method == "GET" {
			return "List records"
		}
		if method == "POST" {
			return "Create record"
		}
		if method == "PUT" || method == "PATCH" {
			return "Update record"
		}
		return "Delete record"
	case strings.Contains(route, "realtime"):
		return "Realtime subscription"
	case strings.Contains(route, "backups"):
		return "Backup management"
	default:
		return method + " " + route
	}
}

func operationIDFromRoute(method, route string) string {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	id := strings.ToLower(method)
	for _, p := range parts {
		if strings.HasPrefix(p, "{") || strings.HasPrefix(p, ":") {
			continue // skip path params
		}
		id += strings.Title(strings.ReplaceAll(p, "-", "_"))
	}
	return id
}

func tagFromRoute(route string) string {
	switch {
	case strings.Contains(route, "health"):
		return "Health"
	case strings.Contains(route, "auth") || strings.Contains(route, "oauth"):
		return "Auth"
	case strings.Contains(route, "admin"):
		return "Admin"
	case strings.Contains(route, "collections"):
		return "Collections"
	case strings.Contains(route, "records"):
		return "Records"
	case strings.Contains(route, "files"):
		return "Files"
	case strings.Contains(route, "realtime") || strings.Contains(route, "sse"):
		return "Realtime"
	case strings.Contains(route, "acme"):
		return "ACME"
	case strings.Contains(route, "certificates"):
		return "Certificates"
	case strings.Contains(route, "settings"):
		return "Settings"
	case strings.Contains(route, "logs"):
		return "Logs"
	case strings.Contains(route, "api-keys"):
		return "API Keys"
	case strings.Contains(route, "backups"):
		return "Backups"
	case strings.Contains(route, "search") || strings.Contains(route, "fts"):
		return "Search"
	case strings.Contains(route, "jobs"):
		return "Jobs"
	case strings.Contains(route, "plugins"):
		return "Plugins"
	default:
		return "General"
	}
}

// Schema helper types
func (s *Schema) Ref(ref string) {
	s.Properties = map[string]*Schema{"$ref": {Type: ref}}
}

// Handler returns an HTTP handler that serves the OpenAPI spec as JSON.
func Handler(spec *Spec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(spec)
	}
}

// Ensure imports are used
var _ = strings.Title
var _ = chi.Walk
