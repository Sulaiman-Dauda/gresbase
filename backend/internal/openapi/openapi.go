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
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Servers    []Server            `json:"servers"`
	Paths      map[string]PathItem `json:"paths"`
	Components *Components         `json:"components,omitempty"`
	Tags       []Tag               `json:"tags,omitempty"`
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
	Summary     string      `json:"summary,omitempty"`
	Description string      `json:"description,omitempty"`
	Get         *Operation  `json:"get,omitempty"`
	Post        *Operation  `json:"post,omitempty"`
	Put         *Operation  `json:"put,omitempty"`
	Patch       *Operation  `json:"patch,omitempty"`
	Delete      *Operation  `json:"delete,omitempty"`
	Parameters  []Parameter `json:"parameters,omitempty"`
}

type Operation struct {
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Security    []map[string][]string `json:"security,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema *Schema `json:"schema"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type Schema struct {
	Type       string             `json:"type,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
	Format     string             `json:"format,omitempty"`
	Example    any                `json:"example,omitempty"`
}

type Components struct {
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Schemas         map[string]Schema         `json:"schemas,omitempty"`
}

type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Name         string `json:"name,omitempty"`
	In           string `json:"in,omitempty"`
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
					"id":     {Type: "string"},
					"name":   {Type: "string"},
					"type":   {Type: "string"},
					"schema": {Type: "array", Items: &Schema{Type: "object"}},
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

		successStatus := route.SuccessStatus
		if successStatus == "" {
			successStatus = "200"
		}
		responseSchema := route.ResponseSchema
		if responseSchema == nil {
			responseSchema = &Schema{Type: "object"}
		}
		responseContentType := route.ResponseContentType
		if responseContentType == "" {
			responseContentType = "application/json"
		}

		op := &Operation{
			Summary:     route.Summary,
			OperationID: route.OperationID,
			Tags:        []string{route.Tag},
			Parameters:  route.Parameters,
			Responses: map[string]Response{
				successStatus: {Description: "Success", Content: map[string]MediaType{
					responseContentType: {Schema: responseSchema},
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
			op.Security = []map[string][]string{{"bearerAuth": {}}, {"apiKey": {}}}
		}

		if route.RequestSchema != nil {
			requestContentType := route.RequestContentType
			if requestContentType == "" {
				requestContentType = "application/json"
			}
			op.RequestBody = &RequestBody{
				Required: true,
				Content: map[string]MediaType{
					requestContentType: {Schema: route.RequestSchema},
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
	Method              string      // GET, POST, PUT, PATCH, DELETE
	Pattern             string      // e.g., /api/v1/collections
	Summary             string      // Short description
	OperationID         string      // CamelCase operation ID
	Tag                 string      // Grouping tag
	AuthRequired        bool        // Whether JWT or API key auth is required
	Parameters          []Parameter // Path/query parameters
	RequestSchema       *Schema     // Request body schema (nil if no body)
	RequestContentType  string      // Request content type (defaults to application/json)
	ResponseSchema      *Schema     // Success response schema
	ResponseContentType string      // Response content type (defaults to application/json)
	SuccessStatus       string      // Success status code (defaults to 200)
}

// ExtractRoutes walks a chi router and extracts route definitions.
func ExtractRoutes(r chi.Router) []RouteDef {
	var routes []RouteDef
	walkChiRouter(r, "", &routes)
	return routes
}

func walkChiRouter(r chi.Router, prefix string, routes *[]RouteDef) {
	if err := chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if !shouldDocumentMethod(method) {
			return nil
		}
		pattern := prefix + route
		def := RouteDef{
			Method:              method,
			Pattern:             pattern,
			Summary:             summarizeRoute(method, pattern),
			OperationID:         operationIDFromRoute(method, pattern),
			Tag:                 tagFromRoute(pattern),
			AuthRequired:        authRequiredForRoute(method, pattern),
			Parameters:          parametersForRoute(method, pattern),
			RequestSchema:       requestSchemaForRoute(method, pattern),
			RequestContentType:  requestContentTypeForRoute(method, pattern),
			ResponseSchema:      responseSchemaForRoute(method, pattern),
			ResponseContentType: responseContentTypeForRoute(method, pattern),
			SuccessStatus:       successStatusForRoute(method, pattern),
		}
		*routes = append(*routes, def)
		return nil
	}); err != nil {
		// Walk failed silently
	}
}

func shouldDocumentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func authRequiredForRoute(method, route string) bool {
	switch {
	case route == "/api/v1/auth/logout", route == "/api/v1/auth/request-email-change":
		return true
	case strings.HasPrefix(route, "/api/v1/admin"):
		return true
	case strings.Contains(route, "/api/v1/collections/") && strings.Contains(route, "/auth/"):
		return strings.Contains(route, "/impersonate/")
	case strings.Contains(route, "/api/v1/collections/") && strings.Contains(route, "/auth-methods"):
		return false
	case strings.HasPrefix(route, "/api/v1/collections"):
		return true
	case route == "/api/v1/files/upload":
		return true
	case strings.HasPrefix(route, "/api/v1/files/") && method == http.MethodDelete:
		return true
	case strings.HasPrefix(route, "/api/v1/certificates"):
		return true
	case strings.HasPrefix(route, "/api/v1/settings"):
		return true
	case strings.HasPrefix(route, "/api/v1/logs"):
		return true
	case strings.HasPrefix(route, "/api/v1/api-keys"):
		return true
	case strings.HasPrefix(route, "/api/v1/backups"):
		return true
	case strings.HasPrefix(route, "/api/v1/fts"):
		return true
	case strings.HasPrefix(route, "/api/v1/plugins/js"):
		return true
	case strings.HasPrefix(route, "/api/v1/jobs"):
		return true
	default:
		return false
	}
}

func parametersForRoute(method, route string) []Parameter {
	params := extractPathParameters(route)

	switch {
	case method == http.MethodGet && strings.HasPrefix(route, "/api/v1/records/") && !strings.Contains(route, "{recordId}"):
		params = append(params,
			Parameter{Name: "page", In: "query", Schema: &Schema{Type: "integer"}},
			Parameter{Name: "perPage", In: "query", Schema: &Schema{Type: "integer"}},
			Parameter{Name: "sort", In: "query", Schema: &Schema{Type: "string"}},
			Parameter{Name: "filter", In: "query", Schema: &Schema{Type: "string"}},
			Parameter{Name: "expand", In: "query", Schema: &Schema{Type: "string"}},
			Parameter{Name: "fields", In: "query", Schema: &Schema{Type: "string"}},
		)
	case method == http.MethodGet && strings.HasPrefix(route, "/api/v1/logs"):
		params = append(params,
			Parameter{Name: "page", In: "query", Schema: &Schema{Type: "integer"}},
			Parameter{Name: "perPage", In: "query", Schema: &Schema{Type: "integer"}},
			Parameter{Name: "action", In: "query", Schema: &Schema{Type: "string"}},
			Parameter{Name: "resource", In: "query", Schema: &Schema{Type: "string"}},
		)
	}

	return params
}

func extractPathParameters(route string) []Parameter {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	params := make([]Parameter, 0)
	for _, part := range parts {
		if len(part) < 3 || part[0] != '{' || part[len(part)-1] != '}' {
			continue
		}
		name := part[1 : len(part)-1]
		params = append(params, Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
	}
	return params
}

func routeHasRequestBody(method, route string) bool {
	switch method {
	case http.MethodPut, http.MethodPatch:
		return true
	case http.MethodPost:
		switch {
		case route == "/api/v1/auth/logout":
			return false
		case strings.HasSuffix(route, "/run"):
			return false
		case strings.HasSuffix(route, "/restore"):
			return false
		default:
			return true
		}
	default:
		return false
	}
}

func requestSchemaForRoute(method, route string) *Schema {
	if !routeHasRequestBody(method, route) {
		return nil
	}

	switch {
	case strings.Contains(route, "/auth/login"):
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"email":    {Type: "string", Format: "email"},
			"password": {Type: "string", Format: "password"},
		}}
	case strings.Contains(route, "/auth/refresh") || strings.Contains(route, "/auth-refresh"):
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"refreshToken": {Type: "string"},
		}}
	case route == "/api/v1/setup", strings.Contains(route, "/auth/register"), route == "/api/v1/admin/users":
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"email":    {Type: "string", Format: "email"},
			"password": {Type: "string", Format: "password"},
			"role":     {Type: "string"},
		}}
	case strings.HasPrefix(route, "/api/v1/batch"):
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"requests": {Type: "array", Items: &Schema{Type: "object"}},
		}}
	case route == "/api/v1/files/upload":
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"collection": {Type: "string"},
			"recordId":   {Type: "string"},
			"file":       {Type: "string", Format: "binary"},
		}}
	default:
		return &Schema{Type: "object"}
	}
}

func requestContentTypeForRoute(method, route string) string {
	if !routeHasRequestBody(method, route) {
		return ""
	}
	if route == "/api/v1/files/upload" {
		return "multipart/form-data"
	}
	return "application/json"
}

func responseSchemaForRoute(method, route string) *Schema {
	switch {
	case strings.Contains(route, "/health"):
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"status":    {Type: "string"},
			"version":   {Type: "string"},
			"database":  {Type: "boolean"},
			"timestamp": {Type: "string", Format: "date-time"},
		}}
	case strings.HasPrefix(route, "/api/v1/records/") && method == http.MethodGet && !strings.Contains(route, "{recordId}"):
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"items":      {Type: "array", Items: &Schema{Type: "object"}},
			"page":       {Type: "integer"},
			"perPage":    {Type: "integer"},
			"totalItems": {Type: "integer"},
			"totalPages": {Type: "integer"},
		}}
	case route == "/api/v1/files/upload":
		return &Schema{Type: "object", Properties: map[string]*Schema{
			"path":          {Type: "string"},
			"original_name": {Type: "string"},
		}}
	case (strings.HasPrefix(route, "/api/v1/files/") && method == http.MethodGet) || strings.Contains(route, "/download"):
		return &Schema{Type: "string", Format: "binary"}
	default:
		return &Schema{Type: "object"}
	}
}

func responseContentTypeForRoute(method, route string) string {
	switch {
	case strings.HasPrefix(route, "/api/v1/files/") && method == http.MethodGet:
		return "application/octet-stream"
	case strings.Contains(route, "/backups/") && strings.Contains(route, "/download"):
		return "application/octet-stream"
	default:
		return "application/json"
	}
}

func successStatusForRoute(method, route string) string {
	if method != http.MethodPost {
		return "200"
	}
	switch route {
	case "/api/v1/setup",
		"/api/v1/auth/register",
		"/api/v1/admin/users",
		"/api/v1/collections/",
		"/api/v1/records/{collection}/",
		"/api/v1/files/upload",
		"/api/v1/api-keys/",
		"/api/v1/backups/",
		"/api/v1/plugins/js/",
		"/api/v1/jobs/":
		return "201"
	default:
		return "200"
	}
}

func summarizeRoute(method, route string) string {
	switch {
	case strings.Contains(route, "health"):
		return "Health check"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth-methods"):
		return "List record auth methods"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/auth-with-password"):
		return "Authenticate record with password"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/auth-with-anonymous"):
		return "Authenticate as a new anonymous record"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/auth-with-otp"):
		return "Authenticate record with OTP"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth-refresh"):
		return "Refresh record auth token"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/passkey/register"):
		return "Register a passkey (WebAuthn) for the authenticated record"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/passkey/login"):
		return "Authenticate record with a passkey (WebAuthn)"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/passkeys"):
		return "List or delete the authenticated record's passkeys"
	case strings.Contains(route, "/collections/") && strings.Contains(route, "/auth/"):
		return "Record authentication"
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
	case strings.Contains(route, "/collections/export"):
		return "Export collections"
	case strings.Contains(route, "/collections/import"):
		return "Import collections"
	case strings.Contains(route, "collections"):
		if method == http.MethodGet {
			if strings.Contains(route, "{id}") {
				return "Get collection"
			}
			return "List collections"
		}
		if method == http.MethodPost {
			return "Create collection"
		}
		if method == http.MethodDelete {
			return "Delete collection"
		}
		return "Manage collection"
	case strings.Contains(route, "/records/") && strings.Contains(route, "/aggregate"):
		return "Aggregate records (count/sum/avg/min/max with groupBy)"
	case strings.Contains(route, "/realtime/broadcast"):
		return "Broadcast a message to a realtime channel"
	case strings.Contains(route, "/files/tus"):
		return "Resumable file upload (TUS protocol): POST creates an upload targeting an existing record's file field, PATCH appends chunks, HEAD retrieves the offset, DELETE terminates"
	case strings.Contains(route, "records"):
		if method == http.MethodGet {
			if strings.Contains(route, "{recordId}") {
				return "Get record"
			}
			return "List records"
		}
		if method == http.MethodPost {
			return "Create record"
		}
		if method == http.MethodPut || method == http.MethodPatch {
			return "Update record"
		}
		return "Delete record"
	case strings.Contains(route, "/batch"):
		return "Batch request"
	case strings.Contains(route, "realtime") || strings.Contains(route, "sse"):
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
	case strings.Contains(route, "/collections/") && (strings.Contains(route, "/auth/") || strings.Contains(route, "/auth-methods")):
		return "Record Auth"
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
